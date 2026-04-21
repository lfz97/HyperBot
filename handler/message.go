package handler

import (
	"HyperBot/tui/global_object"
	"HyperBot/utils/pretty"
	"fmt"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func printMessage(Choice model.Choice, startReasoning *bool, stream bool) {

	if stream {
		//------------------处理流式的响应---------------------------------------------------------------------------
		if Choice.Delta.ReasoningContent != "" && !(*startReasoning) {

			global_object.App_p.QueueUpdateDraw(func() {
				fmt.Fprint(global_object.AgentMessageView_p, pretty.TReasoningStart())
				global_object.AgentMessageView_p.ScrollToEnd()
			})
			*startReasoning = true

		} else if Choice.Delta.ReasoningContent != "" && (*startReasoning) {

			// 思考内容
			global_object.App_p.QueueUpdateDraw(func() {
				fmt.Fprint(global_object.AgentMessageView_p, pretty.TReasoningContent(Choice.Delta.ReasoningContent))
				global_object.AgentMessageView_p.ScrollToEnd()
			})

		} else if Choice.Delta.ReasoningContent == "" && (*startReasoning) {
			*startReasoning = false

			global_object.App_p.QueueUpdateDraw(func() {
				fmt.Fprint(global_object.AgentMessageView_p, pretty.TReasoningEnd())
				global_object.AgentMessageView_p.ScrollToEnd()
			})

		}
		if Choice.Delta.Content != "" {
			// 正文内容
			global_object.App_p.QueueUpdateDraw(func() {
				fmt.Fprint(global_object.AgentMessageView_p, Choice.Delta.Content)
				global_object.AgentMessageView_p.ScrollToEnd()
			})
		}

	} else {
		//------------------处理非流式的响应---------------------------------------------------------------------------
		//处理思考信息 - 使用黄色
		if Choice.Message.ReasoningContent != "" {

			global_object.App_p.QueueUpdateDraw(func() {
				fmt.Fprint(global_object.AgentMessageView_p, pretty.TReasoningStart())
				global_object.AgentMessageView_p.ScrollToEnd()
			})
			global_object.App_p.QueueUpdateDraw(func() {
				fmt.Fprint(global_object.AgentMessageView_p, pretty.TReasoningContent(Choice.Message.ReasoningContent))
				global_object.AgentMessageView_p.ScrollToEnd()
			})
			global_object.App_p.QueueUpdateDraw(func() {
				fmt.Fprint(global_object.AgentMessageView_p, pretty.TReasoningEnd())
				global_object.AgentMessageView_p.ScrollToEnd()
			})

		}
		// 正文内容
		if Choice.Message.Content != "" {
			global_object.App_p.QueueUpdateDraw(func() {
				fmt.Fprint(global_object.AgentMessageView_p, Choice.Message.Content)
				global_object.AgentMessageView_p.ScrollToEnd()
			})
		}
	}

	/*------------------此处统一处理工具信息---------------------------------------------------------------------------*/

	//处理工具请求------------------------------------
	//工具请求信息不一定在delta中，也可能在message中，所以两者都要处理
	if len(Choice.Delta.ToolCalls) != 0 {
		for _, toolCall := range Choice.Delta.ToolCalls {
			global_object.App_p.QueueUpdateDraw(func() {
				fmt.Fprint(global_object.AgentMessageView_p, pretty.TToolCall(toolCall.Function.Name))
				fmt.Fprint(global_object.AgentMessageView_p, pretty.TToolArgs(string(toolCall.Function.Arguments)))
				global_object.AgentMessageView_p.ScrollToEnd()
			})
		}
	}

	if len(Choice.Message.ToolCalls) != 0 {
		for _, toolCall := range Choice.Message.ToolCalls {
			global_object.App_p.QueueUpdateDraw(func() {
				fmt.Fprint(global_object.AgentMessageView_p, pretty.TToolCall(toolCall.Function.Name))
				fmt.Fprint(global_object.AgentMessageView_p, pretty.TToolArgs(string(toolCall.Function.Arguments)))
				global_object.AgentMessageView_p.ScrollToEnd()
			})
		}
	}
	//处理工具结果------------------------------------
	//工具结果的role是tool，但信息不一定在delta中，也可能在message中，所以两者都要处理
	{
		if Choice.Delta.Role == "tool" {
			global_object.App_p.QueueUpdateDraw(func() {
				fmt.Fprint(global_object.AgentMessageView_p, pretty.TToolResult(Choice.Delta.Content))
				global_object.AgentMessageView_p.ScrollToEnd()
			})
		}
		if Choice.Message.Role == "tool" {
			global_object.App_p.QueueUpdateDraw(func() {
				fmt.Fprint(global_object.AgentMessageView_p, pretty.TToolResult(Choice.Message.Content))
				global_object.AgentMessageView_p.ScrollToEnd()
			})
		}
	}
}

// 收集输出正文，如果出现错误，可以通过这段文本在下一轮对llm进行提示，帮助模型更好地理解之前发生了什么，从而调整后续输出
func gatherContentMessage(Container_p *string, Choice model.Choice, Stream bool) {
	if Stream {
		*Container_p += Choice.Delta.Content
	} else {
		*Container_p += Choice.Message.Content
	}
}
