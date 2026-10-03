package tui

import (
	"strings"
	"time"

	"HyperBot/utils/pretty"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	"charm.land/bubbletea/v2"
)

const (
	// noticeTTL 状态行临时通知的停留时长，到点回落到运行指示/空闲。
	noticeTTL = 4 * time.Second
	// quitDelay NoTrigger 退出路径的缓冲：保证退出消息至少被渲染过一帧。
	quitDelay = 500 * time.Millisecond
)

// ── 引擎事件 → tea.Msg（tui.go 的契约方法投递，本文件 Update 消费）──

type (
	// spansMsg 消息区追加一组 Span；clear=true 先清空消息区。
	spansMsg struct {
		spans []pretty.Span
		clear bool
	}
	markdownDeltaMsg string   // assistant 正文流式追加
	markdownDoneMsg  struct{} // assistant 正文流式结束（纯文本渲染，无收尾动作）
	todoMsg          string   // todo 块文本；空串隐藏
	runningMsg       bool     // agent 运行态
	bannerMsg        []string // 启动横幅（物化进消息区，随对话滚动）
	helpItemsMsg struct { // slash 命令清单（最小版只存不显示）
		items []map[string]string
		reset bool
	}
	noticeMsg struct { // 状态行临时通知
		text string
		ttl  time.Duration
	}
	noticeExpireMsg struct{} // 通知到期信号
	escFnMsg        func()   // esc 回调（引擎中断入口）；nil 清除
	exitRequestMsg struct { // 退出请求
		spans   []pretty.Span
		waitKey bool // true=等任意键；false=渲染一帧后自动退
	}
	quitSoonMsg struct{} // quitDelay 到点 → 退出
)

// ── 消息段模型（沿用旧实现：连续同类写入合并进同一段，流式 delta 不膨胀段数）──

type segKind int

const (
	segText     segKind = iota // 普通 Span 文本（回显/工具块/错误/横幅等）
	segMarkdown                // assistant 正文（最小版按源码文本渲染）
)

type seg struct {
	kind      segKind
	spans     []pretty.Span // segText：片段列表
	text      string        // segMarkdown：源码
	streaming bool          // segMarkdown：流式未结束
}

// model 全部 UI 状态。只在 Update（主循环 goroutine）里改，无锁。
type model struct {
	inputChan chan string

	width, height int

	segs       []seg
	todoText   string
	notice     string
	noticeSeen time.Time // 最近一次通知的时刻，用于丢弃过期的 noticeExpireMsg
	running    bool
	helpItems  []helpItem
	escFn      func()

	exiting bool // 进入退出流程：隐藏输入框
	waitKey bool // exiting 后等任意键

	input textarea.Model
	vp    viewport.Model
	ready bool // 已收到首个 WindowSizeMsg，可正常布局
}

type helpItem struct{ cmd, desc string }

// newModel 组装初始 model。尺寸未知时先给最小占位，首个 WindowSizeMsg 立即修正。
func newModel(inputChan chan string) *model {
	m := &model{inputChan: inputChan}
	m.input = textarea.New()
	m.input.Prompt = "> "
	m.input.SetHeight(1)
	m.input.Focus()
	m.vp = viewport.New()
	m.vp.SetHeight(1)
	return m
}

// Init 无启动副作用。
func (m *model) Init() tea.Cmd { return nil }

// Update 唯一改 UI 状态的地方。绝大多数事件以 syncContent 收尾：把 segs
// 拍平进 viewport 并贴底跟随（最小版不做"向上翻页保持位置"这类细节）。
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		m.resize()

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.PasteMsg: // bracketed paste：整段插入输入框
		m.input.InsertString(msg.Content)

	case spansMsg:
		if msg.clear {
			m.segs = nil
		}
		m.appendSpans(msg.spans)

	case markdownDeltaMsg:
		m.appendMarkdown(string(msg))

	case markdownDoneMsg:
		if n := len(m.segs); n > 0 && m.segs[n-1].kind == segMarkdown {
			m.segs[n-1].text = strings.TrimSpace(m.segs[n-1].text)
			m.segs[n-1].streaming = false
		}

	case bannerMsg:
		if len(msg) > 0 {
			m.appendSpans(pretty.Plain(strings.Join(msg, "\n")))
		}

	case todoMsg:
		m.todoText = string(msg)
		m.resize() // todo 块行数变化会改变消息区可用高度

	case noticeMsg:
		m.notice = msg.text
		m.noticeSeen = time.Now()
		return m, tea.Tick(msg.ttl, func(time.Time) tea.Msg { return noticeExpireMsg{} })

	case noticeExpireMsg:
		// 只清掉已到期的那条；期间来了新通知（noticeSeen 被刷新）则保留
		if m.notice != "" && time.Since(m.noticeSeen) >= noticeTTL {
			m.notice = ""
		}

	case runningMsg:
		m.running = bool(msg)

	case helpItemsMsg:
		if msg.reset {
			m.helpItems = nil
		}
		for _, item := range msg.items {
			for cmd, desc := range item {
				m.helpItems = append(m.helpItems, helpItem{cmd: cmd, desc: desc})
			}
		}

	case escFnMsg:
		m.escFn = msg

	case exitRequestMsg:
		if len(msg.spans) > 0 {
			m.appendSpans(msg.spans)
		}
		m.exiting, m.waitKey = true, msg.waitKey
		m.resize() // 输入框隐藏，消息区高度变大
		m.syncContent()
		if msg.waitKey {
			return m, nil // 下一个按键在 handleKey 里退出
		}
		return m, tea.Tick(quitDelay, func(time.Time) tea.Msg { return quitSoonMsg{} })

	case quitSoonMsg:
		return m, tea.Quit
	}

	m.syncContent()
	return m, nil
}

// handleKey 键盘处理。优先级：退出等待态 > enter 提交 > esc 中断 >
// ctrl+c 退出 > 其余交给输入框。
func (m *model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.exiting && m.waitKey {
		return m, tea.Quit // 退出消息已在屏，任意键结束
	}
	switch msg.Keystroke() {
	case "enter":
		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			return m, nil
		}
		// 与旧实现语义一致：引擎没有读取者（自动 turn 期间）就保留文本不投递
		select {
		case m.inputChan <- text:
			m.input.Reset()
		default:
		}
		return m, nil

	case "esc":
		if m.escFn != nil {
			fn := m.escFn
			// escFn 会回调引擎、进而可能 Program.Send；v2 的 msgs 是无缓冲
			// chan，在 Update goroutine（= 接收方）里同步调用会自锁死锁。
			// 必须放进 Cmd，由 bubbletea 在独立 goroutine 执行。
			return m, func() tea.Msg {
				fn()
				return nil
			}
		}
		return m, nil

	case "ctrl+c":
		return m, tea.Quit
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// resize 尺寸或影响布局的状态（todo 文本/退出态）变化后，重算各部件尺寸。
func (m *model) resize() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	m.input.SetWidth(m.width)
	m.input.SetHeight(1)
	m.vp.SetWidth(m.width)
	m.vp.SetHeight(max(1, m.height-m.outsideLines()))
}

// syncContent 把消息段拍平进 viewport 并贴底。
func (m *model) syncContent() {
	m.vp.SetContent(renderSegs(m.segs))
	m.vp.GotoBottom()
}

// appendSpans 追加 Span 文本：连续写入合并进最近的文本段。
func (m *model) appendSpans(spans []pretty.Span) {
	if len(spans) == 0 {
		return
	}
	if n := len(m.segs); n > 0 && m.segs[n-1].kind == segText {
		m.segs[n-1].spans = append(m.segs[n-1].spans, spans...)
		return
	}
	m.segs = append(m.segs, seg{kind: segText, spans: spans})
}

// appendMarkdown 流式追加 assistant 正文：续写最近的流式 markdown 段，
// 没有则新开一段。
func (m *model) appendMarkdown(text string) {
	if n := len(m.segs); n > 0 && m.segs[n-1].kind == segMarkdown && m.segs[n-1].streaming {
		m.segs[n-1].text += text
		return
	}
	m.segs = append(m.segs, seg{kind: segMarkdown, text: text, streaming: true})
}
