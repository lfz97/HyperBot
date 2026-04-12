package handler

import (
	"HyperBot/utils/pretty"
	"context"
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// 交互式对话
func AgentRunIteratively(Ctx context.Context, app_p *tview.Application, AgentMessageView_p *tview.TextView, InputArea_p *tview.TextArea, AgentRunner AgentRunner, sessionID string, userID string, requestID string, inputContext TurnResult) *TurnResult {
	Ctx, cancel := context.WithCancel(Ctx)
	defer cancel()
	//根据传入消息的类型输出不同提示语
	if inputContext.Code == New {
		app_p.QueueUpdateDraw(func() {
			fmt.Fprint(AgentMessageView_p, pretty.TNewConversation())
			AgentMessageView_p.ScrollToEnd()
		})
	} else if inputContext.Code == Error {
		app_p.QueueUpdateDraw(func() {
			fmt.Fprint(AgentMessageView_p, pretty.TErrorF("对话发生错误: %s", inputContext.Reason))
			AgentMessageView_p.ScrollToEnd()
		})
	} else if inputContext.Code == Int { //对话因中断信号而中断,不输出提示语
	}

	var userPrompt string
	for {
		//如果是新对话、继续对话或中断后恢复，用户自行输入prompt
		if inputContext.Code == New || inputContext.Code == Continue || inputContext.Code == Int {
			done := make(chan struct{})
			app_p.QueueUpdateDraw(func() {
				app_p.SetFocus(InputArea_p)
				//注册一个输入捕获器，每次用户在输入框敲击键盘时都会触发
				InputArea_p.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
					//当用户按下回车键时，获取输入内容，清空输入框，并发送信号继续执行后续逻辑
					if event.Key() == tcell.KeyEnter && event.Modifiers() == 0 {
						userPrompt = InputArea_p.GetText()
						InputArea_p.SetText("", false)
						done <- struct{}{} // 发送信号，继续执行后续逻辑
						return nil         // 吞掉回车事件，避免它被输入框处理成换行
					}
					//其他按键正常传递并通过return event显示在输入框中
					return event
				})
			})

			<-done //通过等待信号的方式阻塞代码，直到用户输入完成

			app_p.QueueUpdateDraw(func() {
				fmt.Fprint(AgentMessageView_p, pretty.TUserInput(userPrompt))
				AgentMessageView_p.ScrollToEnd()
			})

			{
				if userPrompt == "/exit" {

					return &TurnResult{
						Code:   Exit,
						Reason: "用户主动结束对话",
					}

				} else if userPrompt == "/new" {

					return &TurnResult{
						Code:   New,
						Reason: "用户主动开始新对话",
					}
				} else if userPrompt == "" {
					continue //如果用户输入为空，重新开始本轮循环，等待用户输入

				} else {
					break //正常输入，继续执行后续逻辑
				}
			}

		} else if inputContext.Code == Error {
			userPrompt = "继续"
			break
		}
	}

	// 注册一个全局的输入捕获器，监听ESC键以取消后续agent的输出。
	app_p.QueueUpdateDraw(func() {
		app_p.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			if event.Key() == tcell.KeyEscape {
				cancel() // 取消 context
				return nil
			}
			return event // 其他按键正常传递
		})
	})
	// 函数返回前清除全局捕获器，避免ESC事件被持续拦截
	defer app_p.QueueUpdateDraw(func() {
		app_p.SetInputCapture(nil)
	})

	// AgentRunOnce返回的消息包含本次对话输入输出的所有消息
	err := AgentRunOnce(Ctx, app_p, AgentMessageView_p, AgentRunner, sessionID, userID, requestID, userPrompt)
	if err != nil { //如果运行过程中发生错误
		return &TurnResult{
			Code:   Error,
			Reason: fmt.Sprintf("对话过程中发生错误: %v", err),
		}
	}

	//如果ctx被取消，则设置结束状态为中断
	select {
	case <-Ctx.Done():
		app_p.QueueUpdateDraw(func() {
			fmt.Fprint(AgentMessageView_p, pretty.TInterrupted())
			AgentMessageView_p.ScrollToEnd()
		})
		return &TurnResult{
			Code:   Int,
			Reason: "会话已取消，停止接收输入",
		}
	default:
	}

	//单轮对话正常结束，设置状态为continue，session自动维护历史
	return &TurnResult{
		Code:   Continue,
		Reason: "单轮对话正常结束",
	}

}
