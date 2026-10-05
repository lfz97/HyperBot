package tui

import (
	"encoding/json"
	"testing"

	"HyperBot/utils/pretty"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestMergeTail 锁定定稿尾替换的语义（沿袭原 ReplaceTailInMsgView 的 LastIndex 算法）：
// 只替换末尾精确匹配的原文；替换失败最多退化成"没有 markdown 渲染"，绝不改错位置。
func TestMergeTail(t *testing.T) {
	// 空原文不替换：LastIndex(buf, "") 返回 len(buf)，不挡会凭空追加一份正文
	if buf, ok := mergeTail("abc", "", "X"); ok || buf != "abc" {
		t.Fatalf("空 raw 应返回原 buffer：%q %v", buf, ok)
	}

	// 末尾精确匹配 → 替换
	if buf, ok := mergeTail("hello world", "world", "WORLD"); !ok || buf != "hello WORLD" {
		t.Fatalf("尾部替换失败：%q %v", buf, ok)
	}

	// 出现在前部但不在末尾 → 放弃替换
	if buf, ok := mergeTail("world hello", "world", "WORLD"); ok || buf != "world hello" {
		t.Fatalf("非尾部匹配应放弃替换：%q %v", buf, ok)
	}

	// 短回复（如"好的。"）在前文出现过：必须用 LastIndex 命中末尾那份，
	// 而不是 Replace 把前面出现的一起改掉
	buf, ok := mergeTail("好的。\n前文\n好的。", "好的。", "渲染版")
	if !ok || buf != "好的。\n前文\n渲染版" {
		t.Fatalf("LastIndex 语义失败：%q %v", buf, ok)
	}

	// 中途有别的写入者插进来（工具块、摘要钩子等）→ 原文不在末尾，放弃替换
	if buf, ok := mergeTail("raw内容[工具块]", "raw内容", "渲染版"); ok || buf != "raw内容[工具块]" {
		t.Fatalf("被插入时应放弃替换：%q %v", buf, ok)
	}
}

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
