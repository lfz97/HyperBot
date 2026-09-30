package tui

import (
	"HyperBot/utils/pretty"
	gotui "github.com/grindlemire/go-tui"
)

// pretty.Span（颜色字符串）→ go-tui 样式的边界映射。pretty 保持纯数据、
// 不依赖 TUI 框架；终端样式只在这一层产生。

// namedColors 把 pretty 用的命名色映射到 go-tui 调色板。
var namedColors = map[string]gotui.Color{
	"red":       gotui.Red,
	"green":     gotui.Green,
	"yellow":    gotui.Yellow,
	"cyan":      gotui.Cyan,
	"white":     gotui.White,
	"gray":      gotui.BrightBlack,
	"orange":    gotui.BrightYellow,
	"lightgreen": gotui.BrightGreen,
}

// spanColor 解析片段颜色：命名色或 #hex（#RRGGBB / #RGB）。
// 未知颜色回落主文本色，渲染降级但不中断。
func spanColor(color string) gotui.Color {
	if c, ok := namedColors[color]; ok {
		return c
	}
	if c, err := gotui.HexColor(color); err == nil {
		return c
	}
	return mustColor(pretty.TuiMainText)
}

// mustColor 把 hex 字符串转成 go-tui 颜色。pretty 的调色板常量都是良构 hex，
// 解析失败在实际输入下不可能发生；兜底白色只为满足 (Color, error) 签名。
func mustColor(hex string) gotui.Color {
	c, err := gotui.HexColor(hex)
	if err != nil {
		return gotui.White
	}
	return c
}

// spanStyle 把片段的样式覆盖项叠到 base 上（零值字段不覆盖）。
func spanStyle(s pretty.Span, base gotui.Style) gotui.Style {
	st := base
	if s.Fg != "" {
		st = st.Foreground(spanColor(s.Fg))
	}
	if s.Bg != "" {
		st = st.Background(spanColor(s.Bg))
	}
	if s.Bold {
		st = st.Bold()
	}
	if s.Dim {
		st = st.Dim()
	}
	return st
}

// richEl 把片段列表渲染成单个富文本元素：行内多样式混排、按词折行，
// 片段内的 \n 自然分行（库的 wrapSpans 原生支持，无需手工切行拼装）。
func richEl(spans []pretty.Span) *gotui.Element {
	ts := make([]gotui.TextSpan, 0, len(spans))
	for _, s := range spans {
		// 零值 Style：未设置的字段在渲染时继承元素基样式（mergeSpanStyle）
		ts = append(ts, gotui.TextSpan{Text: s.Text, Style: spanStyle(s, gotui.Style{})})
	}
	return gotui.New(
		gotui.WithRichText(ts...),
		gotui.WithTextStyle(mainStyle),
		gotui.WithWrap(true),
	)
}
