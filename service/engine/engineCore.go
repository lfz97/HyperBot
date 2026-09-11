package engine

import (
	"HyperBot/service/engine/requirements"
	"HyperBot/utils/pretty"
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// 连续错误自动重试策略。达到上限后不再自动重试，把控制权交还用户。
	// 用常量而非 yaml 配置：目前没有按实例调整的需求，将来要配再提升为 Engine 字段。
	errorMaxTimes = 3
	errorSleepGap = 3 * time.Second
)

func GetEngineService(name string, tui requirements.TuiService) *Engine {
	e := &Engine{
		tui: tui,
	}
	(*e).Agentname = name
	(*e).preCheckLoad()
	(*e).newRunner()
	return e
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
			(*e).tui.ShowMsgAndExitNoTrigger(pretty.TExit("对话已结束，感谢使用！后会有期！"))

		} else if (*EndTurn_p).Code == New { //用户开始新对话，重置 SessionID 与错误计数，更新MsgContext为新对话的初始状态
			// /new 在 agentRunIteratively 的输入分支里是提前 return 的，走不到"用户提交
			// 非空输入"那处归零，所以必须在这里单独归
			(*e).errorStreak = 0
			e.newSessionID()
			MsgContext = turnInfo{
				Code:          New,
				Reason:        "新对话",
				PartialOutput: "",
			}

		} else if (*EndTurn_p).Code == Error { //出错：累加连续错误计数，未达上限则退避后自动重试
			(*e).errorStreak++
			if (*e).errorStreak >= errorMaxTimes {
				// 放弃自动重试，把控制权交还用户。三个要点：
				// ① Code 保持 Error —— Int 的语义是"用户按了 ESC 中断"，与事实不符，
				//    不能为了蹭"回到输入循环"这个副作用而填一个假状态码。真正让下一轮
				//    等用户输入的是 agentRunIteratively 里的 errorStreak < errorMaxTimes 判定。
				// ② errorStreak 不归零 —— 归零会让下一轮重新满足自动重试条件，无限循环照旧。
				//    它只在成功/新对话/中断/用户手动提交输入时归零。
				// ③ 整个复用 *EndTurn_p，不新造 literal —— 新建会静默丢掉 Reason 与
				//    PartialOutput（TerminalError 时后者是真实累积到的部分输出）。
				(*e).tui.PrintToMsgView(pretty.TErrorF("连续 %d 次失败，已停止自动重试。请检查网络/配置后重新输入。", errorMaxTimes), false)
				MsgContext = *EndTurn_p
				continue
			}
			// 必须在 Sleep 之前打：sleep 期间引擎 goroutine 阻塞、不监听 inputChan，
			// 用户打字没有反应，需要知道程序在等什么。
			(*e).tui.PrintToMsgView(pretty.TWarningF("%d 秒后重试（第 %d/%d 次）...", errorSleepGap/time.Second, (*e).errorStreak, errorMaxTimes), false)
			time.Sleep(errorSleepGap)
			MsgContext = *EndTurn_p

		} else { //其他情况（Continue 正常结束 / Int 用户中断），错误链断开、计数归零；继续使用当前的 SessionID 与 UserID，更新MsgContext为当前对话的结束状态，供下一轮对话使用
			// 归零在这里覆盖了两个时机：Continue（自动重试链里第 2 次尝试成功时靠它收口，
			// 否则 streak 会留着，下次失败只剩 2 次预算）与 Int（ESC 打断，人已介入）
			(*e).errorStreak = 0
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
