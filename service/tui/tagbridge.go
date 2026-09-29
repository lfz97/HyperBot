package tui

import (
	"strings"

	gotui "github.com/grindlemire/go-tui"
)

// tagbridge：把 pretty 生成的 tview 颜色标签文本翻译成 go-tui 的元素树。
//
// 引擎通过 TuiService 推上来的内容分两类：
//   - 纯文本（流式 delta、todo 清单）——直接进文本元素；
//   - tview 标签 markup（pretty.TXxx 系列生成的受信内容）——本文件的解析器负责。
//
// tview 标签格式是 [fg:bg:flags]，fg/bg 支持命名色与 #hex，flags 是 b/d/i/u 等字母。
// 注意正文可能嵌入 LLM 撰写的文本（工具结果、思考内容），其中的字面 "[TODO]"
// 不是合法颜色标签，必须原样保留——所以这里只认「已知颜色名 / #hex / 重置」，
// 与 tview 的行为差异是刻意的保守策略：宁可少解析，不能吞正文。

// namedColors 把 pretty 用到的 tview 命名色映射到 go-tui 调色板。
// tcell 的 gray/lightred 等名字没有直接对应物，取最接近的 ANSI 亮色。
var namedColors = map[string]gotui.Color{
	"red":         gotui.Red,
	"green":       gotui.Green,
	"yellow":      gotui.Yellow,
	"blue":        gotui.Blue,
	"magenta":     gotui.Magenta,
	"cyan":        gotui.Cyan,
	"white":       gotui.White,
	"black":       gotui.Black,
	"gray":        gotui.BrightBlack,
	"orange":      mustColor("#FFA500"),
	"lightred":    gotui.BrightRed,
	"lightgreen":  gotui.BrightGreen,
	"lightyellow": gotui.BrightYellow,
	"lightblue":   gotui.BrightBlue,
	"lightcyan":   gotui.BrightCyan,
	"lightwhite":  gotui.BrightWhite,
	"lightgray":   gotui.BrightWhite,
}

// tagFlags 把 tview 属性字母翻译成 go-tui Style 链式调用。
// 未识别的字母静默忽略——pretty 只用 b/d，其余是防御性兼容。
func tagFlags(s *gotui.Style, flags string) {
	for _, r := range flags {
		if r == 'b' {
			*s = s.Bold()
		} else if r == 'd' {
			*s = s.Dim()
		} else if r == 'i' {
			*s = s.Italic()
		} else if r == 'u' {
			*s = s.Underline()
		} else if r == 'r' {
			*s = s.Reverse()
		}
	}
}

// parseColorPart 解析标签的 fg/bg 段：命名色 / #hex / 空 / "-"（重置）。
// 返回 (颜色, 是否为重置, 是否合法)。
func parseColorPart(part string) (gotui.Color, bool, bool) {
	if part == "" {
		return gotui.DefaultColor(), false, true
	}
	if part == "-" {
		return gotui.DefaultColor(), true, true
	}
	if c, ok := namedColors[part]; ok {
		return c, false, true
	}
	// #hex：#RRGGBB 或 #RGB，其余字符必须都是十六进制位
	if strings.HasPrefix(part, "#") && len(part) >= 4 && len(part) <= 7 {
		for _, r := range part[1:] {
			isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
			if !isHex {
				return gotui.DefaultColor(), false, false
			}
		}
		return mustColor(part), false, true
	}
	return gotui.DefaultColor(), false, false
}

// tagRun 一段样式恒定的文本。样式在标签间持续生效，直到被新标签覆盖或重置，
// 与 tview 动态颜色的语义一致。
type tagRun struct {
	style gotui.Style
	text  string
}

// parseTagged 解析 tview 标签文本为 run 序列。base 是未着色文本的兜底样式。
func parseTagged(s string, base gotui.Style) []tagRun {
	runs := make([]tagRun, 0, 4)
	cur := base
	var buf strings.Builder

	flush := func() {
		if buf.Len() > 0 {
			runs = append(runs, tagRun{style: cur, text: buf.String()})
			buf.Reset()
		}
	}

	for i := 0; i < len(s); {
		if s[i] != '[' {
			buf.WriteByte(s[i])
			i++
			continue
		}
		// 尝试解析 [fg:bg:flags]；解析失败按字面 '[' 处理
		end := strings.IndexByte(s[i:], ']')
		if end < 0 {
			buf.WriteByte(s[i])
			i++
			continue
		}
		inner := s[i+1 : i+end]
		parts := strings.Split(inner, ":")
		if len(parts) > 3 {
			buf.WriteByte(s[i])
			i++
			continue
		}
		// 逐段校验，任何一段非法都整体视为字面文本
		valid := true
		isReset := false
		var fg, bg gotui.Color
		var hasFg, hasBg, fgReset bool
		flags := ""
		for pi, p := range parts {
			if pi < 2 {
				c, reset, ok := parseColorPart(p)
				if !ok {
					valid = false
					break
				}
				if reset {
					if pi == 0 {
						fgReset = true
					}
					isReset = true
					continue
				}
				if p != "" {
					if pi == 0 {
						fg, hasFg = c, true
					} else {
						bg, hasBg = c, true
					}
				}
				continue
			}
			// flags 段：允许空、'-' 和已知属性字母
			if p != "" && p != "-" {
				for _, r := range p {
					if !strings.ContainsRune("bdilursa", r) {
						valid = false
						break
					}
				}
				if !valid {
					break
				}
				flags = p
			}
		}
		if !valid {
			buf.WriteByte(s[i])
			i++
			continue
		}

		flush()
		// 应用标签：重置回 base，再叠加新的 fg/bg/flags
		if isReset || inner == "-" {
			cur = base
		}
		if hasFg {
			cur = cur.Foreground(fg)
		}
		if hasBg {
			cur = cur.Background(bg)
		}
		if flags != "" && flags != "-" {
			tagFlags(&cur, flags)
		}
		if fgReset && !hasFg {
			cur = cur.Foreground(gotui.DefaultColor())
		}
		i += end + 1
	}
	flush()
	return runs
}

// plainText 提取标签文本的纯文本（丢弃样式），用于只需内容的场合。
func plainText(s string, base gotui.Style) string {
	runs := parseTagged(s, base)
	var b strings.Builder
	for _, r := range runs {
		b.WriteString(r.text)
	}
	return b.String()
}

// firstRunStyle 返回第一个非空 run 的样式。通知类内容是单色受信 markup，
// 整条取首个样式即可；没有任何样式时回落 base。
func firstRunStyle(s string, base gotui.Style) gotui.Style {
	runs := parseTagged(s, base)
	if len(runs) == 0 {
		return base
	}
	return runs[0].style
}

// buildRichEl 把标签文本渲染成元素块：每行一个元素（自动折行），
// 行内多段不同样式时用横向排列的子元素拼装（截断，不折行）。
// 工具块、思考内容、用户回显、摘要、退出消息都走这里。
func buildRichEl(s string, base gotui.Style) *gotui.Element {
	runs := parseTagged(s, base)

	// 按 \n 切行，样式跨行持续（tview 语义）
	type lineRuns []tagRun
	var lines []lineRuns
	cur := lineRuns{}
	for _, r := range runs {
		segs := strings.Split(r.text, "\n")
		for li, seg := range segs {
			if li > 0 {
				lines = append(lines, cur)
				cur = lineRuns{}
			}
			if seg != "" {
				cur = append(cur, tagRun{style: r.style, text: seg})
			}
		}
	}
	lines = append(lines, cur)

	container := gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Column),
	)
	for _, line := range lines {
		if len(line) == 0 {
			// 空行：保一个空文本元素占位
			container.AddChild(gotui.New(gotui.WithText("")))
			continue
		}
		if len(line) == 1 {
			container.AddChild(gotui.New(
				gotui.WithText(line[0].text),
				gotui.WithTextStyle(line[0].style),
				gotui.WithWrap(true),
			))
			continue
		}
		// 多段样式的行：横向拼装。flex 行内不折行，超宽截断——
		// 工具块内容在 pretty 层已压缩过，正常不会触到终端宽度。
		row := gotui.New(
			gotui.WithDisplay(gotui.DisplayFlex),
			gotui.WithDirection(gotui.Row),
			gotui.WithTruncate(true),
		)
		for _, r := range line {
			row.AddChild(gotui.New(gotui.WithText(r.text), gotui.WithTextStyle(r.style)))
		}
		container.AddChild(row)
	}
	return container
}

// mustColor 把 hex 字符串转成 go-tui 颜色。pretty 的调色板常量都是良构 hex，
// 解析失败在实际输入下不可能发生；兜底白色只为满足 v0.22 的 (Color, error) 签名。
func mustColor(hex string) gotui.Color {
	c, err := gotui.HexColor(hex)
	if err != nil {
		return gotui.White
	}
	return c
}
