package tui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ── 配色 ─────────────────────────────────────────────────
//
// 全部样式统一用 charm 生态（lipgloss）定义。颜色语义与旧 tview 版一致：
// green=成功/正向  red=错误  yellow=警告/推理  cyan=用户/信息  橙=工具名。
// 边框色沿用 demo 的 "62"（紫），是界面唯一的"装饰色"。
var (
	cBorder      = lipgloss.Color("62")      // 输入框/帮助浮层圆角边框（demo 同款）
	cMain        = lipgloss.Color("#C9D1D9") // 主文本
	cSub         = lipgloss.Color("#8B949E") // 次文本（hint/横幅信息/todo 头尾行）
	cRed         = lipgloss.Color("1")       // 错误
	cGreen       = lipgloss.Color("2")       // 成功/退出/摘要
	cYellow      = lipgloss.Color("3")       // 警告/中断
	cCyan        = lipgloss.Color("6")       // 新对话/进行中条目
	cOrange      = lipgloss.Color("#FFA500") // 通知栏 warning
	cToolName    = lipgloss.Color("#f7b786") // 工具名（Claude Code 橙）
	cBadgeFg     = lipgloss.Color("15")      // 用户回显前景（亮白）
	cBadgeBg     = lipgloss.Color("#696969") // 用户回显底色（暗灰徽章）
	bannerSky    = lipgloss.Color("#4FC3F7") // 横幅 logo 渐变上端
	bannerStatus = lipgloss.Color("#6FC3DF") // 横幅 logo 渐变下端
)

// ── 样式渲染辅助 ─────────────────────────────────────────
//
// 文本换行规则与旧版一致：状态类消息前后各一个空行；通知（bar）类单行无换行。

// userEcho 用户输入回显：▶ 徽章（亮白粗体 + 暗灰底），前后空行。
func userEcho(text string) string {
	badge := lipgloss.NewStyle().Foreground(cBadgeFg).Background(cBadgeBg).Bold(true).
		Render("▶ " + text)
	return "\n" + badge + "\n"
}

// errText 错误消息（红）。
func errText(text string) string {
	return "\n" + lipgloss.NewStyle().Foreground(cRed).Render(text) + "\n"
}

// successText 成功消息（绿）。
func successText(text string) string {
	return "\n" + lipgloss.NewStyle().Foreground(cGreen).Render(text) + "\n"
}

// warnText 警告消息（黄）。
func warnText(text string) string {
	return "\n" + lipgloss.NewStyle().Foreground(cYellow).Render(text) + "\n"
}

// slashEcho 斜杠指令回显（亮绿）。
func slashEcho(text string) string {
	return "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(text) + "\n"
}

// summaryText 摘要提示（绿）。
func summaryText(text string) string {
	return "\n" + lipgloss.NewStyle().Foreground(cGreen).
		Render("->已生成摘要：\n"+text) + "\n"
}

// reasoningText 推理正文（黄 + 弱化）。流式尾段与定稿共用，观感一致。
func reasoningText(r string) string {
	return lipgloss.NewStyle().Foreground(cYellow).Faint(true).Render(r)
}

// reasoningBlock 思考块渲染：前后各一个空行（纯函数，流式尾段与定稿同型）。
func reasoningBlock(r string) string {
	return "\n" + reasoningText(r) + "\n"
}

// contentTag 正文前缀标记：加在渲染结果的第一个有可见内容的行上
// （正文以代码块/表格开头时首行是空的，直接前置会让 ● 独占一行）。
func contentTag(line string) string {
	return "● " + line
}

// compactLine 把多行文本压成单行：去掉 ANSI 转义与回车（命令输出常带 PTY 的
// \r\n 和程序自身的颜色码，直接进视图会串色），换行转空格并合并多余空格。
func compactLine(s string) string {
	s = ansi.Strip(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "  ", " ")
	return strings.TrimSpace(s)
}

// toolCompact 紧凑单行工具渲染：绿点 + 橙色工具名 + 灰色参数/结果概要。
// 格式: ● name  args ↪ result（result 换行缩进显示）。
// 参数压缩：去换行、合并空格、截断 80 rune；空/()/{} 不输出。
func toolCompact(name string, args []byte, result string) string {
	var compactArgs string
	if len(args) > 0 {
		s := compactLine(string(args))
		if len([]rune(s)) > 80 {
			s = string([]rune(s)[:80]) + "...)"
		}
		if s != "" && s != "()" && s != "{}" {
			compactArgs = " " + s
		}
	}

	var resultSummary string
	if result != "" {
		s := compactLine(result)
		if len([]rune(s)) > 200 {
			s = string([]rune(s)[:200]) + "...)"
		}
		if s != "" && s != "()" && s != "{}" {
			resultSummary = s
		}
	}

	var tail string
	switch {
	case compactArgs != "" && resultSummary != "":
		tail = compactArgs + " \n    ↪ " + resultSummary
	case compactArgs != "":
		tail = compactArgs
	case resultSummary != "":
		tail = " \n    ↪ " + resultSummary
	}

	dot := lipgloss.NewStyle().Foreground(cGreen).Render("●")
	nameStr := lipgloss.NewStyle().Foreground(cToolName).Render(name)
	tailStr := lipgloss.NewStyle().Foreground(cSub).Render(tail)
	return fmt.Sprintf("\n  %s %s%s", dot, nameStr, tailStr)
}

// ── bar 通知（单行、无首尾换行）──────────────────────────

// noticeNewConversation 新对话通知（青）。
func noticeNewConversation() string {
	return lipgloss.NewStyle().Foreground(cCyan).Render("new conversation started")
}

// noticeCancelled 取消通知（黄）。
func noticeCancelled() string {
	return lipgloss.NewStyle().Foreground(cYellow).Render("session cancelled")
}

// noticeSuccess 成功通知（绿）。
func noticeSuccess(text string) string {
	return lipgloss.NewStyle().Foreground(cGreen).Render(text)
}

// noticeWarning 警告通知（橙，与取消的黄色区分开）。
func noticeWarning(text string) string {
	return lipgloss.NewStyle().Foreground(cOrange).Render(text)
}

// noticeSub 兜底：次文本色。
func noticeSub(text string) string {
	return lipgloss.NewStyle().Foreground(cSub).Render(text)
}

// ── todo 清单栏 ─────────────────────────────────────────

// todoLines 把 TodoBar 的多行纯文本逐行上色：
// [TODO] 头行与 (N done) 计数行暗灰、◐ 进行中青色、☐ 待办正文色。
// ANSI 输出不需要 tview.Escape 那套转义——方括号就是字面量。
func todoLines(text string) string {
	colorOf := func(line string) color.Color {
		if strings.HasPrefix(line, "◐ ") {
			return cCyan
		} else if strings.HasPrefix(line, "☐ ") {
			return cMain
		} else { // [TODO] 头行、计数行
			return cSub
		}
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = lipgloss.NewStyle().Foreground(colorOf(line)).Render(line)
	}
	return strings.Join(lines, "\n")
}
