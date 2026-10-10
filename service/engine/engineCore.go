package engine

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

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

// Init 完成 preCheckLoad 与 newRunner。
func (e *Engine) Init() {
	(*e).preCheckLoad()
	(*e).newRunner()
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

func (e *Engine) AgentStart() {
	// 初始用 Startup 而不是 New：程序刚启动时并不存在"上一轮对话"，
	// 推一条"新对话已开始"到 NoticeBar 是噪音。New 只留给用户真的敲 /new 的场合。
	MsgContext := turnInfo{
		Code:          Startup,
		Reason:        "程序启动",
		PartialOutput: "",
	}
	e.newSessionID()
	for {
		EndTurn_p := e.agentRunIteratively(context.Background(), MsgContext)
		if (*EndTurn_p).Code == Exit { //用户主动结束对话，退出程序
			//关闭AgentRunner，释放资源
			(*(*e).AgentRunner_p).Runner.Close()
			for _, toolset := range (*e).mcpToolsets {
				toolset.Close()
			}
			// 置终态并永久驻留（无文案）：TUI 观察到 fatal 后直接退出，
			// main() 随 Run() 返回——引擎不再知道终端的存在。
			(*e).parkWithFatal(FatalExit, "", false)

		} else if (*EndTurn_p).Code == New { //用户开始新对话：重置 SessionID，更新MsgContext为新对话的初始状态
			// 错误计数不在这里归——输入 /new 本身已走过 agentRunIteratively 的
			// 用户输入充值点，且 New 之后到下一次用户输入之间不存在 fail 路径
			e.newSessionID()
			MsgContext = turnInfo{
				Code:          New,
				Reason:        "新对话",
				PartialOutput: "",
			}

		} else if (*EndTurn_p).Code == Error { //出错：累加连续错误计数，未达上限则退避后自动重试
			n, exhausted := (*e).errBudget.fail()
			if exhausted {
				// 由于此时错误计数已到达上限，agentRunIteratively内将不会自动重试，需要用户输入，这里打印一条提示消息。
				(*e).appendTyped("error", fmt.Sprintf("连续 %d 次失败，已停止自动重试。请检查网络/配置后重新输入。", n))
				MsgContext = *EndTurn_p
				continue
			}
			// 必须在 Sleep 之前打：sleep 期间引擎 goroutine 阻塞、不监听 inputChan，
			// 用户打字没有反应，需要知道程序在等什么。
			(*e).appendTyped("warn", fmt.Sprintf("%d 秒后重试（第 %d/%d 次）...", (*e).errBudget.backoff()/time.Second, n, (*e).errBudget.limit()))
			time.Sleep((*e).errBudget.backoff())
			MsgContext = *EndTurn_p

		} else {
			(*e).errBudget.recharge() //其他任何情况返回，错位计数都归零
			MsgContext = *EndTurn_p
			continue
		}

	}
}

func (e *Engine) newSessionID() {
	(*(*e).AgentRunner_p).SessionId = uuid.New().String()

}

// startupInfoLines 拼出启动横幅的信息行（label 补齐到 13 列），宽度截断交给 TUI。
// 调用时机在 AgentStart 第一轮，此时 preCheckLoad/newRunner/newSessionID 均已完成。
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
