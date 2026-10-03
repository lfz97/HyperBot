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
			b.WriteString(spansRender(s.spans))
		case segMarkdown:
			b.WriteString(s.text)
		}
	}
	return b.String()
}

// prettyColors 把 pretty 包的颜色名常量映射成 ANSI 256 色号
// （颜色契约见 utils/pretty：颜色用 TColorXxx 字符串或 #hex，TUI 层负责映射）。
// #hex 不在表里、单独透传。未知名保持终端默认色。
var prettyColors = map[string]string{
	"red": "1", "green": "2", "yellow": "3", "cyan": "6",
	"white": "7", "gray": "8", "orange": "208", "lightgreen": "10",
}

// colorOf 解析 pretty 颜色值，返回 lipgloss.Color() 可接受的颜色串；
// 非空且可识别返回 ok=true，否则保持终端默认色。
func colorOf(v string) (string, bool) {
	if v == "" {
		return "", false
	}
	if c, ok := prettyColors[v]; ok {
		return c, true
	}
	if strings.HasPrefix(v, "#") {
		return v, true
	}
	return "", false
}

// renderSpan 单个 Span → 带样式文本（lipgloss 渲染，colorprofile 按
// 终端能力自动降级：真彩/256/16 色/单色）。
func renderSpan(s pretty.Span) string {
	if s.Fg == "" && s.Bg == "" && !s.Bold && !s.Dim {
		return s.Text
	}
	st := lipgloss.NewStyle()
	if c, ok := colorOf(s.Fg); ok {
		st = st.Foreground(lipgloss.Color(c))
	}
	if c, ok := colorOf(s.Bg); ok {
		st = st.Background(lipgloss.Color(c))
	}
	if s.Bold {
		st = st.Bold(true)
	}
	if s.Dim {
		st = st.Faint(true)
	}
	return st.Render(s.Text)
}

// spansRender 把 Span 列表渲染成带样式文本。
func spansRender(spans []pretty.Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(renderSpan(s))
	}
	return b.String()
}
