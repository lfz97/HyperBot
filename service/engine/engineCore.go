package engine

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"HyperBot/service/engine/config"
	"HyperBot/service/engine/observability"

	"github.com/google/uuid"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// GetEngineService 创建引擎实例（轻量：只装配字段，不触盘）。
// 启动入口是 Init（boot.go 以 goroutine 异步调用）：成功返回即完全就绪。
func GetEngineService(name string) *Engine {
	return &Engine{
		Agentname: name,
		notice:    notice{Kind: NoticeNone},
		bootState: BootBooting,
		// retry 不在此构造：每轮 turn 由 SubmitInput 重铸（跨轮零残留），
		// 见 uistate.go newTurnRetry。
	}
}

// Init 完成环境检查（bootstrap）与运行时装配。**成功返回即引擎完全就绪**：
// 会话 ID、启动横幅、boot 态一步到位，调用方（boot.go）无需 post-Init 收尾，
// 也无需处理错误返回。检查与装配的分工见 bootstrap.go / assemble.go。
// 首次启动（Check 创建了默认配置）不再驻留退出：引擎照常装配模板配置，
// 置 bootState=config，由 TUI 弹配置页收集模型配置后 ApplyFirstStartupConfig
// 收尾（会话 ID 与横幅由它以真实配置生成——config 态在此不生成，配置页是
// 模态的、无对话）。
// 错误路径 setFatal 后返回 error（渲染与退出由 TUI 完成）：各路径的显式
// setFatal 保留（提前置位更早可见），defer 幂等覆盖写兜底防漏——任何
// return err 都带 fatal 终态，否则 TUI 停在 booting 黑屏挂死（原 Run 的
// 兜底职责并入此处）。
func (e *Engine) Init() (err error) {
	// 幂等兜底（挪自原 Run）：漏设 fatal 的错误路径会让 TUI 停在 booting
	// 黑屏挂死。setFatal 是覆盖写，显式路径已设过时无害。
	defer func() {
		if err != nil {
			e.setFatal(FatalError, err.Error(), true)
		}
	}()
	env, err := Check()
	if err != nil {
		e.setFatal(FatalError, err.Error(), true)
		return err
	}
	e.absorb(env)
	if err := e.flush(); err != nil {
		e.setFatal(FatalError, err.Error(), true)
		return err
	}
	if (*e).Config_p.Observability.Enabled {
		obs := (*e).Config_p.Observability
		ob := observability.NewObservabilityService(observability.ServiceType(obs.Service), obs.Public, obs.Secret, obs.Host, obs.Insecure)
		if ob == nil {
			e.setNotice(NoticeWarning, fmt.Sprintf("observability service %q is not supported, tracing disabled", obs.Service))
		} else if err := ob.Start(); err != nil {
			// 软失败：观测后端不可用不阻断聊天（对比 parkWithFatal 的硬失败路径）。
			// 配置层错误（host/key 缺失等）在这里零反馈会让人以为 tracing 开着。
			e.setNotice(NoticeWarning, fmt.Sprintf("observability start failed (%v): tracing disabled", err))
		} else {
			// 成功才发布 e.ob——Shutdown 的 nil 判定因此语义准确。
			// 注意 langfuse.Start 成功 ≠ 后端可达（启动路径无网络 I/O），
			// 首批导出失败只会进 hyperbot.log（见 observability/langfuse.go
			// 的 otel.SetErrorHandler）。
			(*e).ob = ob
			e.setNotice(NoticeSuccess, "langfuse tracing enabled")
		}
	}
	if env.ConfigCreated {
		// 快照模板 model 块为配置页预填值并进入 config 态（成对写入见 setBootState）。
		e.setBootState(BootConfigNeeded, templateModelFields(e.Config_p.Model))
		return nil
	}
	// 挪自原 Run 尾段：就绪路径的会话 ID 与启动横幅在此一次完成。
	// 横幅先于 BootReady 写入——保证"观察到 Ready ⇒ 横幅已就绪"（TUI 的
	// startupOK gate 依赖此序，否则 ready 态可能空帧一轮）。
	e.newSessionID()
	e.setStartupInfo((*e).startupInfoLines())
	e.setBootState(BootReady, nil)
	return nil
}

// flush 释放上一轮装配的运行时资产后重跑 ⑥–⑪ + newRunner。
// 幂等：不改变语义状态，重复调用只是把资产按当前配置再重建一遍，结果收敛。
// 首次启动配置提交后整条装配路径重跑：引擎无需知道哪些组件快照了 config
// （session service 的 summarizer 拷贝了 config.Model 值——整体重建一次性
// 全部覆盖）。仅可在无回合进行中时调用
// （首次启动的配置页是模态的，天然满足；装配错误由调用方决定呈现方式）。
func (e *Engine) flush() error {
	e.releaseRuntime()
	if err := e.assembleConfig(); err != nil {
		return err
	}
	e.buildSystemPrompt()
	e.buildWorkerPrompt()
	e.SessionService_p = e.buildSessionService()
	if err := e.buildMemoryService(); err != nil {
		return err
	}
	if err := e.buildBuiltinAssets(); err != nil {
		return err
	}
	e.loadSkills()
	return e.newRunner()
}

// releaseRuntime 关闭当前运行时资产（nil 安全：首次调用无事可放）。
// 顺序同 Shutdown：先停 taskService（取消并等待在跑的 Worker，它们还在使用
// MCP 工具集与 localexec 作业表），再放 runner 与工具集；worker 原子指针一并
// 清空，防止 flush 间隙的 spawn 拿到引用旧一代资产的实例。
// session service 接口自带 Close（内存/SQLite 实现均满足）。
func (e *Engine) releaseRuntime() {
	if e.taskService != nil {
		e.taskService.Close()
		e.taskService = nil
	}
	e.taskTools = nil
	e.worker.Store(nil)
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
// 校验 → UpdateModelFields 读-改-写 hyperbot.yaml → flush 整体重跑装配。仅 config 态可调用；
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
	if err := config.UpdateModelFields(e.HyperBotConfigPath, normalizeModelForm(vals)); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	if err := e.flush(); err != nil {
		// 写回成功但装配失败：引擎已不可用，置终态交给 TUI 任意键退出。
		// 不能 parkWithFatal：本方法在 TUI 事件循环 goroutine 上调用，驻留会冻死渲染。
		e.setFatal(FatalError, err.Error(), true)
		return err
	}
	e.newSessionID()
	e.setBootState(BootReady, nil) // 成对写：清空预填值 + 推进状态，同一临界区（裸写 firstStartupFields 是数据竞争）
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

// Run 已删除：原职责全部并入 Init（就绪尾段 + fatal 幂等兜底，见其注释）。
// 启动入口收敛为 boot.go 的 go e.Init()——Init 成功返回即完全就绪。

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
// 不能用 os.Exit 替代：TUI 回调里硬退出会把终端留在 alt-screen + raw mode。
// 仅供无法向上返回错误的深处路径使用（loadConfig/refresh：跑在 runner 的
// 工厂 goroutine 上）；能返回错误的一律走 setFatal+return（见 Init/Run）。
func (e *Engine) parkWithFatal(style string, text string, waitKey bool) {
	e.setFatal(style, text, waitKey)
	select {}
}

// agentLoop 已删除：输入消费从"常驻循环读 channel"改为"SubmitInput 直通"。
// 它的串行化职责由 SubmitInput 的同步占坑（tryClaimRunning）承接，其余遗留
// 职责各自归位：retry 充值与用户记录在 SubmitInput，会话 ID 与横幅在 Run。

// ── 斜杠命令的后端能力接口（前端注册表见 service/tui/handler.go）──────

// Shutdown /exit 的引擎侧收尾：释放 runner 与 MCP 连接。置终态与退出由前端接管（tea.Quit）。
// taskService 最先关：它会取消并等待在跑的 Worker 退出，之后才能安全关闭
// 它们正在使用的 MCP 工具集与 localexec 作业表。
// ob 最后停：在跑 Worker 的 span 要先 End，尾批 flush 才完整（Stop 自带
// 5s 超时，Langfuse 不可达不会挂住 /exit；失败仅日志）。
func (e *Engine) Shutdown() {
	if (*e).taskService != nil {
		(*e).taskService.Close()
	}
	if (*e).AgentRunner_p != nil {
		(*(*e).AgentRunner_p).Runner.Close()
	}
	for _, toolset := range (*e).mcpToolsets {
		toolset.Close()
	}
	if (*e).ob != nil {
		if err := (*e).ob.Stop(); err != nil {
			log.Default.Warnf("observability shutdown: %v", err)
		}
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
// 调用时机在 Init 就绪尾段（newSessionID 之后、BootReady 写入之前），
// 此时 bootstrap/newRunner/newSessionID 均已完成。
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
