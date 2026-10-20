package engine

import (
	"fmt"
	"net/url"
	"os"
	"strings"

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
	}
}

// Init 完成环境检查与资产装配（bootstrap），随后构造 runner。
func (e *Engine) Init() {
	env, err := Check(e.Agentname, e)
	if err != nil {
		e.parkWithFatal(FatalError, err.Error(), true)
		return
	}
	if env.NeedRestart {
		// 首跑创建了默认配置文件：须改完配置重启，本次启动到此为止。
		e.parkWithFatal(FatalSuccess, "Config file not found; a default one has been created. Please review it and restart.", true)
		return
	}
	e.absorb(env)
	e.newRunner()
}

// Run 启动引擎完整生命周期：先同步完成 Init，再启动主循环 AgentStart（goroutine 由其内部负责）。
// 仅当 Init 走到致命终态（parkWithFatal）时才驻留当前 goroutine，否则返回时引擎已后台运行。
func (e *Engine) Run() {
	e.Init()
	e.AgentStart()
}

// absorb 把 bootstrap 产物拷入引擎字段，并上屏启动期收集的非致命提示。
func (e *Engine) absorb(env *BootEnv) {
	e.CWD = env.CWD
	e.ExeDir = env.ExeDir
	e.ConfigFolderPath = env.ConfigFolderPath
	e.HyperBotConfigPath = env.HyperBotConfigPath
	e.SkillFolderPath = env.SkillFolderPath
	e.FrameworkLogFile_p = env.LogFile
	e.Config_p = env.Config
	e.Systemprompt = env.Systemprompt
	e.SessionService_p = env.SessionService
	e.SqliteMemoryService = env.MemoryService
	e.builtinTools = env.BuiltinTools
	e.builtinToolsets = env.BuiltinToolsets
	e.SkillRepo = env.SkillRepo
	e.publishSkillItems()
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
	(*e).setStartupInfo((*e).startupInfoLines())
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
