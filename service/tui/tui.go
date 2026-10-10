package tui

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/cursor"
	sp "charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// EngineView 是 TUI 对引擎的全部依赖（消费方定义的接口）。
// 上下游零 import：跨界只有 stdlib 类型与 JSON 文本，消息类型放在 JSON 的
// type/style/kind 字段上；引擎侧的 *Engine 天然满足本接口。
// 控制权全部在 TUI：状态由 TUI 按帧拉取，输入/取消由 TUI 主动调用。
type EngineView interface {
	Version() uint64
	Records() []string // 消息日志全量（每条一行 JSON，type 字段承载消息类型）
	RunStateJSON() string
	TodoText() string
	NoticeJSON() string
	StartupInfo() ([]string, bool)
	HelpItemsJSON() string
	SubmitInput(line string) bool
	Interrupt() bool
}

// ── 跨界 JSON 形状（与引擎侧 uistate.go 的 wire schema 对齐）──────────

// wireFatal / wireRunState 运行状态快照。
type wireFatal struct {
	Text    string `json:"text"`
	Style   string `json:"style"` // plain | error | success | exit
	WaitKey bool   `json:"waitKey"`
}

type wireRunState struct {
	Running bool       `json:"running"`
	Fatal   *wireFatal `json:"fatal"`
}

// wireNotice 通知栏槽位快照。
type wireNotice struct {
	Kind  string `json:"kind"` // none | new_conversation | cancelled | success | warning
	Text  string `json:"text"`
	SetAt string `json:"setAt"` // RFC3339
}

// wireHelpItem 帮助页一行。
type wireHelpItem struct {
	Cmd  string `json:"cmd"`
	Desc string `json:"desc"`
}

// wireRecord 消息日志记录：type 承载消息类型。
//   - delta / message：msg 为框架 model.Message 原样（框架自带 json 标签，两端同型收发）
//   - user / slash / warn / error / summary：text 为语义原文
type wireRecord struct {
	Type string          `json:"type"`
	Msg  json.RawMessage `json:"msg,omitempty"`
	Text string          `json:"text,omitempty"`
}

// wireToolCall 待结果的工具调用缓冲条目（按 ToolCall.ID 索引）。
type wireToolCall struct {
	name string
	args string
}

// ── 帧消息 ───────────────────────────────────────────────
//
// pull 循环（独立 goroutine）每帧把渲染结果打包成 frameMsg 经 program.Send
// 投递进事件循环。所有 model 字段因此只在 Update 这一个 goroutine 上被写，
// View 由渲染器在帧边界读取——单写者模型，无需任何锁。

// frameMsg 一帧渲染结果（全部为已渲染的最终形态文本）。
type frameMsg struct {
	view    string // 消息区全文（含横幅/终态，glamour 已渲染、lipgloss 已上色）
	todo    string // 清单栏渲染结果（空串=无清单）
	notice  string // 底部通知渲染结果（含 TTL 兜底）
	running bool   // agent 是否在运行（spinner 开关依据）
	fatal   *wireFatal
}

// quitMsg 延迟退出的载体（终态渲染上屏后再退出）。
type quitMsg struct{}

// TUI 是 bubbletea Model。事件循环（Update）只消费 frameMsg 与终端事件，
// 重活（全量重组、glamour）都在 pull 循环的 goroutine 上。
type TUI struct {
	engine  EngineView
	program *tea.Program

	vp viewport.Model
	ta textarea.Model

	width  int
	height int
	// widthAtomic 给 pull 循环读当前组件宽度（glamour 按 viewport 宽度换行，
	// 重组在另一个 goroutine，不能直接读 model 字段）。
	widthAtomic atomic.Int32

	leftPadding  int
	rightPadding int

	helps  *helps
	bottom *bottom

	// 以下全部是事件循环的私有状态（只在 Update 里写）。
	viewText     string // 最近一次上屏的视图全文
	todoText     string
	noticeText   string
	running      bool
	fatal        *wireFatal // 非 nil 后进入终态模式
	fatalHandled bool
	waitingKey   bool // 终态 waitKey=true：等任意键退出
}

// Init 组装组件。用一条 cmd 把 pull 循环拉起来：cmd 在 program 启动后的
// goroutine 里执行，此时 program 已就绪、Send 安全（Run 里先赋值再 p.Run）。
func (t *TUI) Init() tea.Cmd {
	ta, tacmd := NewPrettyTextArea(0, 1, 0)
	vp, vpcmd := NewPrettyViewport(0, 1)
	t.ta = ta
	t.vp = vp
	return tea.Batch(
		tacmd,
		vpcmd,
		tea.Cmd(func() tea.Msg {
			t.startPullLoop()
			return nil
		}),
	)
}

// Update 事件分发。分支里只要产生了 cmd 就透传出去（多个用 tea.Batch，
// 避免赋值覆盖丢失）；cmd 产出的 Msg 会有对应的 case 接住，链条才闭环。
func (t *TUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m := msg.(type) {
	case frameMsg: // pull 循环的一帧
		return t.applyFrame(m)

	case cursor.BlinkMsg:
		t.ta, cmd = t.ta.Update(m)
		return t, cmd

	case tea.KeyPressMsg: //按键（只处理按下；kitty 协议终端还有 release 事件，裸匹配 String() 会把 enter 的 release 也当提交）
		return t.keyMsgHandler(m)

	case tea.PasteMsg: // 粘贴：v2 默认开启 bracketed paste，终端粘贴的整段文本（含换行）以 PasteMsg 到达。
		t.ta, cmd = t.ta.Update(m)
		t.recalcComponentSize()
		return t, cmd

	case tea.WindowSizeMsg: //窗口变化
		t.width = m.Width
		t.height = m.Height
		t.widthAtomic.Store(int32(t.componentWidth()))
		t.applyViewText(t.viewText) // 按新宽度整体重排
		t.recalcComponentSize()
		return t, nil

	case tea.MouseWheelMsg: //鼠标滚动
		t.vp, cmd = t.vp.Update(m)
		t.recalcComponentSize()
		return t, cmd

	case sp.TickMsg: //spinner 帧链（运行态时由 applyFrame 的 Start() 起链）
		cmd := t.bottom.spinner.Update(m)
		t.recalcComponentSize()
		return t, cmd

	case quitMsg:
		return t, tea.Quit
	}
	return t, nil
}

// applyFrame 把 pull 循环的一帧落到 model 上。
func (t *TUI) applyFrame(m frameMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	if m.running != t.running {
		t.running = m.running
		if m.running {
			cmds = append(cmds, t.bottom.spinner.Start())
		} else {
			t.bottom.spinner.Stop()
		}
	}

	if m.todo != t.todoText {
		t.todoText = m.todo
	}

	if m.notice != t.noticeText {
		t.noticeText = m.notice
		t.bottom.SetNotice(m.notice)
	}

	if m.view != t.viewText {
		t.applyViewText(m.view)
	}

	t.recalcComponentSize()

	// 终态：渲染已随 view 上屏，这里只负责收尾。
	if m.fatal != nil && !t.fatalHandled {
		t.fatalHandled = true
		t.fatal = m.fatal
		if m.fatal.WaitKey {
			t.waitingKey = true // 任意键退出（keyMsgHandler 里拦截）
		} else {
			// 不等按键：给一帧渲染时间，别让告别语一闪而过都做不到。
			cmds = append(cmds, tea.Tick(400*time.Millisecond,
				func(time.Time) tea.Msg { return quitMsg{} }))
		}
	}
	return t, tea.Batch(cmds...)
}

// applyViewText 把视图全文写入 viewport（按当前宽度软换行，视口底部跟随）。
func (t *TUI) applyViewText(text string) {
	t.viewText = text
	wasBottom := t.vp.AtBottom() // 必须在写入前获取：写入后内容变长，同一滚动位置会被误判成"不在底部"
	t.vp.SetContent(wrapViewText(text, t.componentWidth()))
	if wasBottom { //在底部就跳底刷新，实现消息持续跟随
		t.vp.GotoBottom()
	}
}

// componentWidth 消息区/输入区的可用列宽（去掉两侧 padding）。
func (t *TUI) componentWidth() int {
	return t.width - t.leftPadding - t.rightPadding
}

// wrapViewText 按宽度软换行。提交时用 lipgloss 根据窗口宽度软换行，
// 不在 viewport 渲染时软换行，提高性能（长文本只重排一次而不是每帧重算）。
func wrapViewText(text string, width int) string {
	if width <= 0 {
		return strings.TrimRight(text, "\n")
	}
	return lipgloss.NewStyle().Width(width).Render(strings.TrimRight(text, "\n"))
}

// View 组合整屏画面（viewport + todo + textarea + bottom，帮助浮层叠加其上）。
func (t *TUI) View() tea.View {
	v := tea.NewView(t.draw())
	v.MouseMode = tea.MouseModeCellMotion //启用主要鼠标事件，不含鼠标悬停
	v.AltScreen = true                    //进入备用屏幕：全屏接管，退出时自动还原
	return v
}

// NewTui 组装界面与子组件。view 是引擎侧状态源（*Engine，经 EngineView 接口注入）。
func NewTui(view EngineView) *TUI {
	return &TUI{
		leftPadding:  2,
		rightPadding: 2,
		helps:        &helps{items: [][2]string{{"ctrl+k", "显示/关闭帮助"}, {"/new", "开始新对话"}, {"/exit", "退出程序"}}},
		bottom: &bottom{
			spinner: NewSpinner([]string{
				"■■■■⬝⬝⬝⬝",
				"⬝■■■■⬝⬝⬝",
				"⬝⬝■■■■⬝⬝",
				"⬝⬝⬝■■■■⬝",
				"⬝⬝⬝⬝■■■■",
				"■⬝⬝⬝⬝■■■",
				"■■⬝⬝⬝⬝■■",
				"■■■⬝⬝⬝⬝■",
			}, "⬝⬝⬝⬝⬝⬝⬝⬝",
				60*time.Millisecond),
		},
		engine: view,
	}
}

// Run 启动 bubbletea 事件循环（阻塞到 tea.Quit）。
func (t *TUI) Run() {
	p := tea.NewProgram(t)
	t.program = p // Init 里的 cmd 会起 pull 循环，届时 Send 已可用
	if _, err := p.Run(); err != nil {
		panic("Error running application: " + err.Error())
	}
}

// ── pull 循环 ───────────────────────────────────────────
//
// 心跳与旧 drawLoop 相同：固定帧率拉引擎五类状态（横幅一次、消息日志按版本、
// 运行态、清单、通知）+ 终态观察。重活（全量重组、glamour 渲染）都在本
// goroutine 上，事件循环只收轻量 frameMsg，按键永远不被渲染阻塞。

const (
	// pullInterval 拉取帧率。既是状态上屏延迟上界，也是 spinner/TTL 的时钟。
	pullInterval = 30 * time.Millisecond
	// composeInterval 视图重组的最小间隔：把 delta 高频到达时的无效重组合并掉。
	composeInterval = 100 * time.Millisecond
	// noticeTTL 临时通知的停留时长，到期回落兜底提示。TTL 从引擎 setAt 起算。
	noticeTTL = 4 * time.Second
)

// pullState pull 循环的私有状态（单 goroutine 读写，无需同步）。
type pullState struct {
	seenVersion  uint64    // 已消费的消息日志版本
	pending      bool      // 有待重组的视图（节流窗口内推迟，不会丢）
	lastCompose  time.Time // 上次视图重组时间（节流）
	banner       string    // 启动横幅（就绪后进视图头部，只渲染一次）
	bannerDone   bool
	lastFatal    *wireFatal
	lastFrame    frameMsg // 上一帧（比对去重，空闲不 Send）
	width        int      // 本轮重组用的组件宽度（glamour 按此换行）
	glamRenderer *glamourCache
}

func (t *TUI) startPullLoop() {
	go func() {
		ticker := time.NewTicker(pullInterval)
		defer ticker.Stop()

		st := &pullState{glamRenderer: &glamourCache{}}
		for range ticker.C {
			frame := t.pullFrame(st)
			if frame != st.lastFrame {
				st.lastFrame = frame
				t.program.Send(frameMsg(frame))
			}
		}
	}()
}

// pullFrame 拉取并组装一帧。返回值与上一帧相同时上层跳过 Send（空闲零开销）。
func (t *TUI) pullFrame(st *pullState) frameMsg {
	var rs wireRunState
	if err := json.Unmarshal([]byte(t.engine.RunStateJSON()), &rs); err != nil {
		return st.lastFrame // 坏帧不致盲：下一帧重拉
	}

	// 启动横幅：引擎就绪后组装一次（引擎 init 未完成时等下一帧再拉）。
	if !st.bannerDone {
		if lines, ok := t.engine.StartupInfo(); ok {
			st.bannerDone = true
			st.banner = composeBanner(lines, int(t.widthAtomic.Load()))
			st.pending = true
		}
	}

	// 消息日志：版本有变化就标记待重组；重组按 composeInterval 节流。
	if ver := t.engine.Version(); ver != st.seenVersion {
		st.seenVersion = ver
		st.pending = true
	}
	// 终态出现也要重组一次（fatal 不 bump 版本，必须显式触发）。
	if rs.Fatal != nil && rs.Fatal != st.lastFatal {
		st.lastFatal = rs.Fatal
		st.pending = true
	}
	if st.pending && time.Since(st.lastCompose) >= composeInterval {
		st.pending = false
		st.lastCompose = time.Now()
		st.lastFrame.view = t.composeView(st)
	}

	// 通知栏槽位：TTL 内显示通知，否则回落常驻 hint（运行态多一条 esc 提示）。
	notice := composeHint(rs.Running)
	var wn wireNotice
	if err := json.Unmarshal([]byte(t.engine.NoticeJSON()), &wn); err == nil {
		if setAt, err := time.Parse(time.RFC3339, wn.SetAt); err == nil &&
			wn.Kind != "none" && time.Since(setAt) < noticeTTL {
			notice = renderNotice(wn.Kind, wn.Text)
		}
	}

	return frameMsg{
		view:    st.lastFrame.view,
		todo:    todoLines(t.engine.TodoText()),
		notice:  notice,
		running: rs.Running,
		fatal:   rs.Fatal,
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

// renderFatal 把终态消息按样式上色（引擎只存语义原文与 style 字符串）。
func renderFatal(text, style string) string {
	if style == "success" {
		return successText(text)
	} else if style == "exit" {
		return exitText(text)
	} else if style == "error" {
		return errText(text)
	} else {
		return text
	}
}
