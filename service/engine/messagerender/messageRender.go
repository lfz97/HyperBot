package messagerender

import (
	"encoding/json"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

type ResponseMessageStatus struct {
	Stream        bool
	ShowReasoning bool
}

// logSink 本包对引擎的全部需求：把序列化好的 JSON 记录投进引擎的消息日志。
// 消费方按需声明小接口（*Engine 天然满足）——渲染包不感知引擎全貌，也不反向依赖。
type logSink interface {
	AppendRecordJSON(raw string)
}

// MessageRender 把框架 Response 事件透传序列化成 JSON 记录：
// 引擎只传原始 message（model.Message 自带 json 标签，可直接序列化），
// 增量/完整靠 type 字段区分；ShowReasoning=false 时引擎侧剥掉 reasoning_content
// （配置属于引擎，UI 无需感知）。怎么渲染（reasoning 框行、正文流式合并、
// 工具行、markdown）全部是 UI 侧的事。
type MessageRender struct {
	sink   logSink
	Status *ResponseMessageStatus
}

func NewMessageRender(sink logSink, ShowReasoning bool, Stream bool) *MessageRender {
	return &MessageRender{
		sink:   sink,
		Status: &ResponseMessageStatus{Stream: Stream, ShowReasoning: ShowReasoning},
	}
}

// wireRecord 消息记录的跨界 JSON 形状：type 承载消息类型，msg 为框架 message 原样。
type wireRecord struct {
	Type string         `json:"type"`
	Msg  *model.Message `json:"msg,omitempty"`
}

func wireMsg(typ string, m model.Message) string {
	b, err := json.Marshal(wireRecord{Type: typ, Msg: &m})
	if err != nil {
		return "" // 纯 string/json 字段的 message 不会 marshal 失败，防御性兜底
	}
	return string(b)
}

func (r *MessageRender) RenderResponse(choice model.Choice, isPartial bool) {
	st := (*r).Status
	if (*st).Stream {
		d := choice.Delta
		if !(*st).ShowReasoning {
			d.ReasoningContent = ""
		}
		// 空增量不入日志（只 bump 版本没有任何意义）
		if d.Content != "" || d.ReasoningContent != "" || len(d.ToolCalls) != 0 || d.Role == "tool" {
			(*r).sink.AppendRecordJSON(wireMsg("delta", d))
		}
	}
	if !isPartial {
		m := choice.Message
		if !(*st).ShowReasoning {
			m.ReasoningContent = ""
		}
		// 有实质内容的完整消息才入日志：正文 / 工具声明 / 工具结果
		if strings.TrimSpace(m.Content) != "" || len(m.ToolCalls) != 0 || m.Role == "tool" {
			(*r).sink.AppendRecordJSON(wireMsg("message", m))
		}
	}
}
