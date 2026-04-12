package handler

import (
	"HyperBot/utils/pretty"
	"context"
	"fmt"

	"github.com/rivo/tview"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func AgentRunOnce(Ctx context.Context, app_p *tview.Application, view_p *tview.TextView, r AgentRunner, sessionID string, userID string, requestID string, userPrompt string) error {

	eventChan, err := r.Runner.Run(Ctx, userID, sessionID, model.Message{
		Role:    model.RoleUser,
		Content: userPrompt,
	}, agent.WithRequestID(requestID))
	if err != nil {
		return err
	}

	startReasoning := false

	for event := range eventChan {

		if event.Error != nil {
			err = fmt.Errorf("获取Event时发生错误: %v", event.Error)
			app_p.QueueUpdateDraw(func() {
				fmt.Fprint(view_p, pretty.TErrorF("对话发生错误: %v", err))
				view_p.ScrollToEnd()
			})
			break
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
		if len((*(*event).Response).Choices) > 0 {

			Choice := (*(*event).Response).Choices[0]
			printMessage(app_p, view_p, Choice, &startReasoning, r.Stream)

		}
		// event.IsRunnerCompletion()判断是否完成输出
		if event.IsRunnerCompletion() {
			break
		}

	}

	return err

}
