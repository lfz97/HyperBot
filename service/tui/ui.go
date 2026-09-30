package tui

//go:generate go run github.com/grindlemire/go-tui/cmd/tui generate agentui.gsx

import (
	"strings"
	"time"
	"unicode/utf8"

	"HyperBot/utils/pretty"
	gotui "github.com/grindlemire/go-tui"
)

// agentUI 的视图在 agentui.gsx（生成 agentui_gsx.go），本文件只放交互逻辑：
// KeyMap / 鼠标 / Watchers / 滚动状态 / 指示器与通知栏的内容计算。
//
// 数据流：引擎 goroutine 只写 Tui 的 staging 字段（带锁）+ MarkDirty，
// 主循环的 Render 以 staging 为唯一事实来源重建整棵树。

// 样式统一来源于 pretty.TuiXxx 常量（Style 是值类型，链式调用返回副本）。
var (
	bgStyle   = gotui.NewStyle().Background(mustColor(pretty.TuiBg))
	inputBg   = gotui.NewStyle().Background(mustColor(pretty.TuiInputAreaBg))
	mainStyle = gotui.NewStyle().Foreground(mustColor(pretty.TuiMainText))
	subStyle  = gotui.NewStyle().Foreground(mustColor(pretty.TuiSubText))
	dimStyle  = gotui.NewStyle().Foreground(gotui.BrightBlack).Dim()
	spinStyle = gotui.NewStyle().Foreground(mustColor(pretty.TColorLightMagenta))
)

// scrollJump 滚轮/方向键滚动的行数。
const scrollJump = 3

// bigOffset 贴底跟随模式下写入 scrollY 的"足够大"的值，
// 布局时 clampScrollOffset 会把它钳到真正的底部。
const bigOffset = 1 << 28

func (a *agentUI) inputHeight() int {
	return a.input.height()
}

// inputView 返回输入框组件实例。必须以 @ 函数调用形式（@a.inputView(app)）
// 渲染：函数调用走 app.Mount，元素被打上 component 标，分发表才能按聚焦
// 状态门控输入框的 focus-gated 绑定。直接 @a.ta 只调 Render 不打标，
// 聚合其 KeyMap 到根组件会被框架判为 dispatch table error。
// 返回的是带滚动视口的包装组件（见 inputViewport）。
func (a *agentUI) inputView(app *gotui.App) gotui.Component {
	return a.input
}

// inputViewport 输入框的滚动视口（设计参照 pi agent 的 editor viewport）。
//
// 库的 TextArea 没有 scroll-to-cursor：内容超过钳制行数后光标行被直接
// 裁掉（编辑发生在不可见处）。这个包装在 ta 的元素上叠一层滚动视口：
// 高度钳到终端的 30%（≥5 行），滚动偏移按光标行跟随——光标永远可见，
// 输入区永不撑爆布局。
//
// 两个配套决定：
//   - ta 用虚拟光标（绘制 ▌ 字形）：库对滚动元素内真实终端光标的定位
//     有缺陷（captureCursor 不减滚动偏移），绘制的字形随内容滚动、天然正确；
//   - 光标行号按"光标前的 \n 数"近似（行宽不超折行宽时精确，超长折行
//     有有界误差），窗口规则三分支保持稳定不震荡。
//
// 分发表/焦点/看门狗经包装组件发现，逐项委托回 ta（mount 包装的标准做法，
// 与旧 pasteSafeInput 同骨架，已验证）。
type inputViewport struct {
	ta     *gotui.TextArea
	prevEl *gotui.Element // 上一帧渲染的元素（读 ReportCursor/ContentRect）
	offset int            // 当前滚动偏移（以布局钳制后的值为准）
	lastH  int            // 上一帧钳制后的视口高度（供模板设输入行高度）
}

const inputViewportMinRows = 5

func newInputViewport(ta *gotui.TextArea) *inputViewport {
	return &inputViewport{ta: ta}
}

// maxRows 视口最大行数：终端高的 30%，下限 5 行（pi 同款参数）。
func (w *inputViewport) maxRows(app *gotui.App) int {
	_, termH := app.Size()
	return max(inputViewportMinRows, termH*3/10)
}

func (w *inputViewport) Render(app *gotui.App) *gotui.Element {
	taEl := w.ta.Render(app) // 自然高度 = 内容行数（含幻影光标行）
	// ta 每渲染行一个子元素（含幻影光标行），据此钳视口高度
	h := min(len(taEl.Children()), w.maxRows(app))
	vp := gotui.New(
		gotui.WithDirection(gotui.Column),
		gotui.WithFlexGrow(1),
		gotui.WithScrollable(gotui.ScrollVertical),
		gotui.WithHeight(h),
		gotui.WithScrollOffset(0, w.offsetY()),
		gotui.WithBackground(inputBg),
	)
	// 内容保持自然高度：ta 的 elementOpts 自带 flexGrow(1)（原为输入行里的
	// 横向填充），在视口列里会把它纵向撑到无限滚动布局的哨兵值（100000），
	// 必须中和；宽度由视口列的 cross 轴 stretch 填满，无需自身 grow
	taEl.Apply(gotui.WithFlexGrow(0), gotui.WithFlexShrink(0))
	vp.AddChild(taEl)
	w.prevEl = vp
	w.lastH = h
	return vp
}

// height 上一帧的视口高度。滚动元素对父级的 intrinsic 测量恒报 0
// （库规则），输入行按内容自动测高会塌成 1 行、消息区吃掉全部剩余
// 空间——所以行高必须显式设，取本值（滞后一帧，收敛）。
func (w *inputViewport) height() int {
	return max(w.lastH, 1)
}

func (w *inputViewport) KeyMap() gotui.KeyMap          { return w.ta.KeyMap() }
func (w *inputViewport) IsFocused() bool               { return w.ta.IsFocused() }
func (w *inputViewport) Watchers() []gotui.Watcher     { return w.ta.Watchers() }
func (w *inputViewport) BindApp(app *gotui.App)        { w.ta.BindApp(app) }

// offsetY 计算本帧滚动偏移：让光标行保持在视口内（窗口规则）。
// 虚拟光标模式下没有 cursorSource 可读，行号用 approxCursorLine 的
// 折行前近似（行宽不超折行宽时精确；超长折行有有界误差）。
// 负值/超界由布局的 clampScrollOffset 兜底钳制。
func (w *inputViewport) offsetY() int {
	el := w.prevEl
	if el == nil {
		return 0
	}
	_, scrollY := el.ScrollOffset()
	w.offset = scrollY // 以布局钳制后的值为准
	line := approxCursorLine(w.ta)
	vh := el.ContentRect().Height
	switch {
	case line < scrollY:
		w.offset = line // 光标升到窗口上方（↑ 导航），向上滚
	case vh > 0 && line >= scrollY+vh:
		w.offset = line - vh + 1 // 光标落到窗口下方，向下滚
	}
	return w.offset
}

// approxCursorLine 光标所在行号（按 \n 计，不含折行展开的近似）。
// CursorPos 是字素索引，与 rune 序在无组合字符时一致。
func approxCursorLine(ta *gotui.TextArea) int {
	text := ta.Text()
	pos := ta.CursorPos()
	off, n := 0, 0
	for _, r := range text {
		if n >= pos {
			break
		}
		n++
		off += utf8.RuneLen(r)
	}
	return strings.Count(text[:off], "\n")
}

// ── Component 接口（视图部分见 agentui_gsx.go）──────

// Watchers 注册 spinner / 通知 TTL 的心跳。
func (a *agentUI) Watchers() []gotui.Watcher {
	return []gotui.Watcher{gotui.OnTimer(spinnerInterval, a.tick)}
}

// KeyMap 组件级按键绑定。dispatch 顺序：聚焦中 widget 的 focus-gated 绑定
// （textarea 的输入编辑，经 inputView 的 mount 打标发现）优先独占，
// 其余事件按树序落到这里的绑定。
func (a *agentUI) KeyMap() gotui.KeyMap {
	if a.t.exiting.Load() {
		return gotui.KeyMap{
			gotui.OnStop(gotui.AnyKey, func(ke gotui.KeyEvent) { ke.App().Stop() }),
		}
	}
	km := gotui.KeyMap{}
	// modal 打开期间 ctrl+k 由 modal 自己的 keyMap 处理（trapFocus 会拦截
	// 父组件绑定；且此处的 OnStop 会与 modal 的绑定冲突、炸掉分发表）
	if !a.helpOpen.Get() {
		km = append(km, gotui.OnStop(gotui.Rune('k').Ctrl(), func(ke gotui.KeyEvent) { a.toggleHelp() }))
	}
	return append(km,
		// Esc：textarea 聚焦时它的 blur 绑定先吃掉第一次（focus-gated 优先），
		// 失焦后的 Esc 到这里触发中断
		gotui.OnStop(gotui.KeyEscape, func(ke gotui.KeyEvent) { a.interrupt() }),
		gotui.OnStop(gotui.KeyCtrlC, func(ke gotui.KeyEvent) {
			t := a.t
			t.mu.Lock()
			f := t.escFn
			t.mu.Unlock()
			if f != nil {
				f()
			} else {
				ke.App().Stop()
			}
		}),
		// 滚动：聚焦时这些键被 textarea 的光标移动占用（focus-gated 优先），
		// 失焦时落到这里滚动消息区
		gotui.OnStop(gotui.KeyPageUp, func(ke gotui.KeyEvent) { a.pageScroll(-1) }),
		gotui.OnStop(gotui.KeyPageDown, func(ke gotui.KeyEvent) { a.pageScroll(1) }),
		gotui.OnStop(gotui.KeyUp, func(ke gotui.KeyEvent) { a.scrollBy(-scrollJump) }),
		gotui.OnStop(gotui.KeyDown, func(ke gotui.KeyEvent) { a.scrollBy(scrollJump) }),
		gotui.OnStop(gotui.KeyHome, func(ke gotui.KeyEvent) { a.scrollTop() }),
		gotui.OnStop(gotui.KeyEnd, func(ke gotui.KeyEvent) { a.scrollEnd() }),
		// 失焦后按 Enter 重新聚焦输入框（聚焦时由 textarea 的 submit 优先处理）。
		// 必须走 app 级焦点（FocusNext）而不是组件 Focus()：后者只置组件内部
		// flag，不进 app 焦点链，Esc 的 BlurFocused 会失效、中断链路被卡死。
		gotui.OnStop(gotui.KeyEnter, func(ke gotui.KeyEvent) {
			if !a.ta.IsFocused() {
				ke.App().FocusNext()
			}
		}),
		// 自愈兜底：textarea 失焦后打字直接聚焦并补上该字符，
		// 避免出现"怎么按都没反应"的假死状态。AnyRune 模式本身排除 Ctrl/Alt。
		gotui.OnStop(gotui.AnyRune, func(ke gotui.KeyEvent) {
			if !a.ta.IsFocused() {
				ke.App().FocusNext()
				a.ta.InsertText(string(ke.Rune))
			}
		}),
	)
}

// toggleHelp 开关帮助面板（modal 的 open state）。
func (a *agentUI) toggleHelp() {
	a.helpOpen.Set(!a.helpOpen.Get())
}

// helpModalKeyMap modal 打开期间的补充绑定：ctrl+k 关闭。必须用
// OnPreemptStop：trapFocus 的 AnyKey catch-all 也是 preempt 且先于普通
// 轮分发，非 preempt 的自定义绑定永远轮不到（Esc 关闭由 modal 内建）。
func (a *agentUI) helpModalKeyMap() gotui.KeyMap {
	return gotui.KeyMap{
		gotui.OnPreemptStop(gotui.Rune('k').Ctrl(), func(ke gotui.KeyEvent) { a.toggleHelp() }),
	}
}

// HandleMouse 滚轮滚动消息区。原生路径（ElementAtPoint 命中可滚元素）在
// 每帧重建的树里会被下一帧的 scrollOffset 覆盖，所以滚动状态由组件持有。
func (a *agentUI) HandleMouse(me gotui.MouseEvent) bool {
	switch me.Button {
	case gotui.MouseWheelUp:
		a.scrollBy(-scrollJump)
		return true
	case gotui.MouseWheelDown:
		a.scrollBy(scrollJump)
		return true
	}
	return false
}

// tick spinner 心跳 + 通知 TTL 检查（主循环执行）。
func (a *agentUI) tick() {
	if a.t.running.Load() {
		a.spinN++
		a.t.app.MarkDirty()
		return
	}
	a.t.mu.Lock()
	expired := a.t.noticeMsg.Text != "" && !time.Now().Before(a.t.noticeUntil)
	a.t.mu.Unlock()
	if expired {
		a.t.app.MarkDirty()
	}
}

// interrupt 触发引擎注册的中断回调（agent 运行期间取消当前 turn）。
func (a *agentUI) interrupt() {
	a.t.mu.Lock()
	f := a.t.escFn
	a.t.mu.Unlock()
	if f != nil {
		f()
	}
}

// ── 滚动状态 ───────────────────────────────────────
// 滚动偏移是组件状态：模板通过 scrollOffset 属性应用，
// 跟随模式写入大偏移值由布局钳到内容底部（等价 tview 的 trackEnd）。

// offsetY 本帧消息区的滚动偏移：贴底跟随（含退出态，退出消息必须可见）
// 返回大偏移值，布局时钳到底部。
func (a *agentUI) offsetY() int {
	if a.follow || a.t.exiting.Load() {
		return bigOffset
	}
	return a.scrollY
}

// scrollBy 相对滚动 n 行（n<0 向上）。滚回底部时恢复跟随。
func (a *agentUI) scrollBy(n int) {
	if n < 0 && a.follow {
		a.follow = false
		if el := a.msgsRef.El(); el != nil {
			_, y := el.ScrollOffset()
			a.scrollY = y
		}
	}
	a.scrollY = max(0, a.scrollY+n)
	if n > 0 && a.scrollY >= a.maxScrollY() {
		a.follow = true
	}
	a.t.app.MarkDirty()
}

// pageScroll 翻页（n<0 向上）。
func (a *agentUI) pageScroll(n int) {
	a.scrollBy(n * max(1, a.viewportH()))
}

func (a *agentUI) scrollTop() {
	a.follow = false
	a.scrollY = 0
	a.t.app.MarkDirty()
}

func (a *agentUI) scrollEnd() {
	a.follow = true
	a.t.app.MarkDirty()
}

func (a *agentUI) maxScrollY() int {
	if el := a.msgsRef.El(); el != nil {
		_, maxY := el.MaxScroll()
		return maxY
	}
	return 0
}

func (a *agentUI) viewportH() int {
	if el := a.msgsRef.El(); el != nil {
		_, vh := el.ViewportSize()
		return vh
	}
	return 0
}

// ── 内容计算（模板每帧调用，读 staging 走带锁方法）──

func (a *agentUI) indicatorText() string {
	if a.t.running.Load() {
		return string(spinnerFrames[a.spinN%len(spinnerFrames)]) + " "
	}
	return "> "
}

func (a *agentUI) indicatorStyle() gotui.Style {
	if a.t.running.Load() {
		return spinStyle
	}
	return subStyle
}

// noticeSpan 通知栏当前应显示的内容：临时通知 > 运行提示 > 空闲提示。
func (a *agentUI) noticeSpan() pretty.Span {
	t := a.t
	t.mu.Lock()
	notice := t.noticeMsg
	until := t.noticeUntil
	t.mu.Unlock()
	if notice.Text != "" && time.Now().Before(until) {
		return notice
	}
	if t.running.Load() {
		return pretty.Span{Text: hintRunning, Fg: pretty.TColorGray, Dim: true}
	}
	return pretty.Span{Text: hintIdle, Fg: pretty.TColorGray, Dim: true}
}

func (a *agentUI) noticeText() string {
	return a.noticeSpan().Text
}

func (a *agentUI) noticeStyle() gotui.Style {
	return spanStyle(a.noticeSpan(), dimStyle)
}

// todoLineStyle 按行首标记上色：◐ 进行中青色、☐ 待办正文色、其余暗灰。
func todoLineStyle(line string) gotui.Style {
	if len(line) >= 2 && line[:2] == "◐ " {
		return gotui.NewStyle().Foreground(gotui.Cyan)
	}
	if len(line) >= 2 && line[:2] == "☐ " {
		return mainStyle
	}
	return subStyle
}

// ── 消息区文本段组件 ───────────────────────────────

// textSegView 文本段的渲染组件。@ 表达式经 app.Mount 缓存，而 Mount 只对
// 实现 PropsUpdater 的组件每帧调 factory() 刷新 props——直接渲染裸元素
// （*tui.Element 只实现 Render）会让流式追加的文本冻结在首帧。
// 包一层、在 UpdateProps 里重建元素树即可恢复逐帧刷新。
type textSegView struct {
	spans []pretty.Span
	el    *gotui.Element
}

func newTextSegView(spans []pretty.Span) *textSegView {
	return &textSegView{spans: spans, el: richEl(spans)}
}

func (v *textSegView) Render(app *gotui.App) *gotui.Element { return v.el }

func (v *textSegView) UpdateProps(fresh gotui.Component) {
	f, ok := fresh.(*textSegView)
	if !ok {
		return
	}
	v.spans = f.spans
	v.el = richEl(v.spans)
}
