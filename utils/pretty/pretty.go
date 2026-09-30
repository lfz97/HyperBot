package pretty

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ========== 颜色定义 ==========
const (
	ColorRed     = "\033[31m"
	ColorGreen   = "\033[32m"
	ColorYellow  = "\033[33m"
	ColorBlue    = "\033[34m"
	ColorMagenta = "\033[35m"
	ColorCyan    = "\033[36m"
	ColorWhite   = "\033[37m"
	ColorGray    = "\033[90m"
	ColorReset   = "\033[0m"

	// 背景色
	ColorBgRed    = "\033[41m"
	ColorBgGreen  = "\033[42m"
	ColorBgYellow = "\033[43m"
	ColorBgBlue   = "\033[44m"

	// 样式
	Bold      = "\033[1m"
	Dim       = "\033[2m"
	Italic    = "\033[3m"
	Underline = "\033[4m"
)

// ========== TView 颜色定义 (tview 格式) ==========
const (
	TColorRed     = "red"
	TColorGreen   = "green"
	TColorYellow  = "yellow"
	TColorBlue    = "blue"
	TColorMagenta = "magenta"
	TColorCyan    = "cyan"
	TColorWhite   = "white"
	TColorGray    = "gray"
	TColorBlack   = "black"
	TColorOrange  = "orange"

	// 浅色版本
	TColorLightRed     = "lightred"
	TColorLightGreen   = "lightgreen"
	TColorLightYellow  = "lightyellow"
	TColorLightBlue    = "lightblue"
	TColorLightMagenta = "#B39DDB"
	TColorLightCyan    = "lightcyan"
	TColorLightWhite   = "lightwhite"

	//特殊版本
	TColorClaudeCodeOrange = "#f7b786" // Claude Code 橙色

	// 背景色（Span.Bg 用）
	TBgDarkGray = "#696969" // 暗灰（用户输入回显的底色）
)

// ========== TUI 界面配色（GitHub 深色模式 + 深空蓝调）==========
const (
	TuiBg          = "#000000" // 整体背景色
	TuiPanelBg     = "#151821" // 面板/侧边栏背景色
	TuiBorderColor = "#2A2F3A" // 边框颜色
	TuiInputAreaBg = "#151821" // 输入区背景色
	TuiSplitLine   = "#4A5060" // 分割线颜色
	TuiMainText    = "#C9D1D9" // 主文本颜色
	TuiSubText     = "#8B949E" // 次文本颜色
	TuiStatusHint  = "#6FC3DF" // 状态提示颜色
)

// ========== 分隔线样式 ==========
const (
	SeparatorLine  = "─"
	SeparatorThick = "═"
)

// ========== 符号定义 ==========
var (
	// 状态符号
	SymbolSuccess     = "✓"
	SymbolError       = "✗"
	SymbolWarning     = "⚠"
	SymbolInfo        = "ℹ"
	SymbolQuestion    = "❓"
	SymbolThinking    = "💭"
	SymbolRobot       = "🤖"
	SymbolUser        = "👤"
	SymbolExit        = "👋"
	SymbolWelcome     = "🌟"
	SymbolLoading     = "⟳"
	SymbolBullet      = "•"
	SymbolArrow       = "→"
	SymbolDoubleArrow = "⇒"
)

// ========== 标题与分隔线 ==========

// SectionTitle 输出 section 标题
func SectionTitle(title string) {
	fmt.Printf("%s%s%s %s %s\n", ColorCyan, Bold, strings.Repeat(SeparatorLine, 25), title, ColorReset)
}

// SectionEnd 输出 section 结束
func SectionEnd() {
	fmt.Println(ColorGray + strings.Repeat(SeparatorLine, 60) + ColorReset)
}

// Header 输出标题
func Header(text string) {
	padding := (60 - len(text)) / 2
	fmt.Printf("%s%s%s%s%s\n", ColorBlue, Bold, strings.Repeat(" ", padding), text, ColorReset)
}

// SubHeader 输出副标题
func SubHeader(text string) {
	fmt.Printf("%s%s%s\n", ColorCyan, text, ColorReset)
}

// Divider 输出分隔线
func Divider() {
	fmt.Println(ColorGray + strings.Repeat(SeparatorLine, 60) + ColorReset)
}

// DividerThick 输出粗分隔线
func DividerThick() {
	fmt.Println(ColorWhite + strings.Repeat(SeparatorThick, 60) + ColorReset)
}

// ========== 提示信息 ==========

// Welcome 输出欢迎信息
func Welcome(text string) {
	fmt.Printf("%s%s %s %s\n", ColorGreen, SymbolWelcome, text, ColorReset)
}

// Success 输出成功信息
func Success(text string) {
	fmt.Printf("%s%s %s%s\n", ColorGreen, SymbolSuccess, ColorReset, text)
}

// SuccessF 输出格式化成功信息
func SuccessF(format string, args ...interface{}) {
	fmt.Printf("%s%s %s%s\n", ColorGreen, SymbolSuccess, ColorReset, fmt.Sprintf(format, args...))
}

// Error 输出错误信息
func Error(text string) {
	fmt.Printf("%s%s %s%s\n", ColorRed, SymbolError, ColorReset, text)
}

// ErrorF 输出格式化错误信息
func ErrorF(format string, args ...interface{}) {
	fmt.Printf("%s%s %s%s\n", ColorRed, SymbolError, ColorReset, fmt.Sprintf(format, args...))
}

// Warning 输出警告信息
func Warning(text string) {
	fmt.Printf("%s%s %s%s\n", ColorYellow, SymbolWarning, ColorReset, text)
}

// WarningF 输出格式化警告信息
func WarningF(format string, args ...interface{}) {
	fmt.Printf("%s%s %s%s\n", ColorYellow, SymbolWarning, ColorReset, fmt.Sprintf(format, args...))
}

// Info 输出提示信息
func Info(text string) {
	fmt.Printf("%s%s %s%s\n", ColorBlue, SymbolInfo, ColorReset, text)
}

// InfoF 输出格式化提示信息
func InfoF(format string, args ...interface{}) {
	fmt.Printf("%s%s %s%s\n", ColorBlue, SymbolInfo, ColorReset, fmt.Sprintf(format, args...))
}

// Question 输出问题信息
func Question(text string) {
	fmt.Printf("%s%s %s%s\n", ColorCyan, SymbolQuestion, ColorReset, text)
}

// ========== 对话相关 ==========

// Greet 输出问候语（带换行）
func Greet(text string) {
	fmt.Printf("%s%s%s %s %s\n\n", ColorBlue, Bold, SymbolRobot, text, ColorReset)
}

// Thinking 输出思考中
func Thinking(text string) {
	fmt.Printf("%s%s %s%s", ColorYellow, SymbolThinking, ColorReset, text)
}

// ThinkingEnd 输出思考结束
func ThinkingEnd() {
	fmt.Printf("\n%s%s %s\n", ColorGreen, SymbolThinking, ColorReset)
}

// UserInput 输出用户输入提示（带颜色和换行）
func UserInput(text string) {
	fmt.Printf("\n%s%s%s %s%s\n", ColorGreen, Bold, SymbolUser, ColorReset, text)
}

// PromptInput 通用输入提示符（简洁版本）
func PromptInput() {
	fmt.Printf("\n%s%s%s ", ColorGreen, SymbolUser, ColorReset)
}

// ========== 工具调用 ==========

// ToolCall 输出工具调用信息
func ToolCall(name string) {
	fmt.Printf("%s%s %s调用工具: %s%s\n", ColorMagenta, SymbolArrow, ColorCyan, name, ColorReset)
}

// ToolCallArgs 输出工具参数
func ToolCallArgs(args string) {
	fmt.Printf("%s%s %s参数: %s%s\n", ColorMagenta, strings.Repeat(" ", 2), ColorGray, args, ColorReset)
}

// ========== 流式输出 ==========

// StreamStart 流式输出开始
func StreamStart() {
	fmt.Printf("%s%s %s", ColorCyan, SymbolLoading, ColorReset)
}

// StreamEnd 流式输出结束
func StreamEnd() {
	fmt.Println()
}

// ========== 内容输出（不同类型不同颜色）==========
var (
	ContentColor   = ColorWhite   // 正文内容 - 白色
	ReasoningColor = ColorYellow  // 思考推理 - 黄色
	ToolColor      = ColorMagenta // 工具调用 - 洋红色
	CodeColor      = ColorCyan    // 代码块 - 青色
	URLColor       = ColorBlue    // 链接 - 蓝色
)

// Content 输出正文内容（白色）
func Content(text string) {
	fmt.Printf("%s%s%s", ContentColor, text, ColorReset)
}

// Reasoning 输出思考推理（黄色）
func Reasoning(text string) {
	fmt.Printf("%s%s%s", ReasoningColor, text, ColorReset)
}

// ToolCallOutput 输出工具调用（洋红色）
func ToolCallOutput(name string) {
	fmt.Printf("%s%s %s[工具] %s%s\n", ColorMagenta, SymbolArrow, ColorCyan, name, ColorReset)
}

// ToolCallArgsOutput 输出工具调用参数
func ToolCallArgsOutput(args string) {
	// 截断过长的参数显示
	displayArgs := args
	if len(displayArgs) > 200 {
		displayArgs = displayArgs[:200] + "..."
	}
	fmt.Printf("%s    └── 参数: %s%s%s\n", ColorMagenta, ColorGray, displayArgs, ColorReset)
}

// ToolResult 输出工具执行结果
func ToolResult(text string) {
	// 截断过长的结果显示
	displayText := text
	if len(displayText) > 300 {
		displayText = displayText[:300] + "..."
	}
	fmt.Printf("%s    └── 结果: %s%s%s\n", ColorGreen, ColorWhite, displayText, ColorReset)
}

// MarkdownHeading 输出 Markdown 标题
func MarkdownHeading(level int, text string) {
	prefix := strings.Repeat("#", level)
	color := []string{ColorBlue, ColorGreen, ColorCyan, ColorYellow}[level-1]
	fmt.Printf("%s%s %s%s%s\n", color, prefix, ColorReset, text, ColorReset)
}

// ========== 程序流程 ==========

// Exit 退出信息
func Exit(text string) {
	fmt.Printf("%s%s %s %s\n", ColorMagenta, SymbolExit, text, ColorReset)
}

// Loading 加载信息
func Loading(text string) {
	fmt.Printf("%s%s %s%s\n", ColorCyan, SymbolLoading, ColorReset, text)
}

// LoadingF 格式化加载信息
func LoadingF(format string, args ...interface{}) {
	fmt.Printf("%s%s %s%s\n", ColorCyan, SymbolLoading, ColorReset, fmt.Sprintf(format, args...))
}

// Step 完成步骤
func Step(step int, total int, text string) {
	fmt.Printf("%s[%d/%d]%s %s%s\n", ColorCyan, step, total, ColorReset, text, ColorReset)
}

// ========== 表格与列表 ==========

// ListItem 输出列表项
func ListItem(text string) {
	fmt.Printf("%s%s%s %s\n", ColorWhite, SymbolBullet, ColorReset, text)
}

// KeyValue 输出键值对
func KeyValue(key string, value string) {
	fmt.Printf("%s%s:%s %s\n", ColorCyan, key, ColorReset, value)
}

// KeyValueF 输出格式化键值对
func KeyValueF(key string, format string, args ...interface{}) {
	value := fmt.Sprintf(format, args...)
	fmt.Printf("%s%s:%s %s\n", ColorCyan, key, ColorReset, value)
}

// ========== 进度与状态 ==========

// Progress 输出进度条
func Progress(current int, total int, barWidth int) {
	percent := float64(current) / float64(total) * 100
	filled := int(float64(barWidth) * float64(current) / float64(total))
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	fmt.Printf("\r%s[%s] %.1f%%", ColorCyan, bar, percent)
	if current == total {
		fmt.Println(ColorReset)
	}
}

// Status 输出状态
func Status(label string, status string, isOK bool) {
	if isOK {
		fmt.Printf("%s%s: %s%s %s%s\n", ColorCyan, label, ColorGreen, status, SymbolSuccess, ColorReset)
	} else {
		fmt.Printf("%s%s: %s%s %s%s\n", ColorCyan, label, ColorRed, status, SymbolError, ColorReset)
	}
}

// ========== 调试信息 ==========

// Debug 输出调试信息（仅调试模式使用）
func Debug(text string) {
	fmt.Printf("%s[DEBUG]%s %s%s\n", ColorGray, ColorReset, text, ColorReset)
}

// DebugF 输出格式化调试信息
func DebugF(format string, args ...interface{}) {
	fmt.Printf("%s[DEBUG]%s %s%s\n", ColorGray, ColorReset, fmt.Sprintf(format, args...), ColorReset)
}

// ========== 等待用户输入 ==========

// WaitForEnter 等待用户按回车
func WaitForEnter(prompt string) {
	fmt.Printf("%s%s %s", ColorYellow, SymbolArrow, ColorReset)
	fmt.Print(" ")
	fmt.Print(prompt)
	fmt.Scanln()
}

// WaitForEnterDefault 默认等待提示
func WaitForEnterDefault() {
	fmt.Printf("%s%s 按回车键继续...%s", ColorYellow, SymbolArrow, ColorReset)
	fmt.Scanln()
}

// ========== 彩色文本 ==========

// ColoredText 输出彩色文本
func ColoredText(color string, text string) {
	fmt.Printf("%s%s%s", color, text, ColorReset)
}

// BoldText 输出粗体文本
func BoldText(text string) {
	fmt.Printf("%s%s%s", Bold, text, ColorReset)
}

// ========== 格式化输出 ==========

// Println 普通换行输出
func Println(text string) {
	fmt.Println(text)
}

// Print 普通输出
func Print(text string) {
	fmt.Print(text)
}

// Printf 格式化输出
func Printf(format string, args ...interface{}) {
	fmt.Printf(format, args...)
}

// Printfln 格式化输出并换行
func Printfln(format string, args ...interface{}) {
	fmt.Printf(format, args...)
	fmt.Println()
}

// NewLine 输出空行
func NewLine() {
	fmt.Println()
}

// ========== 便捷函数 ==========

// Prompt 通用提示符
func Prompt() {
	fmt.Print(ColorCyan + SymbolArrow + ColorReset + " ")
}

// ErrorWithExit 错误并退出
func ErrorWithExit(text string) {
	Error(text)
	WaitForEnterDefault()
	os.Exit(1)
}

// ErrorFWithExit 格式化错误并退出
func ErrorFWithExit(format string, args ...interface{}) {
	ErrorF(format, args...)
	WaitForEnterDefault()
	os.Exit(1)
}

// ========== TUI 结构化片段 ==========
//
// 设计规范:
//   颜色语义: green=成功/正向  red=错误  yellow=警告/推理  cyan=用户/信息  magenta=工具
//   符号规范: 系统消息（下面「状态消息」与「生命周期」两节）**不带任何符号**，语义完全
//             由颜色承载。符号只用于对话内容的视觉标记，当前实际在用的是：
//             ▶用户输入  ●工具行  ↪工具结果  »«推理区块
//   布局规范: 状态消息前后空行分隔（写在 Text 里）
//
// 渲染契约: 颜色用字符串常量（TColorXxx / #hex），由 TUI 层映射成具体终端
// 样式——pretty 不依赖任何 TUI 框架，纯数据。Tview 标签格式的函数已随
// tview 迁移 go-tui 一并删除。

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

const thinLine = "────────────────────────────────────────"

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

// TSuccessF TUI 格式化成功信息
func TSuccessF(format string, args ...interface{}) []Span {
	return TSuccess(fmt.Sprintf(format, args...))
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

// TReasoningStart TUI 推理开始
func TReasoningStart() []Span {
	return []Span{{Text: "\n»\n", Fg: TColorYellow, Bold: true}}
}

// TReasoningEnd TUI 推理结束
func TReasoningEnd() []Span {
	return []Span{{Text: "\n«\n", Fg: TColorYellow, Bold: true}}
}

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
