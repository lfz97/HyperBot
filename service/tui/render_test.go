package tui

import (
	"encoding/json"
	"testing"

	"HyperBot/utils/pretty"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func TestRenderFatalStyles(t *testing.T) {
	cases := []struct{ style, in, want string }{
		{"success", "已创建默认配置", pretty.TSuccess("已创建默认配置")},
		{"exit", "对话已结束", pretty.TExit("对话已结束")},
		{"error", "加载配置文件错误", pretty.TErrorF("%s", "加载配置文件错误")},
		{"plain", "原样输出", "原样输出"},
	}
	for _, c := range cases {
		if got := renderFatal(c.in, c.style); got != c.want {
			t.Fatalf("style %q: got %q want %q", c.style, got, c.want)
		}
	}
}

func TestRenderNoticeKinds(t *testing.T) {
	if got := renderNotice("new_conversation", ""); got != pretty.TBarNewConversation() {
		t.Fatalf("新对话通知不符：%q", got)
	}
	if got := renderNotice("cancelled", ""); got != pretty.TBarCancelled() {
		t.Fatalf("取消通知不符：%q", got)
	}
	if got := renderNotice("success", "done"); got != pretty.TBarSuccess("done") {
		t.Fatalf("成功通知不符：%q", got)
	}
	if got := renderNotice("warning", "warn"); got != pretty.TBarWarning("warn") {
		t.Fatalf("警告通知不符：%q", got)
	}
}

// TestParseWireMsg 锁定跨界 JSON 记录的解析：type 字段承载消息类型，
// msg 为框架 model.Message 原样（框架自带 json 标签，两端同型收发）。
func TestParseWireMsg(t *testing.T) {
	line := `{"type":"delta","msg":{"role":"assistant","content":"hi","reasoning_content":"th"}}`
	var rec wireRecord
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("记录解析失败：%v", err)
	}
	if rec.Type != "delta" {
		t.Fatalf("type 字段不符：%q", rec.Type)
	}
	var m model.Message
	if err := json.Unmarshal(rec.Msg, &m); err != nil {
		t.Fatalf("msg 解析失败：%v", err)
	}
	if m.Role != model.RoleAssistant || m.Content != "hi" || m.ReasoningContent != "th" {
		t.Fatalf("message 字段不符：%+v", m)
	}

	// 文本记录：text 字段直出
	line = `{"type":"user","text":"/exit"}`
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("文本记录解析失败：%v", err)
	}
	if rec.Type != "user" || rec.Text != "/exit" {
		t.Fatalf("文本记录不符：%+v", rec)
	}
}
