package handler

import (
	"HyperBot/utils/pretty"
	"fmt"
	"github.com/rivo/tview"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func printMessage(app_p *tview.Application, view_p *tview.TextView, Choice model.Choice, startReasoning *bool, stream bool) {

	if stream {
		//------------------处理流式的响应---------------------------------------------------------------------------
		if Choice.Delta.ReasoningContent != "" && !(*startReasoning) {

			app_p.QueueUpdateDraw(func() {
				fmt.Fprint(view_p, pretty.TReasoningStart())
				view_p.ScrollToEnd()
			})
			*startReasoning = true

		} else if Choice.Delta.ReasoningContent != "" && (*startReasoning) {

			// 思考内容
			app_p.QueueUpdateDraw(func() {
				fmt.Fprint(view_p, pretty.TReasoningContent(Choice.Delta.ReasoningContent))
				view_p.ScrollToEnd()
			})

		} else if Choice.Delta.ReasoningContent == "" && (*startReasoning) {
			*startReasoning = false

			app_p.QueueUpdateDraw(func() {
				fmt.Fprint(view_p, pretty.TReasoningEnd())
				view_p.ScrollToEnd()
			})

		}
		if Choice.Delta.Content != "" {
			// 正文内容
			app_p.QueueUpdateDraw(func() {
				fmt.Fprint(view_p, Choice.Delta.Content)
				view_p.ScrollToEnd()
			})
		}

	} else {
		//------------------处理非流式的响应---------------------------------------------------------------------------
		//处理思考信息 - 使用黄色
		if Choice.Message.ReasoningContent != "" {

			app_p.QueueUpdateDraw(func() {
				fmt.Fprint(view_p, pretty.TReasoningStart())
				view_p.ScrollToEnd()
			})
			app_p.QueueUpdateDraw(func() {
				fmt.Fprint(view_p, pretty.TReasoningContent(Choice.Message.ReasoningContent))
				view_p.ScrollToEnd()
			})
			app_p.QueueUpdateDraw(func() {
				fmt.Fprint(view_p, pretty.TReasoningEnd())
				view_p.ScrollToEnd()
			})

		}
		// 正文内容
		if Choice.Message.Content != "" {
			app_p.QueueUpdateDraw(func() {
				fmt.Fprint(view_p, Choice.Message.Content)
				view_p.ScrollToEnd()
			})
		}
	}

	/*------------------此处统一处理工具信息---------------------------------------------------------------------------*/

	//处理工具请求------------------------------------
	//工具请求信息不一定在delta中，也可能在message中，所以两者都要处理
	if len(Choice.Delta.ToolCalls) != 0 {
		for _, toolCall := range Choice.Delta.ToolCalls {
			app_p.QueueUpdateDraw(func() {
				fmt.Fprint(view_p, pretty.TToolCall(toolCall.Function.Name))
				fmt.Fprint(view_p, pretty.TToolArgs(string(toolCall.Function.Arguments)))
				view_p.ScrollToEnd()
			})
		}
	}

	if len(Choice.Message.ToolCalls) != 0 {
		for _, toolCall := range Choice.Message.ToolCalls {
			app_p.QueueUpdateDraw(func() {
				fmt.Fprint(view_p, pretty.TToolCall(toolCall.Function.Name))
				fmt.Fprint(view_p, pretty.TToolArgs(string(toolCall.Function.Arguments)))
				view_p.ScrollToEnd()
			})
		}
	}
	//处理工具结果------------------------------------
	//工具结果的role是tool，但信息不一定在delta中，也可能在message中，所以两者都要处理
	{
		if Choice.Delta.Role == "tool" {
			app_p.QueueUpdateDraw(func() {
				fmt.Fprint(view_p, pretty.TToolResult(Choice.Delta.Content))
				view_p.ScrollToEnd()
			})
		}
		if Choice.Message.Role == "tool" {
			app_p.QueueUpdateDraw(func() {
				fmt.Fprint(view_p, pretty.TToolResult(Choice.Message.Content))
				view_p.ScrollToEnd()
			})
		}
	}
}
