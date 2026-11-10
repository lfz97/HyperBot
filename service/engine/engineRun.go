package engine

import (
	"context"
	"fmt"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ---------- 一轮对话 ----------

type AgentError struct {
	Error         error
	ErrorType     string
	PartialOutput string
}

// turn 跑一轮对话：调 runner 流式执行，失败时按错误预算自动重试（纠正 prompt + 退避）；
// 预算耗尽则交还用户，等下一次输入充值。中断（Esc→Interrupt）不充值：已注册的 run
// 由 runner.Cancel 立即终止；注册表空窗（退避间隙）的取消意图由 pendingCancel
// 在下一个 Run 边界生效（见 uistate.go Interrupt）。
// 占坑（running）与 requestID 记账均由 SubmitInput 同步完成，这里只负责执行与收尾。
func (e *Engine) turn(prompt string, reqID string) {
	// defer 复位是 panic 安全：runOnce 跑的是框架代码，panic 时指示器不能永远转下去。
	// endTurn 单临界区清 pendingCancel+running 的理由见其注释。
	defer (*e).endTurn()

	for {
		if (*e).consumePendingCancel() {
			// 取消意图兑现。保留的负载场景是错误预算退避 sleep：上一轮 run 已注销、
			// 下一轮未发起，Cancel 查无此 run——这是秒级空窗，且旧实现（ctx 桥接）
			// 覆盖过此处，删掉即 Esc 在重试期失效（退避里的模型调用照跑）。
			// 注册前 <3ms 窗口只是顺带覆盖，不是保留理由。
			(*e).setNotice(NoticeCancelled, "")
			return
		}
		// reqID 请求粒度（SubmitInput 铸造，错误重试沿用）：失败的 run 在 Run
		// 返回错误前已注销，同 ID 复用无冲突；极端竞速撞上上一轮收尾的
		// "already running" 会走错误预算自愈（退避 3s 远大于收尾时长）。
		err := e.agentRunOnce(context.Background(), prompt, reqID)
		if err == nil { //正常结束，或被用户取消
			// 取消的提示兜底：完成事件（RunOutcome）的投递会被已取消的 ctx
			// 掐断（框架 EmitEventWithTimeout 在 ctx done 后丢事件，实测 WARN
			// 可见），runOnce 里的 RunOutcome 检测因此不可靠。Interrupt 置位的
			// pendingCancel 是取消意图的可靠记账，在成功分支消费它：
			// run 被取消 → 此刻必然残留 true；正常完成 → 无残留不误报。
			if (*e).consumePendingCancel() {
				(*e).setNotice(NoticeCancelled, "")
			}
			return
		}
		n, exhausted := (*e).errBudget.fail()
		if exhausted {
			// 放弃自动重试，把控制权交还用户：SubmitInput 回到可占坑状态，下一次提交
			// 用户输入时充值。预算不在这里充值——耗尽只可能由零输出失败触发。
			(*e).appendTyped("error", fmt.Sprintf("failed %d times in a row, auto-retry stopped. Check network/config and try again.", n))
			return
		}
		// 必须在 Sleep 之前打：sleep 期间引擎不收输入，用户需要知道程序在等什么。
		(*e).appendTyped("warn", fmt.Sprintf("retrying in %ds (attempt %d/%d)...", (*e).errBudget.backoff()/time.Second, n, (*e).errBudget.limit()))
		time.Sleep((*e).errBudget.backoff())
		// 纠正 prompt：把错误与已产出的部分输出喂回去，让模型调整后续输出。
		if err.PartialOutput != "" {
			prompt = fmt.Sprintf("之前的对话发生了错误，错误信息是: %s, 之前的输出内容是: %s, 请基于这些信息调整你的回答并继续完成对话", err.Error, err.PartialOutput)
		} else {
			prompt = fmt.Sprintf("之前的对话发生了错误，错误信息是: %s, 请基于这个信息调整你的回答并继续完成对话", err.Error)
		}
	}
}

// ---------- runner 桥接 ----------

// agentRunOnce 把一个 prompt 交给 runner 执行：流式事件转发给 TUI、
// 处理取消、收集失败前的部分输出。返回 nil 表示成功或被取消。
// reqID 由调用方（turn）铸造：WithRequestID 传入后 runner 把它注入每个
// 事件的 event.RequestID，并据此支持 Cancel/RunStatus（见 Interrupt）。
func (e *Engine) agentRunOnce(Ctx context.Context, userPrompt string, reqID string) *AgentError {
	eventChan, err := (*(*e).AgentRunner_p).Runner.Run(
		Ctx,
		(*(*e).Config_p).User.UserID,
		(*(*e).AgentRunner_p).SessionId,
		model.Message{
			Role:    model.RoleUser,
			Content: userPrompt,
		},
		agent.WithRequestID(reqID), //调用方记账：中断/状态查询按此 id 定位本 run
		agent.WithToolCallArgumentsJSONRepairEnabled(true), //开启工具调用参数的JSON修复功能，解决因模型输出格式不规范导致的工具调用失败问题
		// 开启 pre-LLM 延迟诊断 span（含 compaction 的 summary/rebuild 两段）：
		// Langfuse 只收 OTel span，不开则压缩阶段在 trace 里是黑盒。
		// 事件开关显式关掉——诊断 span 已足够，不要往事件流里灌状态事件。
		agent.WithLatencyDiagnostics(true),
		agent.WithLatencyDiagnosticsEvents(false),
	)
	if err != nil {
		err = fmt.Errorf("AgentRunner.Run failed: %v", err)
		// 在源头打印，与下面的 TerminalError 分支保持一致：每类错误只打一次。
		(*e).appendTyped("error", err.Error())
		return &AgentError{
			Error:         err,
			ErrorType:     "RunError",
			PartialOutput: "",
		}
	}

	partialOutput := ""
	for event := range eventChan {
		//只有terminal error才会中断对话，其他error直接continue
		if event.Error != nil {
			if event.IsTerminalError() {
				err = fmt.Errorf("event returned TerminalError: %v", event.Error)
				(*e).appendTyped("error", err.Error())
				return &AgentError{
					Error:         err,
					ErrorType:     "TerminalError",
					PartialOutput: partialOutput,
				}
			}
			continue

		}
		if (*event).Response != nil && len((*(*event).Response).Choices) > 0 {

			(*e).errBudget.recharge() // 收到任何带 Choices 的 Response 事件（含流式部分块）即归零。
			for _, choice := range (*(*event).Response).Choices {

				(*e).emitChoice(choice, (*(*event).Response).IsPartial)
				gatherPartialOutput(&partialOutput, choice, (*(*e).AgentRunner_p).Stream)
			}

		}
		if event.IsRunnerCompletion() {
			// 取消检测挂到框架的完成事件上（原 ctx.Done 分支已随桥接删除）：
			// RunOutcome 只在 run 被显式取消（Cancel）或超时时非 nil。尽力而为
			// ——取消后完成事件的投递可能被已取消的 ctx 掐断（框架 WARN 可见），
			// 可靠兜底见 turn 成功分支的 pendingCancel 消费。
			if event.RunOutcome != nil {
				(*e).setNotice(NoticeCancelled, "")
			}
			break
		}

	}

	return nil

}

// gatherPartialOutput 收集失败前已产出的部分输出，供重试时喂回模型。
// 不能靠框架 session 代替：流式 delta 一律不落盘，中途失败时本轮 assistant 文本在
// session 里一个字都没有（框架的补救机制默认关闭，且只在 ctx 取消时触发，覆盖不到
// TerminalError），这里是唯一记录。只累积 Content，不含 ReasoningContent / ToolCalls。
func gatherPartialOutput(Container_p *string, Choice model.Choice, Stream bool) {
	if Stream {
		*Container_p += Choice.Delta.Content
	} else {
		*Container_p += Choice.Message.Content
	}
}
