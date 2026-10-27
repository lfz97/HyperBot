package engine

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"HyperBot/service/engine/config"

	"github.com/google/uuid"
)

// GetEngineService 创建引擎实例（轻量：只装配字段，不触盘）。生命周期统一由 Run 驱动。
func GetEngineService(name string) *Engine {
	return &Engine{
		Agentname:   name,
		inputCh:     make(chan string),
		interruptCh: make(chan struct{}),
		notice:      notice{Kind: NoticeNone},
		errBudget:   newErrorBudget(defaultErrorMaxTimes, defaultErrorSleepGap),
		bootState:   BootBooting,
	}
}

// Init 完成环境检查（bootstrap），把检查产物拷入引擎后由引擎装配运行时资产，
// 最后构造 runner。检查与装配的分工见 bootstrap.go / assemble.go。
// 首次启动（Check 创建了默认配置）不再驻留退出：引擎照常装配模板配置，
// 置 bootState=config，由 TUI 弹配置页收集模型配置后 ApplyFirstStartupConfig 收尾。
func (e *Engine) Init() {
	env, err := Check()
	if err != nil {
		e.parkWithFatal(FatalError, err.Error(), true)
		return
	}
	e.absorb(env)
	if err := e.flush(); err != nil {
		e.parkWithFatal(FatalError, err.Error(), true)
		return
	}
	if env.ConfigCreated {
		e.enterFirstStartup()
		return
	}
	e.setBootState(BootReady)
}

// flush 释放上一轮装配的运行时资产后重跑 ⑥–⑪ + newRunner。
// 幂等：不改变语义状态，重复调用只是把资产按当前配置再重建一遍，结果收敛。
// 首次启动配置提交后整条装配路径重跑：引擎无需知道哪些组件快照了 config
// （session service 的 summarizer 拷贝了 config.Model 值、cron agent 冻结了
// config 指针——整体重建一次性全部覆盖）。仅可在无回合进行中时调用
// （首次启动的配置页是模态的，天然满足；装配错误由调用方决定呈现方式）。
func (e *Engine) flush() error {
	e.releaseRuntime()
	if err := e.assembleConfig(); err != nil {
		return err
	}
	e.buildSystemPrompt()
	e.SessionService_p = e.buildSessionService()
	if err := e.buildMemoryService(); err != nil {
		return err
	}
	if err := e.buildBuiltinAssets(); err != nil {
		return err
	}
	e.loadSkills()
	e.newRunner()
	return nil
}

// releaseRuntime 关闭当前运行时资产（nil 安全：首次调用无事可放）。
// runner 持有 session service 引用，二者一起放；cron 工具集有调度器须显式停。
func (e *Engine) releaseRuntime() {
	if e.AgentRunner_p != nil {
		(*e.AgentRunner_p).Runner.Close()
		e.AgentRunner_p = nil
	}
	for _, ts := range e.builtinToolsets {
		ts.Close()
	}
	e.builtinToolsets = nil
	e.builtinTools = nil
	if e.SessionService_p != nil {
		_ = e.SessionService_p.Close() //session.Service 接口自带 Close（内存/SQLite 实现均满足）
		e.SessionService_p = nil
	}
}

// ApplyFirstStartupConfig 接收 TUI 配置页提交的模型配置（yaml 字段名 → 值）：
// 校验 → 行级写回 hyperbot.yaml → flush 整体重跑装配。仅 config 态可调用；
// 成功后引擎进入 ready，TUI 侧横幅与本页关闭由返回值驱动。
func (e *Engine) ApplyFirstStartupConfig(vals map[string]string) error {
	e.mu.Lock()
	state := e.bootState
	e.mu.Unlock()
	if state != BootConfigNeeded {
		return fmt.Errorf("engine is not waiting for first startup config")
	}
	if err := validateModelForm(vals); err != nil {
		return err
	}
	if err := config.PatchModelFields(e.HyperBotConfigPath, normalizeModelForm(vals)); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	if err := e.flush(); err != nil {
		// 写回成功但装配失败：引擎已不可用，置终态交给 TUI 任意键退出。
		// 不能 parkWithFatal：本方法在 TUI 事件循环 goroutine 上调用，驻留会冻死渲染。
		e.setFatal(FatalError, err.Error(), true)
		return err
	}
	e.newSessionID()
	e.firstStartupFields = nil
	e.setBootState(BootReady)
	e.setStartupInfo(e.startupInfoLines())
	return nil
}

// modelFormKeys 配置页可提交的字段白名单（yaml 字段名）。
var modelFormKeys = []string{"model", "baseurl", "apikey", "apitype"}

// validateModelForm 必填与枚举校验；白名单外的键忽略（前向兼容）。
func validateModelForm(vals map[string]string) error {
	for _, k := range modelFormKeys {
		if strings.TrimSpace(vals[k]) == "" {
			return fmt.Errorf("model config field %q is required", k)
		}
	}
	switch strings.ToLower(strings.TrimSpace(vals["apitype"])) {
	case "openai", "anthropic":
	default:
		return fmt.Errorf("apitype must be openai or anthropic, got %q", vals["apitype"])
	}
	return nil
}

// normalizeModelForm 裁剪空白并统一 apitype 小写。
func normalizeModelForm(vals map[string]string) map[string]string {
	out := make(map[string]string, len(vals))
	for _, k := range modelFormKeys {
		out[k] = strings.TrimSpace(vals[k])
	}
	out["apitype"] = strings.ToLower(out["apitype"])
	return out
}

// Run 启动引擎完整生命周期：先同步完成 Init，再启动主循环 AgentStart（goroutine 由其内部负责）。
// 仅当 Init 走到致命终态（parkWithFatal）时才驻留当前 goroutine，否则返回时引擎已后台运行。
func (e *Engine) Run() {
	e.Init()
	e.AgentStart()
}

// absorb 把 bootstrap 检查产物（路径、日志句柄）拷入引擎字段，
// 并上屏启动期收集的非致命提示。运行时对象的装配在 Init 后续步骤完成。
func (e *Engine) absorb(env *BootEnv) {
	e.CWD = env.CWD
	e.ExeDir = env.ExeDir
	e.ConfigFolderPath = env.ConfigFolderPath
	e.HyperBotConfigPath = env.HyperBotConfigPath
	e.SkillFolderPath = env.SkillFolderPath
	e.FrameworkLogFile_p = env.LogFile
	for _, n := range env.Notices {
		e.setNotice(n.Kind, n.Text)
	}
}

// parkWithFatal 置进程终态并永久驻留当前 goroutine；渲染、等待按键、停循环由 TUI 完成。
// 不能用 os.Exit 替代：TUI 回调里硬退出会把终端留在 alt-screen + raw mode；
// 也不能带着未初始化状态继续跑（如 loadConfig 失败时 Config_p 仍是 nil，往下走会 panic）。
func (e *Engine) parkWithFatal(style string, text string, waitKey bool) {
	e.setFatal(style, text, waitKey)
	select {}
}

// AgentStart 启动引擎主循环（goroutine 内读用户输入、分类分发，直到退出），
// 调用方无需自行起 goroutine。错误预算的状态流转见 errorBudget。
func (e *Engine) AgentStart() {
	go e.agentLoop()
}

// agentLoop 主循环本体：读用户输入、分类分发，直到退出。
func (e *Engine) agentLoop() {
	e.newSessionID()
	// 启动横幅只在此组装一次（bootstrap/newRunner 均已完成）。
	// 首次启动例外：此时还是模板配置，等 ApplyFirstStartupConfig 提交后
	// 以真实值组装（本函数此刻可能还没跑到，gate 保证不抢在配置页前面）。
	if e.bootStateSnapshot() == BootReady {
		(*e).setStartupInfo((*e).startupInfoLines())
	}
	for {
		line := <-(*e).inputCh
		(*e).errBudget.recharge() //任何用户输入都充值（空输入也是，无害）

		if strings.TrimSpace(line) == "" { //空输入，重新等待（前端已兜底，双保险）
			continue
		}
		//斜杠命令不经过这里：语义与注册表在前端（service/tui/handler.go），后端只暴露能力接口
		(*e).appendTyped(RecUser, line)
		e.turn(line)
	}
}

// ── 斜杠命令的后端能力接口（前端注册表见 service/tui/handler.go）──────

// Shutdown /exit 的引擎侧收尾：释放 runner 与 MCP 连接。置终态与退出由前端接管（tea.Quit）。
func (e *Engine) Shutdown() {
	if (*e).AgentRunner_p != nil {
		(*(*e).AgentRunner_p).Runner.Close()
	}
	for _, toolset := range (*e).mcpToolsets {
		toolset.Close()
	}
}

// NewConversation /new：重置 SessionID 开新对话。running 时拒绝：
// SessionId 读写无锁，turn 进行中重置是 data race，这里显式兜底。
func (e *Engine) NewConversation() bool {
	if e.isRunning() {
		e.setNotice(NoticeWarning, "agent is running, try /new later")
		return false
	}
	(*e).appendTyped(RecSlash, "/new")
	e.newSessionID()
	(*e).setNotice(NoticeNewConversation, "")
	return true
}

// ClearRecords /clear：丢弃全部消息记录（连命令本身也不留痕）。running 拒绝同 NewConversation。
func (e *Engine) ClearRecords() bool {
	if e.isRunning() {
		e.setNotice(NoticeWarning, "agent is running, try /clear later")
		return false
	}
	e.clearRecords()
	e.setNotice(NoticeClear, "")
	return true
}

func (e *Engine) newSessionID() {
	(*(*e).AgentRunner_p).SessionId = uuid.New().String()

}

// startupInfoLines 拼出启动横幅的信息行（label 补齐到 13 列），宽度截断交给 TUI。
// 调用时机在 AgentStart 第一轮，此时 bootstrap/newRunner/newSessionID 均已完成。
func (e *Engine) startupInfoLines() []string {
	cfg := (*e).Config_p.Model
	cwd := (*e).CWD
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(cwd, home) {
		cwd = "~" + cwd[len(home):]
	}
	sid := (*(*e).AgentRunner_p).SessionId
	if len(sid) > 8 {
		sid = sid[:8]
	}
	// SkillRepo 可能为 nil（loadSkills 忽略了错误），不判空会 panic
	skills := 0
	if (*e).SkillRepo != nil {
		skills = len((*e).SkillRepo.Summaries())
	}
	host, err := url.Parse(cfg.BaseURL)
	if err != nil {
		host = &url.URL{
			Host: "",
		}
	}
	return []string{
		fmt.Sprintf("model        %s %d", cfg.Model, cfg.ContextWindow),
		fmt.Sprintf("endpoint     %s", host.Host),
		fmt.Sprintf("cwd          %s", cwd),
		fmt.Sprintf("tools        %d · skills %d · mcp %d",
			len((*e).builtinTools)+len((*e).builtinToolsets),
			skills,
			len((*e).mcpToolsets)),
		fmt.Sprintf("session      %s", sid),
	}
}
