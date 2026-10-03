package tui

import (
	"strings"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"HyperBot/utils/pretty"
)

// faint 次要信息的统一弱化样式（本实现唯一用到的样式）。
var faint = lipgloss.NewStyle().Faint(true).Render

// View 整屏渲染。AltScreen 全屏模式，退出时由 bubbletea 自动复原终端。
func (m *model) View() tea.View {
	v := tea.NewView(m.layout())
	v.AltScreen = true
	return v
}

// layout 屏幕自上而下：消息区(viewport) → todo 块 → 状态行 → 输入框 → 帮助行。
// 行数预算必须与 outsideLines 保持一致，否则 resize 会算错消息区高度。
func (m *model) layout() string {
	if !m.ready {
		return faint("HyperBot 启动中…")
	}
	parts := make([]string, 0, 5)
	parts = append(parts, m.vp.View())
	if m.todoText != "" {
		parts = append(parts, faint("── todo ──"), m.todoText)
	}
	parts = append(parts, statusLine(m))
	if !m.exiting { // 退出流程隐藏输入框
		parts = append(parts, m.input.View())
	}
	parts = append(parts, helpLine(m.exiting))
	return strings.Join(parts, "\n")
}

// outsideLines viewport 之外占用的行数，resize 用它反推消息区高度。
func (m *model) outsideLines() int {
	n := 2 // 状态行 + 帮助行
	if !m.exiting {
		n++ // 输入框
	}
	if m.todoText != "" {
		n++ // todo 标题行
		n += strings.Count(m.todoText, "\n") + 1
	}
	return n
}

// statusLine 状态行：临时通知 > agent 运行指示 > 留空（占位保持布局稳定）。
func statusLine(m *model) string {
	switch {
	case m.notice != "":
		return "※ " + m.notice
	case m.running:
		return faint("… agent 运行中（esc 中断）")
	default:
		return ""
	}
}

// helpLine 底部固定提示；退出流程改为"任意键退出"。
func helpLine(exiting bool) string {
	if exiting {
		return faint("按任意键退出…")
	}
	return faint("enter 发送 · esc 中断 · ctrl+c 退出")
}

// renderSegs 把消息段拍平成纯文本（最小版忽略 Span 的颜色/加粗属性）。
// 段与段之间以空行分隔，让"回显/工具块/assistant 正文"各自成块。
func renderSegs(segs []seg) string {
	var b strings.Builder
	for i, s := range segs {
		if i > 0 {
			b.WriteString("\n")
		}
		switch s.kind {
		case segText:
			b.WriteString(spansText(s.spans))
		case segMarkdown:
			b.WriteString(s.text)
		}
	}
	return b.String()
}

// spansText 把 Span 列表拼成纯文本。
func spansText(spans []pretty.Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}
