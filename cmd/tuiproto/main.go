// tuiproto 是 bubbletea 最小化 TUI 的独立驱动。
//
// 不连真实引擎（不需要 API key / 配置文件），用一个假引擎回放引擎侧
// 对 TuiService 的全部用法：启动横幅、用户回显、流式 markdown
// （MarkdownDelta/MarkdownDone）、工具块、警告、todo、通知、running 指示、
// esc 中断——供真终端人工检查和 pty 冒烟测试使用。
//
//	跑起来：   go run ./cmd/tuiproto
//	冒烟测试： go test ./cmd/tuiproto
package main

import (
	"strings"
	"sync"
	"time"

	"HyperBot/service/tui"
	"HyperBot/utils/pretty"
)

func main() {
	svc := tui.GetTuiService()
	go fakeEngine(svc)
	svc.Run()
}

// sampleMarkdown 一段覆盖常见语法的回复（最小版 TUI 按源码文本显示）。
func sampleMarkdown() string {
	return `## 原型标题

这是流式输出的 **markdown** 正文，完成后应整体替换为渲染版。

- 列表项一
- 列表项二

` + "`" + `code_inline` + "`" + ` 与代码块：

` + "```go" + `
func main() {
    println("hello bubbletea")
}
` + "```" + `

| 列 A | 列 B |
|------|------|
| 1    | 2    |
`
}

func fakeEngine(svc *tui.Tui) {
	svc.ShowStartupBanner([]string{
		"HyperBot · bubbletea 最小原型",
		"直接输入回车发送；/long 生成 80 行长输出（测贴底跟随）；/exit 退出",
		"快捷键：enter 发送 · esc 中断 · ctrl+c 退出",
	})
	svc.ShowNotice(pretty.TBarNewConversation()) // 状态行临时通知

	for input := range svc.ListenUserInput() {
		switch strings.TrimSpace(input) {
		case "/exit":
			svc.ShowMsgAndExitNoTrigger(pretty.TExit("对话已结束，再见！"))
			return
		case "/boom":
			svc.ShowErrorInMsgViewAndExit(pretty.TErrorF("模拟致命错误"))
			return
		case "/new":
			svc.ShowNotice(pretty.TBarNewConversation())
			continue
		}

		svc.PrintToMsgView(pretty.TUserInput(input), false) // ▶ 回显
		svc.SetAgentRunning(true)

		// 模拟引擎的 esc 中断注册：每个 turn 一个取消信号
		stop := make(chan struct{})
		var once sync.Once
		svc.SetAppFuncTriggerWithEsc(func() {
			once.Do(func() { close(stop) })
			svc.ShowNotice(pretty.TBarCancelled())
		})

		// 工具块
		svc.PrintToMsgView(pretty.TToolCompact("read_file", []byte(`{"path":"main.go"}`), ""), false)

		if strings.TrimSpace(input) == "/long" {
			streamLongOutput(svc, stop)
		} else if streamMarkdown(svc, stop) {
			// 已打断：不再刷 done/todo，保持"已取消"的现场
			svc.ClearAppFuncTrigger()
			svc.SetAgentRunning(false)
			continue
		}
		svc.SetTodoText("◐ 调查 bubbletea 贴底跟随\n☐ 补 pty 冒烟测试")
		svc.ClearAppFuncTrigger()
		svc.SetAgentRunning(false)
		svc.ShowNotice(pretty.TBarSuccess("done"))
	}
}

// streamMarkdown 按固定长度的 rune 块"流式"喂给 MarkdownDelta（与真实引擎的
// verbatim delta 语义一致：所有 chunk 拼接 == 全文），结束后 MarkdownDone 收尾。
// 返回是否被 esc 打断。
func streamMarkdown(svc *tui.Tui, stop <-chan struct{}) bool {
	md := sampleMarkdown()
	runes := []rune(md)
	for i := 0; i < len(runes); i += 6 {
		end := min(i+6, len(runes))
		chunk := string(runes[i:end])
		select {
		case <-time.After(15 * time.Millisecond):
		case <-stop:
			svc.PrintToMsgView(pretty.TColoredText(pretty.TColorYellow, "（已打断）"), false)
			return true
		}
		svc.MarkdownDelta(chunk)
	}
	svc.MarkdownDone()
	svc.PrintToMsgView(pretty.Plain("\n"), false)
	svc.PrintToMsgView(pretty.TWarningF("示例警告：这是带标签的文本"), false)
	return false
}

func streamLongOutput(svc *tui.Tui, stop <-chan struct{}) {
	for i := 1; i <= 80; i++ {
		select {
		case <-stop:
			return
		default:
		}
		svc.PrintToMsgView(pretty.TColoredText(pretty.TColorGray, "line-"+pad3(i)+" —— 滚动测试行\n"), false)
		time.Sleep(5 * time.Millisecond)
	}
}

func pad3(n int) string {
	s := []byte("000")
	for i := 2; i >= 0 && n > 0; i-- {
		s[i] = byte('0' + n%10)
		n /= 10
	}
	return string(s)
}
