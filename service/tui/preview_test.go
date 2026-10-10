package tui

import (
	"encoding/json"
	"os"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestRenderPreview 渲染一段完整对话并落盘，供人工目检：
// HYPERBOT_RENDER_PREVIEW=1 go test ./service/tui/ -run TestRenderPreview
func TestRenderPreview(t *testing.T) {
	if os.Getenv("HYPERBOT_RENDER_PREVIEW") == "" {
		t.Skip("set HYPERBOT_RENDER_PREVIEW=1 to dump the render preview")
	}
	mk := func(typ, text string, msg any) string {
		type rec struct {
			Type string          `json:"type"`
			Msg  json.RawMessage `json:"msg,omitempty"`
			Text string          `json:"text,omitempty"`
		}
		r := rec{Type: typ, Text: text}
		if msg != nil {
			b, _ := json.Marshal(msg)
			r.Msg = b
		}
		b, _ := json.Marshal(r)
		return string(b)
	}
	f := &fakeEngine{
		records: []string{
			mk("user", "帮我看下这段 Go 代码有没有问题：\n\n```go\nfunc main() {}\n```\n\n顺便列一下改进点。", nil),
			mk("delta", "", model.Message{Role: "assistant", ReasoningContent: "用户给了一段空的 main 函数，让我检查问题并列出改进点。\n首先，函数体是空的，编译没问题……"}),
			mk("message", "", model.Message{
				Role:             "assistant",
				Content:          "## 检查结果\n\n代码可以编译，但有几个改进点：\n\n1. `main` 函数体为空\n2. 没有返回值检查\n\n详见下表：\n\n| 项 | 状态 |\n|---|---|\n| 编译 | ✓ |\n| 规范 | ✗ |\n\n> 建议：空 main 换成 `_ = struct{}{}`。",
				ReasoningContent: "用户给了一段空的 main 函数，让我检查问题并列出改进点。\n首先，函数体是空的，编译没问题……",
				ToolCalls: []model.ToolCall{{
					ID: "t1",
					Function: model.FunctionDefinitionParam{
						Name:      "ReadFile",
						Arguments: []byte(`{"path":"main.go"}`),
					},
				}},
			}),
			mk("message", "", model.Message{Role: "tool", ToolID: "t1", Content: "func main() {\n\t// TODO: implement\n}\n\nexit code 0, 3 lines"}),
			mk("user", "谢谢，再来一段", nil),
			mk("warn", "3 秒后重试（第 1/3 次）...", nil),
			mk("error", "连续 3 次失败，已停止自动重试。请检查网络/配置后重新输入。", nil),
		},
		runState: `{"running":false,"fatal":null}`,
	}
	tui := NewTui(f)
	st := newPullState()
	st.width = 80
	out := tui.composeView(st)

	// 再拼上 todo 与通知，模拟整屏内容区
	out += "\n" + todoLines("[TODO] 3 items\n◐ 检查 main.go\n☐ 列出改进点\n(1 more · 1 done)") + "\n"
	out += "\n" + composeHint(false) + "\n"
	out += "\n" + renderNotice("success", "skills folder created") + "\n"

	if err := os.WriteFile("/tmp/opencode/render-preview.txt", []byte(out), 0644); err != nil {
		t.Fatal(err)
	}
}
