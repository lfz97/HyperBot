package handler

import (
	"HyperBot/utils/pretty"
	"context"
	"fmt"

	"github.com/rivo/tview"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

type AgentError struct {
	Error      error
	ErrorType  string
	OutputPart string
}

func AgentRunOnce(Ctx context.Context, app_p *tview.Application, view_p *tview.TextView, r AgentRunner, sessionID string, userID string, requestID string, userPrompt string) *AgentError {

	eventChan, err := r.Runner.Run(Ctx, userID, sessionID, model.Message{
		Role:    model.RoleUser,
		Content: userPrompt,
	}, agent.WithRequestID(requestID))
	if err != nil {
		return &AgentError{
			Error:      fmt.Errorf("AgentRunner.Run发生错误: %v", err),
			ErrorType:  "RunError",
			OutputPart: "",
		}
	}

	OutputPart := ""
	startReasoning := false
	for event := range eventChan {
		//只有terminal error才会中断对话，其他error直接continue
		if event.Error != nil {
			if event.IsTerminalError() {
				//填充err，使得返回的err不为nil，表示对话发生了错误
				err = fmt.Errorf("Event发生TerminalError: %v", event.Error)
				app_p.QueueUpdateDraw(func() {
					fmt.Fprint(view_p, pretty.TErrorF("Event发生TerminalError: %v", err))
					view_p.ScrollToEnd()
				})
				return &AgentError{
					Error:      err,
					ErrorType:  "TerminalError",
					OutputPart: OutputPart,
				}
			} else {
				continue
			}

		}
		select {
		case <-Ctx.Done():
			app_p.QueueUpdateDraw(func() {
				fmt.Fprint(view_p, pretty.TCancelled())
				view_p.ScrollToEnd()
			})
			return nil

		default:
		}
		if event.Response != nil && len((*(*event).Response).Choices) > 0 {

			Choice := (*(*event).Response).Choices[0]
			printMessage(app_p, view_p, Choice, &startReasoning, r.Stream)
			gatherContentMessage(&OutputPart, Choice, r.Stream)

		}
		// event.IsRunnerCompletion()判断是否完成输出
		if event.IsRunnerCompletion() {
			break
		}

	}

	return nil

}
