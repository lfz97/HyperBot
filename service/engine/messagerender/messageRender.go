package messagerender

import (
	"strings"
	"sync"

	"HyperBot/service/engine/tools"

	"github.com/tidwall/gjson"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

type ResponseMessageStatus struct {
	startReasoning bool
	Stream         bool
	ShowReasoning  bool
}

// MessageRender 把框架的 Response 事件翻译成领域记录经 logSink 投进引擎的消息日志。
// 它不再持有任何展示端引用：markdown 渲染、配色、版式全部是消费端（TUI）的事，
// 本包只负责"发生了什么"——正文增量、定稿原文、思考内容、工具行。
// logSink 本包对引擎的全部需求：把"发生了什么"投进引擎的消息日志。
// 消费方按需声明小接口（*Engine 天然满足）——渲染包不感知引擎全貌，也不反向依赖。
type logSink interface {
	AppendNewline()
	AppendReasoning(text string)
	AppendContentDelta(text string)
	AppendContentFinal(text string)
	AppendTool(name string, in, out string)
}

type MessageRender struct {
	log    logSink
	Status *ResponseMessageStatus
	Buffer *toolMsgBuffer
}

func NewMessageRender(log logSink, ShowReasoning bool, Stream bool) *MessageRender {

	return &MessageRender{
		log: log,
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
				(*r).log.AppendNewline()
			}
			(*(*r).Status).startReasoning = true
		}
		if (*(*r).Status).ShowReasoning {
			// 思考内容
			(*r).log.AppendReasoning(Choice.Delta.ReasoningContent)
		}
	} else if (*(*r).Status).startReasoning {
		(*(*r).Status).startReasoning = false
		if (*(*r).Status).ShowReasoning {
			(*r).log.AppendNewline()
		}
	}
	if Choice.Delta.Content != "" && Choice.Delta.Role != "tool" {
		// 正文内容（工具响应片段不作为正文渲染，由下方统一处理工具信息部分处理）
		(*r).log.AppendContentDelta(Choice.Delta.Content)
	}

	// 合并响应：本 hop 的正文已完整，投一条定稿记录，消费端把刚流出去的 raw 尾部
	// 替换成 markdown 渲染版。两个 provider 实测都是每 hop 恰好一个这样的响应，
	// 且 Message.Content 与所有 Delta.Content 的拼接字节相同；替换（及失败时保 raw）
	// 的全部语义在消费端，本侧只保证定稿原文与流出的拼接一致。
	//
	// 判据只能用 IsPartial：同样在带工具调用的 hop 上，openai 给 Done=false 而 anthropic 给
	// Done=true（Done 的框架语义是"flow 是否该停"），IsFinalResponse() 则因带工具调用恒为 false。
	// Role=="tool" 也要挡：工具结果事件的 Message.Content 非空且 Role 是 tool。
	if !isPartial && strings.TrimSpace(Choice.Message.Content) != "" && Choice.Message.Role != "tool" {
		(*r).log.AppendContentFinal(Choice.Message.Content)
	}
}

func (r *MessageRender) renderNonStreamEvent(Choice model.Choice) {
	// 思考信息 - 根据配置决定是否显示
	if Choice.Message.ReasoningContent != "" && (*(*r).Status).ShowReasoning {
		(*r).log.AppendNewline()
		(*r).log.AppendReasoning(Choice.Message.ReasoningContent)
		(*r).log.AppendNewline()
	}
	// 正文内容（非流式没有先流出的 raw，消费端会直接追加渲染版）
	if strings.TrimSpace(Choice.Message.Content) != "" && Choice.Message.Role != "tool" {
		(*r).log.AppendContentFinal(Choice.Message.Content)
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
		(*r).log.AppendTool((*msg_p).FunctionName, in, out)
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
