package engine

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/google/uuid"
)

// GetEngineService 创建引擎实例（轻量：只装配字段，不触盘、不阻塞）。
// 初始化由调用方在 goroutine 里调 Init()——初始化失败会 parkWithFatal 永久驻留，
// 若在 TUI 启动前同步执行会卡死整个进程（终态都无从渲染）。
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

// absorb 把 bootstrap 产物拷入引擎字段，并上屏启动期收集的非致命提示。
func (e *Engine) absorb(env *BootEnv) {
	e.CWD = env.CWD
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

// AgentStart 启动引擎主循环：读用户输入、分类分发，直到退出。
// 一轮对话在 turn 里跑（含自动重试）；错误预算的状态流转见 errorBudget。
func (e *Engine) AgentStart() {
	e.newSessionID()
	// 启动横幅只在此组装一次（bootstrap/newRunner 均已完成）。
	(*e).setStartupInfo((*e).startupInfoLines())
	for {
		cmd := parseInput(<-(*e).inputCh)
		(*e).errBudget.recharge() //任何用户输入都充值（斜杠/空输入也是，无害）

		switch cmd.Kind {
		case cmdExit: //用户主动结束对话：释放资源，置终态并永久驻留
			(*e).appendTyped("slash", cmd.Prompt)
			//关闭AgentRunner，释放资源
			(*(*e).AgentRunner_p).Runner.Close()
			for _, toolset := range (*e).mcpToolsets {
				toolset.Close()
			}
			// 置终态并永久驻留（无文案）：TUI 观察到 fatal 后直接退出，
			// main() 随 Run() 返回——引擎不再知道终端的存在。
			(*e).parkWithFatal(FatalExit, "", false)

		case cmdNew: //用户开始新对话：重置 SessionID
			(*e).appendTyped("slash", cmd.Prompt)
			e.newSessionID()
			(*e).setNotice(NoticeNewConversation, "")

		case cmdPrompt: //普通对话输入
			(*e).appendTyped("user", cmd.Prompt)
			e.turn(cmd.Prompt)

		case cmdEmpty: //空输入，重新等待
		}
	}
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
