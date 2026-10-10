package tui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"charm.land/glamour/v2"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// composeView 全量渲染：从消息日志整体重组视图文本（无缓存、纯函数式重放，
// 正确性不依赖任何记账）。每次重组都对全部记录重放状态机（流式尾段、工具缓冲）
// 并重新渲染，运行在 pull 循环的 goroutine 上。
func (t *TUI) composeView(st *pullState) string {
	// 宽度来自 pullOnce 的参数（pullCmd 续链时的快照），glamour/截断按此工作
	recs := t.fetchRecords()
	var b strings.Builder
	if st.bannerDone {
		b.WriteString(st.banner)
	}
	tailReasoning := &strings.Builder{} // 流式尾段：末尾连续 delta 累积的思考/正文
	tailContent := &strings.Builder{}
	toolBuf := map[string]*wireToolCall{} // 工具调用缓冲（随扫描重建）
	for _, line := range recs {
		var rec wireRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue // 坏行直接跳过（记录是自描述 JSON，正常流程不会出现）
		}

		var out string
		if rec.Type == "delta" {
			var m model.Message
			if rec.Msg == nil || json.Unmarshal(rec.Msg, &m) != nil {
				continue
			}
			if m.Role == "tool" {
				out = renderToolResult(&m, toolBuf)
			} else {
				tailReasoning.WriteString(m.ReasoningContent)
				tailContent.WriteString(m.Content)
				bufferToolCalls(&m, toolBuf)
			}

		} else if rec.Type == "message" {
			var m model.Message
			if rec.Msg == nil || json.Unmarshal(rec.Msg, &m) != nil {
				continue
			}
			if m.Role == "tool" {
				out = renderToolResult(&m, toolBuf)
			} else {
				bufferToolCalls(&m, toolBuf)
				// 思考块并入本条输出（定稿自带则用它，否则用流式尾累积的，两者本应同文），
				// 流式期间与定稿后的观感一致，没有切换跳变。
				var body strings.Builder
				if m.ReasoningContent != "" {
					body.WriteString(reasoningBlock(m.ReasoningContent))
				} else if tailReasoning.Len() > 0 {
					body.WriteString(reasoningBlock(tailReasoning.String()))
				}
				tailReasoning.Reset()
				if strings.TrimSpace(m.Content) != "" {
					body.WriteString(renderBody(st, m.Content))
				}
				tailContent.Reset()
				out = body.String()
			}

		} else if rec.Type == "user" {
			// 流式尾段先于边界记录上屏，并清空累积。
			out = flushTail(st, tailReasoning, tailContent) + userEcho(rec.Text)

		} else if rec.Type == "slash" {
			out = flushTail(st, tailReasoning, tailContent) + slashEcho(rec.Text)

		} else if rec.Type == "warn" {
			out = flushTail(st, tailReasoning, tailContent) + warnText(rec.Text)

		} else if rec.Type == "error" {
			out = flushTail(st, tailReasoning, tailContent) + errText(rec.Text)

		} else if rec.Type == "summary" {
			out = flushTail(st, tailReasoning, tailContent) + summaryText(rec.Text)
		}
		b.WriteString(out)
	}
	// 流式尾段 live：按累积内容整体出渲染版（流式期间看到的就是最终形态）
	if tailReasoning.Len() > 0 {
		b.WriteString(reasoningBlock(tailReasoning.String()))
	}
	if tailContent.Len() > 0 {
		b.WriteString(renderBody(st, tailContent.String()))
	}
	// 终态消息拼在最后（引擎置 fatal 后 park，不再有新记录）；
	// /exit 的终态无文案（Text 为空），不拼接。
	if st.lastFatal != nil && st.lastFatal.Text != "" {
		b.WriteString(renderFatal(st.lastFatal.Text, st.lastFatal.Style))
	}
	return b.String()
}

// renderNotice 把通知槽位翻译成带色文本（文案配色在 TUI 侧，
// 引擎只存 kind 与原文；无文本的固定文案也由本侧拼装）。
func renderNotice(kind, text string) string {
	if kind == "new_conversation" {
		return noticeNewConversation()
	} else if kind == "cancelled" {
		return noticeCancelled()
	} else if kind == "success" {
		return noticeSuccess(text)
	} else if kind == "warning" {
		return noticeWarning(text)
	} else {
		return noticeSub(text)
	}
}

// composeHint 常驻兜底提示。esc to interrupt 只在运行态出现——
// ESC 中断仅在 agent 运行期间有效，平时显示它是噪音。
func composeHint(running bool) string {
	if running {
		return noticeSub("esc to interrupt · ctrl+k for help")
	}
	return noticeSub("ctrl+k for help")
}

// renderFatal 把终态消息按样式上色（引擎只存语义原文与 style 字符串）。
// "exit"（/exit 终态）无文案、不再走这里渲染。
func renderFatal(text, style string) string {
	if style == "success" {
		return successText(text)
	} else if style == "error" {
		return errText(text)
	} else {
		return text
	}
}

// bufferToolCalls 缓冲工具调用（等结果到达后拼工具行）。
// provider 可能把一次调用拆成多个增量片段：同 ID 合并、无 ID 按 index 兜底、
// 名字非空才覆盖、参数按字节续接——完整调用与分片调用两种形态都稳。
func bufferToolCalls(m *model.Message, toolBuf map[string]*wireToolCall) {
	for i, tc := range m.ToolCalls {
		key := tc.ID
		if key == "" {
			key = fmt.Sprintf("#%d", i)
		}
		entry := toolBuf[key]
		if entry == nil {
			entry = &wireToolCall{}
			toolBuf[key] = entry
		}
		if tc.Function.Name != "" {
			entry.name = tc.Function.Name
		}
		entry.args += string(tc.Function.Arguments)
	}
}

// renderToolResult 渲染工具行：结果（Role=tool 的框架 message）到达时，按 ToolID
// 匹配此前缓冲的工具调用（toolCompact 内建参数/结果压缩与截断，原始数据直喂即可）。
// 无缓冲匹配时与旧版一致：宁可不出工具行，也不凭空渲染。
func renderToolResult(m *model.Message, toolBuf map[string]*wireToolCall) string {
	entry := toolBuf[m.ToolID]
	if entry == nil {
		return ""
	}
	delete(toolBuf, m.ToolID)
	return toolCompact(entry.name, []byte(entry.args), m.Content)
}

// flushTail 渲染流式尾段并清空累积，返回值作为边界记录（user/slash/warn/
// error/summary）输出的前缀——边界记录到达时，未定稿的流式内容先于它上屏。
func flushTail(st *pullState, tailReasoning, tailContent *strings.Builder) string {
	var b strings.Builder
	if tailReasoning.Len() > 0 {
		b.WriteString(reasoningBlock(tailReasoning.String()))
	}
	if tailContent.Len() > 0 {
		b.WriteString(renderBody(st, tailContent.String()))
	}
	tailReasoning.Reset()
	tailContent.Reset()
	return b.String()
}

// ── markdown 渲染 ─────────────────────────────────────────

// glamourTailPad 匹配行尾的"空白 + ANSI 序列"混合填充。glamour 会把标题、表格、
// 代码块的每一行都补满整行宽度，每个空格还裹一层 SGR：实测 153B 的 markdown
// 渲染后是 12.7KB，其中 92% 是这种填充。消息区每帧全量重绘、每次重组都要扫
// 一遍整个 buffer，所以必须剥掉。
var glamourTailPad = regexp.MustCompile(`(?:\x1b\[[0-9;]*m|[ \t])+$`)

// renderBody 用 glamour 渲染 markdown。ANSI 输出直接进 viewport——bubbletea
// 渲染器本身就是 ANSI 消费者，不需要任何标签转义层。
//
// 正文标记必须加在渲染结果上，不能加在 markdown 源码前面：`● ` 会让首行的
// 块级结构失效——实测代码围栏和表格会整个塌成一行、列表首项不再被识别、
// 标题降级成普通段落。
func renderBody(st *pullState, content string) string {
	out, err := st.glamRenderer.render(content, st.width)
	if err != nil {
		out = content // 渲染失败退回原文，别把整条回复吞掉
	}

	// glamour 的输出恒以一个换行开头，不剥掉的话标记会独占一行、与正文脱开
	out = strings.TrimRight(strings.TrimLeft(out, "\n\r"), "\n\r ")
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		l = glamourTailPad.ReplaceAllString(l, "")
		// 剥填充会连带剥掉行尾闭合样式的 reset，不补回则颜色泄漏到后续行
		// （实测不补会留下 2~5 个未闭合 SGR）
		if l != "" && !strings.HasSuffix(l, "\x1b[m") {
			l += "\x1b[m"
		}
		lines[i] = l
	}
	// 标记加在第一个有可见内容的行上：正文以代码块或表格开头时，
	// 剥完填充后首行是空的，直接前置会让 ● 独占一行
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			lines[i] = contentTag(l)
			break
		}
	}
	return strings.Join(lines, "\n")
}

// glamourCache 按宽度缓存的 glamour renderer（重建要解析一遍 style JSON，
// 所以必须按宽度缓存复用；pull 循环单 goroutine 使用，无需加锁）。
type glamourCache struct {
	renderer *glamour.TermRenderer
	width    int
}

func (c *glamourCache) render(in string, width int) (string, error) {
	// 宽度小于 40 时 glamour 样式会严重劣化，兜底 80
	if width < 40 {
		width = 80
	}
	r, err := c.rendererFor(width)
	if err != nil {
		return "", err
	}
	return r.Render(in)
}

// rendererFor 按宽度取缓存的 renderer，宽度变化时才重建。
func (c *glamourCache) rendererFor(w int) (*glamour.TermRenderer, error) {
	if c.renderer != nil && c.width == w {
		return c.renderer, nil
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(w),
		glamour.WithStylesFromJSONBytes([]byte(`{
			"document": {
				"margin": 0
			}
		}`)),
	)
	if err != nil {
		return nil, err
	}
	c.renderer = r
	c.width = w
	return r, nil
}
