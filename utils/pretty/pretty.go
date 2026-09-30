package pretty

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// pretty 是 TUI 的纯数据层：结构化文本片段（Span）+ 颜色字符串常量。
// 不做任何 stdout 直印——TUI 运行期间裸 fmt.Printf 会绕过 alt-screen 毁掉
// 画面，本包已删除全部 CLI 时代的直印 helpers。
//
// 颜色契约：颜色用字符串常量（TColorXxx / #hex），由 TUI 层
// （service/tui/style.go）映射成具体终端样式。

// ========== 颜色常量 ==========
const (
	TColorRed        = "red"
	TColorGreen      = "green"
	TColorYellow     = "yellow"
	TColorCyan       = "cyan"
	TColorWhite      = "white"
	TColorGray       = "gray"
	TColorOrange     = "orange"
	TColorLightGreen = "lightgreen"

	// 特殊色
	TColorLightMagenta     = "#B39DDB"
	TColorClaudeCodeOrange = "#f7b786" // Claude Code 橙色

	// 背景色（Span.Bg 用）
	TBgDarkGray = "#696969" // 暗灰（用户输入回显的底色）
)

// ========== TUI 界面配色（GitHub 深色模式 + 深空蓝调）==========
const (
	TuiBg          = "#000000" // 整体背景色
	TuiBorderColor = "#2A2F3A" // 边框/选中高亮色
	TuiInputAreaBg = "#151821" // 输入区背景色
	TuiMainText    = "#C9D1D9" // 主文本颜色
	TuiSubText     = "#8B949E" // 次文本颜色
)

// ========== TUI 结构化片段 ==========
//
// 设计规范:
//   颜色语义: green=成功/正向  red=错误  yellow=警告/推理  cyan=用户/信息  magenta=工具
//   符号规范: 系统消息（下面「状态消息」与「生命周期」两节）**不带任何符号**，语义完全
//             由颜色承载。符号只用于对话内容的视觉标记，当前实际在用的是：
//             ▶用户输入  ●工具行  ↪工具结果  »«推理区块
//   布局规范: 状态消息前后空行分隔（写在 Text 里）

// Span 一段同色同属性的文本。Fg/Bg 为空表示继承显示区的默认样式。
type Span struct {
	Text string
	Fg   string // 前景色：TColorXxx 常量或 #hex
	Bg   string // 背景色
	Bold bool
	Dim  bool
}

// Plain 默认样式的纯文本片段（消息区主色）。
func Plain(text string) []Span { return []Span{{Text: text}} }

// ── 状态消息 ─────────────────────────────────
//
// 本节与下面的「生命周期」节都属于"系统消息"：不带任何装饰符号，语义完全由颜色承载
// （红=错误、黄=警告/中断、绿=成功、青=信息）。新增本节的 helper 必须遵守此规则。
//
// 不在此列、符号必须保留的是对话内容的视觉标记：TUserInput 的 ▶、
// 工具区块的 ● ↪、推理区块的 » «。

// TError TUI 错误信息
func TError(text string) []Span {
	return []Span{{Text: "\n" + text + "\n", Fg: TColorRed}}
}

// TErrorF TUI 格式化错误信息
func TErrorF(format string, args ...interface{}) []Span {
	return TError(fmt.Sprintf(format, args...))
}

// TSuccess TUI 成功信息
func TSuccess(text string) []Span {
	return []Span{{Text: "\n" + text + "\n", Fg: TColorGreen}}
}

// TWarning TUI 警告信息
func TWarning(text string) []Span {
	return []Span{{Text: "\n" + text + "\n", Fg: TColorYellow}}
}

// TWarningF TUI 格式化警告信息
func TWarningF(format string, args ...interface{}) []Span {
	return TWarning(fmt.Sprintf(format, args...))
}

// ── 生命周期 ─────────────────────────────────
//
// 同属"系统消息"，规则见上面「状态消息」节的注释。
// TNewConversation / TInterrupted / TCancelled（消息区版）无调用方，已删除；
// bar 版同色同语义保留。

// TExit TUI 退出/结束信息
func TExit(text string) []Span {
	return []Span{{Text: "\n" + text + "\n", Fg: TColorGreen}}
}

// ── bar 通知（单行、无首尾换行）─────────────────────────
// 与消息区版本同色同语义，唯一差别是不带 \n（bar 是单行右对齐元素）。

// TBarNewConversation bar 版新对话提示
func TBarNewConversation() Span { return Span{Text: "new conversation started", Fg: TColorCyan} }

// TBarCancelled bar 版取消提示
func TBarCancelled() Span { return Span{Text: "session cancelled", Fg: TColorYellow} }

// TBarSuccess bar 版成功提示
func TBarSuccess(text string) Span { return Span{Text: text, Fg: TColorGreen} }

// TBarWarning bar 版警告提示。用橙色与 TBarCancelled 的黄色区分开，
// 供启动期非致命问题（如配置文件被隔离）使用。
func TBarWarning(text string) Span { return Span{Text: text, Fg: TColorOrange} }

// ── 对话内容 ─────────────────────────────────

// TUserInput TUI 用户输入回显
func TUserInput(text string) []Span {
	return []Span{{Text: "\n▶ " + text + "\n", Fg: TColorWhite, Bg: TBgDarkGray, Bold: true}}
}

// ── 推理区块 ─────────────────────────────────

// TReasoningContent 推理正文（暗黄色）
func TReasoningContent(text string) []Span {
	return []Span{{Text: text, Fg: TColorYellow, Dim: true}}
}

// ── 工具区块 ─────────────────────────────────

// compactLine 把多行文本压成单行：去掉 ANSI 转义与回车（命令输出常带 PTY 的 \r\n
// 和程序自身的颜色码，直接上屏会串色），换行转空格并合并多余空格。
// 这是数据清洗，与展示样式无关。
func compactLine(s string) string {
	s = ansi.Strip(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "  ", " ")
	return strings.TrimSpace(s)
}

// TToolCompact 紧凑单行工具渲染：绿点 + 橙色工具名 + 灰色参数/结果概要
// 格式: ● name  args ↪ result（result 换行缩进显示）
func TToolCompact(name string, args []byte, result string) []Span {
	// ── 参数压缩：去换行、合并空格、截断 80 rune；空/()/{} 不输出 ──
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

	// ── 结果：去换行、合并空格、截断 200 rune；空/()/{} 不输出（与 args 同法）──
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

	// 拼灰色尾部：有 result 才换行带 "↪ "（args 可空；仅 args 无 result 时无箭头）
	var tail string
	switch {
	case compactArgs != "" && resultSummary != "":
		tail = compactArgs + " \n    ↪ " + resultSummary
	case compactArgs != "":
		tail = compactArgs
	case resultSummary != "":
		tail = " \n    ↪ " + resultSummary
	}

	return []Span{
		{Text: "\n  "},
		{Text: "●", Fg: TColorGreen},
		{Text: " " + name, Fg: TColorClaudeCodeOrange},
		{Text: tail, Fg: TColorGray, Dim: true},
	}
}

// ── 通用 ─────────────────────────────────────

// TColoredText TUI 彩色文本
func TColoredText(color string, text string) []Span {
	return []Span{{Text: text, Fg: color}}
}
