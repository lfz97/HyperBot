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

// TUI 是 bubbletea Model。事件循环（Update）只消费 frameMsg 与终端事件，
// 重活（全量重组、glamour）都在 pullCmd 的 goroutine 上。
type TUI struct {
	engine EngineView

	vp viewport.Model
	ta textarea.Model

	width  int
	height int
	// widthAtomic 给 pullCmd 读当前组件宽度（glamour 按 viewport 宽度换行，
	// cmd 在另一个 goroutine 上跑，不能直接读 model 字段）。
	widthAtomic atomic.Int32

	leftPadding  int
	rightPadding int

	helps  *helps
	bottom *bottom

	// pull 是拉取循环的私有状态（见 pullState 注释）。
	pull *pullState

	// 以下全部是事件循环的私有状态（只在 Update 里写；v2 的 Update 与
	// View 在 eventLoop 上串行执行，所以 draw() 读它们是安全的）。
	viewText     string // 最近一次上屏的视图全文
	todoText     string
	running      bool
	fatal        *wireFatal // 非 nil 后进入终态模式
	fatalHandled bool
	waitingKey   bool // 终态 waitKey=true：等任意键退出
}

// Init 组装组件并起 pull 链。pullCmd 是订阅的自续形式：cmd 在框架的
// goroutine 上轮询引擎，聚合出变化的帧才 return frameMsg 交给框架送进 Update。
func (t *TUI) Init() tea.Cmd {
	ta, tacmd := NewPrettyTextArea(0, 1, 0)
	vp, vpcmd := NewPrettyViewport(0, 1)
	t.ta = ta
	t.vp = vp
	return tea.Batch(tacmd, vpcmd, t.pullCmd())
}

// Update 事件分发。分支里只要产生了 cmd 就透传出去（多个用 tea.Batch，
// 避免赋值覆盖丢失）；cmd 产出的 Msg 会有对应的 case 接住，链条才闭环。
func (t *TUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m := msg.(type) {
	case frameMsg: // pull 链的一帧
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

// applyFrame 把 pull 链的一帧落到 model 上。
// 续链的 pullCmd 必须无条件排第一个：任何处理路径漏掉它，拉取链就断了。
//
// 帧不做去重，重复帧是常态（30ms 一帧、重组 100ms 节流）——脏活框架做：
// renderer 的 flush 有 viewEquals 整帧短路 + cell 级 diff。本函数的逐字段比较
// 才是真正的防线：view/todo/notice 未变时不触碰 viewport、不做布局重算。
func (t *TUI) applyFrame(m frameMsg) (tea.Model, tea.Cmd) {
	cmds := []tea.Cmd{t.pullCmd()}

	changed := false
	if m.running != t.running {
		t.running = m.running
		changed = true
		if m.running {
			cmds = append(cmds, t.bottom.spinner.Start())
		} else {
			t.bottom.spinner.Stop()
		}
	}

	if m.todo != t.todoText {
		t.todoText = m.todo
		changed = true
	}

	if m.notice != t.bottom.notice {
		t.bottom.SetNotice(m.notice)
		changed = true
	}

	if m.view != t.viewText {
		t.applyViewText(m.view)
		changed = true
	}

	if changed {
		t.recalcComponentSize()
	}

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
		pull:   newPullState(),
	}
}

// Run 启动 bubbletea 事件循环（阻塞到 tea.Quit）。
func (t *TUI) Run() {
	defer close(t.pull.stop) // 退出后收编 pull 链的 goroutine（它 select stop）
	if _, err := tea.NewProgram(t).Run(); err != nil {
		panic("Error running application: " + err.Error())
	}
}

// ── pull 链（订阅式 cmd）────────────────────────────────
//
// 心跳与旧 drawLoop 相同：固定帧率拉引擎五类状态（横幅一次、消息日志按版本、
// 运行态、清单、通知）+ 终态观察。与旧版的差别只在交付方式：不 program.Send，
// 而是作为 cmd 的返回值交给框架——Update 接 frameMsg、处理完返回下一个
// pullCmd 续链，纯 Elm 语义（与 spinner 的 Tick 续链同构）。

const (
	// pullInterval 拉取帧率。既是状态上屏延迟上界，也是通知 TTL 的时钟。
	pullInterval = 30 * time.Millisecond
	// composeInterval 视图重组的最小间隔：把 delta 高频到达时的无效重组合并掉。
	composeInterval = 100 * time.Millisecond
	// noticeTTL 临时通知的停留时长，到期回落兜底提示。TTL 从引擎 setAt 起算。
	noticeTTL = 4 * time.Second
)

// pullState pull 链的私有状态。只被 pullCmd 的 goroutine 读写——cmd 链是
// 串行的（同一时刻至多一个 pullCmd 在飞：Init 发一个、applyFrame 每帧续一个，
// 绝不并发两个，否则这里就是数据竞争），Update 从不触碰。
type pullState struct {
	seenVersion  uint64    // 已消费的消息日志版本
	pending      bool      // 有待重组的视图（节流窗口内推迟，不会丢）
	lastCompose  time.Time // 上次视图重组时间（节流）
	banner       string    // 启动横幅（就绪后进视图头部，只渲染一次）
	bannerDone   bool
	lastFatal    *wireFatal
	view         string // 最近一次重组的视图全文（帧恒携带，重复帧交给框架去重）
	width        int    // 本轮重组用的组件宽度（glamour 按此换行）
	glamRenderer *glamourCache
	stop         chan struct{} // Run 返回后关闭，收编 pull 链 goroutine
}

func newPullState() *pullState {
	return &pullState{glamRenderer: &glamourCache{}, stop: make(chan struct{})}
}

// pullCmd 是 pull 的 Elm 载体：cmd 在框架的 goroutine 上按 pullInterval 轮询，
// 每轮 return 一帧 frameMsg——由框架送进 Update，Update 处理完再返回下一个
// pullCmd 续链。重活（重组、glamour）都在本 goroutine 上，事件循环只收轻量帧，
// 按键永不被渲染阻塞；重复帧的渲染去重交给 renderer（viewEquals + cell diff）。
func (t *TUI) pullCmd() tea.Cmd {
	return func() tea.Msg {
		for {
			select {
			case <-t.pull.stop:
				return nil
			case <-time.After(pullInterval):
			}
			if frame, ok := t.pullOnce(); ok {
				return frameMsg(frame)
			}
		}
	}
}

// pullOnce 执行一轮拉取：按需重组视图、组装帧并返回。false 仅在
// RunStateJSON 解析失败时出现（坏帧不致盲：下一轮重拉）。
//
// 刻意不做帧级去重——重复帧（30ms 一帧、重组 100ms 节流）是常态，脏活
// 框架做：renderer 的 flush 有 viewEquals 整帧相等短路，内部是 cell 级
// 双缓冲 diff，重复帧不会产生任何终端 I/O。自己再去重只会引入状态与
// bug 面（上一版 lastSent 的先赋值后比较曾让流式输出一个字都刷不出来）。
func (t *TUI) pullOnce() (frameMsg, bool) {
	st := t.pull

	var rs wireRunState
	if err := json.Unmarshal([]byte(t.engine.RunStateJSON()), &rs); err != nil {
		return frameMsg{}, false // 坏帧不致盲：下一轮重拉
	}

	// 启动横幅：引擎就绪且宽度已知后组装一次（宽度未就绪时等下一轮，
	// 否则 0 宽度会把横幅永久降级成堆叠版）。
	if !st.bannerDone && t.widthAtomic.Load() > 0 {
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
		st.view = t.composeView(st)
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
		view:    st.view,
		todo:    todoLines(t.engine.TodoText()),
		notice:  notice,
		running: rs.Running,
		fatal:   rs.Fatal,
	}, true
}

// composeHint 常驻兜底提示。esc to interrupt 只在运行态出现——
// ESC 中断仅在 agent 运行期间有效，平时显示它是噪音。
func composeHint(running bool) string {
	if running {
		return noticeSub("esc to interrupt · ctrl+k for help")
	}
	return noticeSub("ctrl+k for help")
}
