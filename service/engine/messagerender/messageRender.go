package messagerender

import (
	"HyperBot/service/engine/tools"
	"HyperBot/utils/pretty"
	"github.com/tidwall/gjson"
	"strings"
	"sync"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

type ResponseMessageStatus struct {
	startReasoning bool
	Stream         bool
	ShowReasoning  bool
}

type MessageRender struct {
	tui    tuiService
	Status *ResponseMessageStatus
	Buffer *toolMsgBuffer
}

type tuiService interface {
	PrintToMsgView(content []pretty.Span, clear bool)
	MarkdownDelta(content string)
	MarkdownDone()
}

func NewMessageRender(tui tuiService, ShowReasoning bool, Stream bool) *MessageRender {

	return &MessageRender{
		tui: tui,
		Status: &ResponseMessageStatus{
			Stream:        Stream,
			ShowReasoning: ShowReasoning,
		},
		Buffer: &toolMsgBuffer{
			mu:           sync.Mutex{},
			toolMessages: []*toolMessage{},
		},
	}
}

// startReasoning 用于标记是否正在输出思考内容。只在stream为true的情况下使用，对于思考内容的渲染方式和正文不同
func (r *MessageRender) RenderResponse(Choice model.Choice, isPartial bool) {

	if (*(*r).Status).Stream {
		r.renderStreamEvent(Choice, isPartial)

	} else {
		r.renderNonStreamEvent(Choice)
	}

	r.gatherToolMessage(Choice)
	r.renderToolMessages()
}

func (r *MessageRender) renderStreamEvent(Choice model.Choice, isPartial bool) {
	if Choice.Delta.ReasoningContent != "" {
		if !(*(*r).Status).startReasoning {
			if (*(*r).Status).ShowReasoning {
				(*r).tui.PrintToMsgView(pretty.Plain("\n"), false)
			}
			(*(*r).Status).startReasoning = true
		}
		if (*(*r).Status).ShowReasoning {
			// 思考内容
			(*r).tui.PrintToMsgView(pretty.TReasoningContent(Choice.Delta.ReasoningContent), false)
		}
	} else if (*(*r).Status).startReasoning {
		(*(*r).Status).startReasoning = false
		if (*(*r).Status).ShowReasoning {
			(*r).tui.PrintToMsgView(pretty.Plain("\n"), false)
		}
	}
	if Choice.Delta.Content != "" && Choice.Delta.Role != "tool" {
		// 正文内容（工具响应片段不作为正文渲染，由下方统一处理工具信息部分处理）：
		// 直接流进 markdown 段，组件原生渲染
		(*r).tui.MarkdownDelta(Choice.Delta.Content)
	}

	// 合并响应：本 hop 的正文已完整，把流式段定稿（去首尾空白）。
	// 两个 provider 实测 Message.Content 与所有 Delta.Content 的拼接字节相同，
	// 流式内容已在屏，收尾无需回填——旧的"流原文 + ReplaceTail 尾部替换"
	// 两段式已删除。
	//
	// 判据只能用 IsPartial：同样在带工具调用的 hop 上，openai 给 Done=false 而
	// anthropic 给 Done=true（Done 的框架语义是"flow 是否该停"），IsFinalResponse()
	// 则因带工具调用恒为 false。Role=="tool" 也要挡：工具结果事件的
	// Message.Content 非空且 Role 是 tool。
	if !isPartial && strings.TrimSpace(Choice.Message.Content) != "" && Choice.Message.Role != "tool" {
		(*r).tui.MarkdownDone()
	}
}

func (r *MessageRender) renderNonStreamEvent(Choice model.Choice) {
	// 思考信息 - 根据配置决定是否显示
	if Choice.Message.ReasoningContent != "" && (*(*r).Status).ShowReasoning {
		(*r).tui.PrintToMsgView(pretty.Plain("\n"), false)
		(*r).tui.PrintToMsgView(pretty.TReasoningContent(Choice.Message.ReasoningContent), false)
		(*r).tui.PrintToMsgView(pretty.Plain("\n"), false)
	}
	// 正文内容：整段流进 markdown 段并定稿——与流式路径共用同一条渲染管线
	if strings.TrimSpace(Choice.Message.Content) != "" && Choice.Message.Role != "tool" {
		(*r).tui.MarkdownDelta(Choice.Message.Content)
		(*r).tui.MarkdownDone()
	}
}

func (r *MessageRender) gatherToolMessage(Choice model.Choice) {

	//处理工具请求------------------------------------
	//工具请求信息不一定在delta中，也可能在message中，所以两者都要处理
	if len(Choice.Delta.ToolCalls) != 0 {
		for _, toolCall := range Choice.Delta.ToolCalls {
			r.addToolCallMsg(toolCall)
		}
	}

	if len(Choice.Message.ToolCalls) != 0 {
		for _, toolCall := range Choice.Message.ToolCalls {
			r.addToolCallMsg(toolCall)
		}
	}

	//处理工具结果------------------------------------
	//工具结果的role是tool，但信息不一定在delta中，也可能在message中，所以两者都要处理

	if Choice.Delta.Role == "tool" {

		r.addToolResultMsg(Choice.Delta.ToolID, Choice.Delta.Content)
	}
	if Choice.Message.Role == "tool" {
		r.addToolResultMsg(Choice.Message.ToolID, Choice.Message.Content)
	}
}

// 将toolcall消息放进临时buffer中
func (r *MessageRender) addToolCallMsg(toolcall model.ToolCall) {

	(*(*r).Buffer).mu.Lock()
	defer (*(*r).Buffer).mu.Unlock()

	(*(*r).Buffer).toolMessages = append((*(*r).Buffer).toolMessages, &toolMessage{
		Id:                toolcall.ID,
		FunctionName:      toolcall.Function.Name,
		FunctionArguments: toolcall.Function.Arguments,
	})
}

// 将toolresult消息，按照id一一对应放进buffer中
func (r *MessageRender) addToolResultMsg(ToolID string, content string) {
	(*(*r).Buffer).mu.Lock()
	defer (*(*r).Buffer).mu.Unlock()
	for _, msg_p := range (*(*r).Buffer).toolMessages {
		if (*msg_p).Id == ToolID {
			(*msg_p).Result = content
			(*msg_p).hasResult = true
		}
	}

}

func (r *MessageRender) renderToolMessages() {
	mappers := tools.GetParamMapper()
	for _, msg_p := range (*(*r).Buffer).toolMessages {
		if !(*msg_p).hasResult || (*msg_p).printed {
			continue
		}
		(*msg_p).printed = true
		in, out := renderToolMessageByMapper(mappers, msg_p)
		(*r).tui.PrintToMsgView(pretty.TToolCompact(
			(*msg_p).FunctionName,
			[]byte(in),
			out,
		), false)
	}

}

func renderToolMessageByMapper(mappers *tools.Mappers, msg *toolMessage) (string, string) {
	for _, mapper := range *mappers {
		if (*mapper).Name == (*msg).FunctionName {
			in := ""
			for _, in_arg := range (*mapper).In {
				in += gjson.GetBytes((*msg).FunctionArguments, in_arg).String() + "  "
			}
			out := ""
			for _, out_arg := range (*mapper).Out {
				out += gjson.GetBytes([]byte((*msg).Result), out_arg).String() + "  "
			}
			in = "(" + strings.TrimSuffix(in, "  ") + ")"
			out = "(" + strings.TrimSuffix(out, "  ") + ")"
			return in, out
		}
	}
	return string((*msg).FunctionArguments), (*msg).Result
}
