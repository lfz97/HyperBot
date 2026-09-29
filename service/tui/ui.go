package tui

import (
	"fmt"
	"strings"
	"time"

	"HyperBot/utils/pretty"
	gotui "github.com/grindlemire/go-tui"
)

// agentUI 是 go-tui 的根组件。
//
// 渲染模型遵循 go-tui 的反应式契约：Render() 每次调用都构建一棵全新的元素树
// （框架在 dirty 帧重渲染根组件，State/挂载组件的状态读取都发生在 Render 里）。
// 有状态 widget（TextArea、Markdown）通过 app.Mount 以稳定 key 跨帧复用，
// 框架会对缓存的实例调用 UpdateProps + Render，从而反映最新内容。
//
// 之前版本把树"保留"在 Render 外构建一次，导致 TextArea 的输出子树冻结在
// 第一帧——打字不可见、按键看起来全部失灵。这是对框架渲染契约的误用，
// 本文件整体回到库的标准用法。
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

type agentUI struct {
	t *Tui

	ta *gotui.TextArea // 输入框组件实例（mount 缓存复用，跨帧同一实例）

	// ── 以下字段仅主循环读写（渲染帧、按键/鼠标分发、watcher 回调都在主循环）──
	follow   bool           // 贴底跟随：新内容到达时自动滚到底
	scrollY  int            // 非跟随态的滚动偏移
	spinN    int            // spinner 帧计数
	msgsEl   *gotui.Element // 最近一帧的消息区元素（滚动计算的参照）
	helpOpen bool
}

func newAgentUI(t *Tui) *agentUI {
	a := &agentUI{t: t, follow: true}
	a.ta = gotui.NewTextArea(
		gotui.WithTextAreaAutoFocus(true),
		gotui.WithTextAreaMaxHeight(3),
		gotui.WithTextAreaTextStyle(mainStyle),
		gotui.WithTextAreaElementOptions(
			gotui.WithFlexGrow(1),
			gotui.WithBackground(inputBg),
		),
		gotui.WithTextAreaOnSubmit(t.submitInput),
	)
	return a
}

// ── Component 接口 ─────────────────────────────────

// Render 构建本帧的完整元素树。
func (a *agentUI) Render(app *gotui.App) *gotui.Element {
	root := gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Column),
		gotui.WithBackground(bgStyle),
	)

	// 退出态只剩消息区：剥掉输入框后没有任何 focusable，
	// "任意键退出"的 AnyKey 绑定才不会被聚焦的 textarea 抢先消费。
	if a.t.exiting.Load() {
		root.AddChild(a.buildMsgs(app))
		return root
	}

	root.AddChild(a.buildMsgs(app))
	if todo := a.buildTodo(); todo != nil {
		root.AddChild(todo)
	}
	if a.helpOpen {
		root.AddChild(a.buildHelp())
	}
	root.AddChild(a.buildNotice())
	root.AddChild(a.buildInputRow(app))
	return root
}

// Watchers 注册 spinner / 通知 TTL 的心跳。
func (a *agentUI) Watchers() []gotui.Watcher {
	return []gotui.Watcher{gotui.OnTimer(spinnerInterval, a.tick)}
}

// KeyMap 组件级按键绑定。dispatch 顺序：聚焦中 widget 的 focus-gated 绑定
// （textarea 的输入编辑）优先独占，其余事件按树序落到这里的绑定。
func (a *agentUI) KeyMap() gotui.KeyMap {
	if a.t.exiting.Load() {
		return gotui.KeyMap{
			gotui.OnStop(gotui.AnyKey, func(ke gotui.KeyEvent) { ke.App().Stop() }),
		}
	}
	return gotui.KeyMap{
		gotui.OnStop(gotui.Rune('k').Ctrl(), func(ke gotui.KeyEvent) {
			a.helpOpen = !a.helpOpen
			a.t.app.MarkDirty()
		}),
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
		// 失焦后按 Enter 重新聚焦输入框（聚焦时由 textarea 的 submit 优先处理）
		gotui.OnStop(gotui.KeyEnter, func(ke gotui.KeyEvent) {
			if !a.ta.IsFocused() {
				a.ta.Focus()
			}
		}),
		// 自愈兜底：textarea 失焦后打字直接聚焦并补上该字符，
		// 避免出现"怎么按都没反应"的假死状态。AnyRune 模式本身排除 Ctrl/Alt。
		gotui.OnStop(gotui.AnyRune, func(ke gotui.KeyEvent) {
			if !a.ta.IsFocused() {
				a.ta.Focus()
				a.ta.InsertText(string(ke.Rune))
			}
		}),
	}
}

// HandleMouse 滚轮滚动消息区。原生路径（ElementAtPoint 命中可滚元素）在
// 每帧重建的树里会被下一帧的 WithScrollOffset 覆盖，所以滚动状态由组件持有。
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
	expired := a.t.noticeMsg != "" && !time.Now().Before(a.t.noticeUntil)
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
// 滚动偏移是组件状态：Render 通过 WithScrollOffset 应用，
// 跟随模式写入大偏移值由布局钳到内容底部（等价 tview 的 trackEnd）。

func (a *agentUI) maxScrollY() int {
	if a.msgsEl == nil {
		return 0
	}
	_, maxY := a.msgsEl.MaxScroll()
	return maxY
}

func (a *agentUI) viewportH() int {
	if a.msgsEl == nil {
		return 0
	}
	_, vh := a.msgsEl.ViewportSize()
	return vh
}

// scrollBy 相对滚动 n 行（n<0 向上）。滚回底部时恢复跟随。
func (a *agentUI) scrollBy(n int) {
	if n < 0 && a.follow {
		a.follow = false
		if a.msgsEl != nil {
			_, y := a.msgsEl.ScrollOffset()
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

// ── 各区域构建 ─────────────────────────────────────

// buildMsgs 消息区：可滚动、占满剩余空间。每帧从 staging 段列表重建：
// segText 走 tagbridge 富文本渲染，segMarkdown 走内置 Markdown 组件（mount 缓存）。
func (a *agentUI) buildMsgs(app *gotui.App) *gotui.Element {
	t := a.t
	t.mu.Lock()
	segs := t.segs
	bannerSet := t.bannerSet
	banner := t.bannerLines
	t.mu.Unlock()

	offsetY := a.scrollY
	// 贴底跟随：退出消息必须可见，exiting 时强制贴底
	if a.follow || a.t.exiting.Load() {
		offsetY = bigOffset
	}
	el := gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Column),
		gotui.WithFlexGrow(1),
		gotui.WithScrollable(gotui.ScrollVertical),
		gotui.WithScrollOffset(0, offsetY),
		gotui.WithBackground(bgStyle),
	)
	if bannerSet {
		el.AddChild(buildBanner(banner))
	}
	for _, s := range segs {
		if s.kind == segText {
			el.AddChild(buildRichEl(s.text, mainStyle))
			continue
		}
		md := app.Mount(a, fmt.Sprintf("md-%d", s.id), func() gotui.Component {
			return gotui.NewMarkdown(gotui.WithMarkdownSource(s.text))
		})
		el.AddChild(md)
	}
	a.msgsEl = el
	return el
}

// buildTodo 清单栏：一行一个元素，无清单时整栏消失。
func (a *agentUI) buildTodo() *gotui.Element {
	a.t.mu.Lock()
	todo := a.t.todoText
	a.t.mu.Unlock()
	if todo == "" {
		return nil
	}
	el := gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Column),
	)
	for _, line := range strings.Split(todo, "\n") {
		el.AddChild(gotui.New(
			gotui.WithText(line),
			gotui.WithTextStyle(todoLineStyle(line)),
			gotui.WithTruncate(true),
		))
	}
	return el
}

// buildHelp 帮助面板：打开时插在清单栏和通知栏之间。
func (a *agentUI) buildHelp() *gotui.Element {
	a.t.mu.Lock()
	items := append([]helpItem(nil), a.t.helpItems...)
	a.t.mu.Unlock()

	el := gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Column),
		gotui.WithWidth(60),
		gotui.WithBorder(gotui.BorderRounded),
		gotui.WithBorderTitle(" slash commands — ctrl+k 关闭 "),
		gotui.WithPadding(1),
		gotui.WithBackground(bgStyle),
	)
	for _, it := range items {
		row := gotui.New(
			gotui.WithDisplay(gotui.DisplayFlex),
			gotui.WithDirection(gotui.Row),
		)
		row.AddChild(gotui.New(
			gotui.WithText(it.cmd),
			gotui.WithTextStyle(mainStyle),
			gotui.WithWidth(16),
			gotui.WithTruncate(true),
		))
		row.AddChild(gotui.New(
			gotui.WithText(it.desc),
			gotui.WithTextStyle(subStyle),
			gotui.WithFlexGrow(1),
			gotui.WithTruncate(true),
		))
		el.AddChild(row)
	}
	return el
}

// buildNotice 通知栏：固定 1 行、内容靠右。
// 通知是单行受信 markup：取首个 run 的样式 + 纯文本。
func (a *agentUI) buildNotice() *gotui.Element {
	t := a.t
	t.mu.Lock()
	notice := t.noticeMsg
	until := t.noticeUntil
	t.mu.Unlock()

	text := hintIdle
	if notice != "" && time.Now().Before(until) {
		text = notice
	} else if t.running.Load() {
		text = hintRunning
	}
	return gotui.New(
		gotui.WithText(plainText(text, dimStyle)),
		gotui.WithTextStyle(firstRunStyle(text, dimStyle)),
		gotui.WithTextAlign(gotui.TextAlignRight),
		gotui.WithWidthPercent(100),
		gotui.WithHeight(1),
	)
}

// buildInputRow 输入行：左侧运行指示器 + textarea。
// textarea 通过 app.Mount 以固定 key 挂载：首帧创建并缓存，
// 之后每帧由框架调用其 Render 反映最新输入内容（打字实时可见的关键）。
func (a *agentUI) buildInputRow(app *gotui.App) *gotui.Element {
	indicator := gotui.New(
		gotui.WithText(a.indicatorText()),
		gotui.WithTextStyle(a.indicatorStyle()),
		gotui.WithWidth(2),
		gotui.WithHeight(1),
		gotui.WithBackground(inputBg),
	)
	taEl := app.Mount(a, "input", func() gotui.Component { return a.ta })
	row := gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Row),
		gotui.WithAlign(gotui.AlignEnd), // 指示器贴着输入框的最后一行
	)
	row.AddChild(indicator)
	row.AddChild(taEl)
	return row
}

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

// todoLineStyle 按行首标记上色：◐ 进行中青色、☐ 待办正文色、其余暗灰。
func todoLineStyle(line string) gotui.Style {
	if strings.HasPrefix(line, "◐ ") {
		return gotui.NewStyle().Foreground(gotui.Cyan)
	}
	if strings.HasPrefix(line, "☐ ") {
		return mainStyle
	}
	return subStyle
}

// buildBanner 启动横幅：信息行列表，暗灰展示，随对话滚动。
func buildBanner(infoLines []string) *gotui.Element {
	el := gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Column),
	)
	for _, l := range infoLines {
		el.AddChild(gotui.New(
			gotui.WithText(l),
			gotui.WithTextStyle(subStyle),
			gotui.WithTruncate(true),
		))
	}
	return el
}
