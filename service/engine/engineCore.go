package engine

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/google/uuid"
)

// GetEngineService 创建引擎实例（轻量：只装配字段，不触盘、不阻塞）。
// 生命周期统一由 Run 驱动（见 Run 的注释），调用方不再自行编排初始化序列。
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
// 检查器只返回错误与产物；致命呈现（parkWithFatal）是 Engine 的职责。
func (e *Engine) Init() {
	env, err := Check(e.Agentname, e)
	if err != nil {
		e.parkWithFatal(FatalError, err.Error(), true)
		return
	}
	if env.NeedRestart {
		// 首跑创建了默认配置文件：须改完配置重启，本次启动到此为止。
		e.parkWithFatal(FatalSuccess, "检查到配置文件不存在，已创建默认配置文件。请根据实际情况修改配置文件后重新启动程序！", true)
		return
	}
	e.absorb(env)
	e.newRunner()
}

// Run 启动引擎的完整生命周期：先同步完成初始化（Init），再启动主循环
// （AgentStart）。主循环的 goroutine 由 AgentStart 内部负责，正常路径下
// Run 返回时引擎已在后台自主运行；仅当 Init 走到致命终态（parkWithFatal）
// 时才驻留当前 goroutine。
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

// parkWithFatal 置进程终态并永久驻留当前 goroutine。
// 旧版等价物是 TuiService.ShowXxxAndExit 内部末尾的 select{}，语义必须原样保留：
//   - 引擎 init 序列打完致命消息后绝不能带着未初始化状态继续往下跑（loadConfig 失败
//     时 Config_p 仍是 nil，继续走 init 会在别处 panic）；
//   - 不能在 TUI 回调里 os.Exit——screen.Fini() 在 app.Run() 返回路径上调用，
//     硬退出会把终端留在 alt-screen + raw mode。
//
// pull 之后的分工：引擎置终态 + 驻留；渲染、等待按键、停循环由 TUI 完成。
func (e *Engine) parkWithFatal(style string, text string, waitKey bool) {
	e.setFatal(style, text, waitKey)
	select {}
}

// AgentStart 启动引擎主循环：goroutine 内读用户输入、分类分发，直到退出。
// 阻塞语义收敛在引擎内部，调用方无需自行起 goroutine；一轮对话在 turn 里跑
// （含自动重试），错误预算的状态流转见 errorBudget。
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
		//斜杠命令不经过这里：语义与注册表在前端（service/tui/handler.go 的
		// dispatchSlash），后端只暴露能力接口——字符串 magic word 已废除。
		(*e).appendTyped(RecUser, line)
		e.turn(line)
	}
}

// ── 斜杠命令的后端能力接口（前端注册表见 service/tui/handler.go）──────

// Shutdown /exit 的引擎侧收尾：释放 runner 与 MCP 连接。置终态与退出由
// 前端接管（tea.Quit 直接结束进程），引擎不再依赖“观察 fatal”的退出路径。
func (e *Engine) Shutdown() {
	if (*e).AgentRunner_p != nil {
		(*(*e).AgentRunner_p).Runner.Close()
	}
	for _, toolset := range (*e).mcpToolsets {
		toolset.Close()
	}
}

// NewConversation /new：重置 SessionID 开新对话。running 时拒绝——
// SessionId 读写无锁，turn 进行中重置是 data race；旧版命令经 inputCh
// 排队自然避开，命令前移到前端后这里显式兜底，拒绝即最小正确实现。
func (e *Engine) NewConversation() bool {
	if e.isRunning() {
		e.setNotice(NoticeWarning, "对话进行中，请稍后再开新对话")
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
		e.setNotice(NoticeWarning, "对话进行中，请稍后再清屏")
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
