package tui

import (
	"fmt"
	"strings"
	"time"

	"HyperBot/utils/pretty"
	gotui "github.com/grindlemire/go-tui"
)

// agentUI 是 go-tui 的根组件，持有整棵"保留式"元素树。
//
// 架构沿用 tview 版的 drawLoop 模型：引擎 goroutine 只写 Tui 的 staging 字段
// （带锁、永不阻塞），30ms 的 OnTimer watcher 在主循环里把 staging 物化到元素上。
// 元素全部 retain + 原地改写（SetText/SetTextStyle/SetHeight），
// 框架的双缓冲 diff 渲染保证每帧只写变化的格子。
//
// 与 tview 版的对应关系：
//   drawLoop/tickIndicator/tickTodoBar/tickNoticeBar → tickAndFlush
//   GetTuiService 里的 widget 构建                    → build
//   banner.go 的手拼文本                              → buildBannerEl（flexbox 接管）

// bannerLogo 5 行 H 字标（原 banner.go 的点阵，固定 12 列宽）。
var bannerLogo = []string{
	"  ██    ██  ",
	" ██      ██ ",
	" ██████████ ",
	" ██      ██ ",
	"  ██    ██  ",
}

// bannerLogoColors 逐行渐变色（两端取 pretty 调色板常量，中间线性插值）。
var bannerLogoColors = []string{
	"#4FC3F7", // = pretty.TColorSkyBlue
	"#57C3F1",
	"#5FC3EB",
	"#67C3E5",
	"#6FC3DF", // = pretty.TuiStatusHint
}

// bannerPanelLines 右栏键位提示。go-tui 分支的换行键是 ctrl+j
// （textarea 的 submitKey 是 Enter，Ctrl+J 是内置的插入换行绑定）。
var bannerPanelLines = []string{
	"ctrl+k       slash commands",
	"esc          interrupt this run",
	"ctrl+j       insert a newline",
}

type agentUI struct {
	t *Tui

	root        *gotui.Element
	msgsEl      *gotui.Element
	todoEl      *gotui.Element
	todoLines   []*gotui.Element
	noticeEl    *gotui.Element
	indicatorEl *gotui.Element
	textarea    *gotui.TextArea
	helpModalEl *gotui.Element
	helpRows    []*gotui.Element // cmd/desc 成对存放

	helpOpen *gotui.State[bool]

	built      bool
	bannerDone bool
}

func newAgentUI(t *Tui) *agentUI {
	return &agentUI{t: t, helpOpen: gotui.NewState(false)}
}

// Render 返回保留式根树。动态内容全部由 tick 原地改写，这里不做重建——
// 与 .gsx 生成代码的静态视图模式一致（Render 恒定返回同一棵树）。
func (a *agentUI) Render(app *gotui.App) *gotui.Element {
	if !a.built {
		a.build(app)
		a.built = true
	}
	return a.root
}

// Watchers 注册 30ms 心跳：staging → 元素的物化时钟 + spinner 动画时钟。
func (a *agentUI) Watchers() []gotui.Watcher {
	return []gotui.Watcher{gotui.OnTimer(tickInterval, a.t.tick)}
}

func (a *agentUI) build(app *gotui.App) {
	bgStyle := gotui.NewStyle().Background(mustColor(pretty.TuiBg))
	inputBg := gotui.NewStyle().Background(mustColor(pretty.TuiInputAreaBg))
	mainStyle := gotui.NewStyle().Foreground(mustColor(pretty.TuiMainText))
	subStyle := gotui.NewStyle().Foreground(mustColor(pretty.TuiSubText))

	a.root = gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Column),
		gotui.WithBackground(bgStyle),
	)

	// 消息区：可滚动、占满剩余空间。底部跟随语义见 tickAndFlush 里的
	// IsAtBottom/ScrollToBottom 组合（替代 tview 的 trackEnd）。
	a.msgsEl = gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Column),
		gotui.WithFlexGrow(1),
		gotui.WithScrollable(gotui.ScrollVertical),
		gotui.WithBackground(bgStyle),
	)

	// 清单栏：高度由 tick 按行数动态调整（SetHeight(Fixed(n))），无清单时 0 行塌陷。
	// 行元素是固定池子，超出的行 SetText("") 置空（空文本元素高度为 0），
	// 容器 overflow hidden 兜底——go-tui 没有 RemoveChild，池子是标准替代手法。
	a.todoEl = gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Column),
		gotui.WithHeight(0),
		gotui.WithOverflow(gotui.OverflowHidden),
		gotui.WithBackground(bgStyle),
	)
	for i := 0; i < todoLinePool; i++ {
		el := gotui.New(gotui.WithText(""), gotui.WithTruncate(true))
		a.todoLines = append(a.todoLines, el)
		a.todoEl.AddChild(el)
	}

	// 通知栏：固定 1 行、永不塌陷，内容靠右（与 tview 版的视线落点一致）
	a.noticeEl = gotui.New(
		gotui.WithText("ctrl+k for help"),
		gotui.WithTextStyle(subStyle),
		gotui.WithTextAlign(gotui.TextAlignRight),
		gotui.WithWidthPercent(100),
		gotui.WithHeight(1),
	)

	// 输入行：左侧运行指示器 + 多行输入框。
	// 指示器独立成元素而不是 textarea 的 label：内容由 tick 单线程改写，
	// 避免 tview 版注释里说的跨 goroutine 写 label 的数据竞争。
	a.indicatorEl = gotui.New(
		gotui.WithText("> "),
		gotui.WithTextStyle(subStyle),
		gotui.WithWidth(indicatorWidth),
		gotui.WithHeight(1),
		gotui.WithBackground(inputBg),
	)
	a.textarea = gotui.NewTextArea(
		gotui.WithTextAreaAutoFocus(true),
		gotui.WithTextAreaMaxHeight(3),
		gotui.WithTextAreaTextStyle(mainStyle),
		gotui.WithTextAreaElementOptions(
			gotui.WithFlexGrow(1),
			gotui.WithBackground(inputBg),
		),
		gotui.WithTextAreaOnSubmit(a.t.submitInput),
	)
	row := gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Row),
		gotui.WithAlign(gotui.AlignEnd), // 指示器贴着输入框的最后一行
	)
	row.AddChild(a.indicatorEl)
	// textarea 是带内部状态的 widget，走 MountPersistent 挂载：
	// 首次调用创建并缓存，之后 Render 返回同一实例。
	row.AddChild(app.MountPersistent(a, "input", func() gotui.Component { return a.textarea }))

	// 帮助弹层：open 绑定 helpOpen State，Esc 关闭走 closeOnEscape 默认值，
	// trapFocus 默认开启（Tab 在弹层内循环）。
	helpContent := gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Column),
		gotui.WithWidth(60),
		gotui.WithBorder(gotui.BorderRounded),
		gotui.WithBorderTitle(" slash commands — ctrl+k/esc 关闭 "),
		gotui.WithPadding(1),
		gotui.WithBackground(bgStyle),
	)
	for i := 0; i < helpRowPool; i++ {
		cmdEl := gotui.New(gotui.WithText(""), gotui.WithTextStyle(mainStyle), gotui.WithWidth(16), gotui.WithTruncate(true))
		descEl := gotui.New(gotui.WithText(""), gotui.WithTextStyle(subStyle), gotui.WithFlexGrow(1), gotui.WithTruncate(true))
		r := gotui.New(gotui.WithDisplay(gotui.DisplayFlex), gotui.WithDirection(gotui.Row))
		r.AddChild(cmdEl)
		r.AddChild(descEl)
		helpContent.AddChild(r)
		a.helpRows = append(a.helpRows, cmdEl, descEl)
	}
	a.helpModalEl = app.MountPersistent(a, "help", func() gotui.Component {
		return gotui.NewModal(
			gotui.WithModalOpen(a.helpOpen),
			gotui.WithModalBackdrop("dim"),
			gotui.WithModalElementOptions(
				gotui.WithDisplay(gotui.DisplayFlex),
				gotui.WithJustify(gotui.JustifyCenter),
				gotui.WithAlign(gotui.AlignCenter),
			),
		)
	})
	a.helpModalEl.AddChild(helpContent)

	a.root.AddChild(a.msgsEl, a.todoEl, a.noticeEl, row, a.helpModalEl)
}

// tickAndFlush 是 30ms 心跳的全部职责：消息段物化、清单栏、通知栏、
// 启动横幅、运行指示器、帮助内容。所有元素写都在主循环，无需额外同步；
// 结束时按需 MarkDirty，空闲心跳零开销。
func (a *agentUI) tickAndFlush() {
	t := a.t
	if !a.built || t.app == nil {
		return
	}
	dirty := false

	// ── 消息段物化 ──
	t.mu.Lock()
	if t.msgVersion != t.appliedVersion {
		// 贴底判断要在改内容之前做：改完再查 IsAtBottom 永远是真
		wasBottom := a.msgsEl.IsAtBottom()
		for _, s := range t.segs {
			if s.kind == segRaw {
				if s.el == nil {
					s.el = gotui.New(
						gotui.WithText(s.text),
						gotui.WithTextStyle(gotui.NewStyle().Foreground(mustColor(pretty.TuiMainText))),
						gotui.WithWrap(true),
					)
					a.msgsEl.AddChild(s.el)
				} else if s.el.Text() != s.text {
					// 流式增量的原地更新：只改当前段，历史段零成本
					s.el.SetText(s.text)
				}
			} else if s.kind == segRich {
				if s.el == nil {
					s.el = buildRichEl(s.text, gotui.NewStyle().Foreground(mustColor(pretty.TuiMainText)))
					a.msgsEl.AddChild(s.el)
				}
			} else if s.kind == segMarkdown && s.md == nil {
				s.md = gotui.NewMarkdown(gotui.WithMarkdownSource(s.text))
				el := t.app.MountPersistent(a, fmt.Sprintf("md-%d", s.id), func() gotui.Component { return s.md })
				a.msgsEl.AddChild(el)
			}
		}
		t.appliedVersion = t.msgVersion
		if wasBottom {
			// 延迟贴底：ScrollToBottom 在下一次布局算出新内容高度后落到真正的底部，
			// 用户上翻过（wasBottom=false）则完全不打扰——trackEnd 语义的等价物
			a.msgsEl.ScrollToBottom()
		}
		dirty = true
	}
	t.mu.Unlock()

	// ── 清单栏 ──
	t.mu.Lock()
	todo := t.todoText
	t.mu.Unlock()
	if todo != t.lastTodo {
		t.lastTodo = todo
		var lines []string
		if todo != "" {
			lines = strings.Split(todo, "\n")
		}
		_, termH := t.app.Size()
		n := len(lines)
		// 通知栏与输入行至少各占 1 行，消息区至少保留 minMessageRows 行
		if max := termH - 2 - minMessageRows; n > max {
			n = max
		}
		if n < 0 {
			n = 0
		}
		if n > len(a.todoLines) {
			n = len(a.todoLines)
		}
		// 纯文本直写：go-tui 没有颜色标签语法，LLM 文本里的 "[TODO]" 天然安全，
		// tview 版的 Escape + 标签包裹管线不需要了
		for i, el := range a.todoLines {
			if i < n {
				el.SetText(lines[i])
				el.SetTextStyle(todoLineStyle(lines[i]))
			} else {
				el.SetText("")
			}
		}
		a.todoEl.SetHeight(gotui.Fixed(n))
		dirty = true
	}

	// ── 通知栏（TTL 到期/新通知/running 翻转共用一条路径）──
	t.mu.Lock()
	notice := t.noticeMsg
	until := t.noticeUntil
	t.mu.Unlock()
	running := t.running.Load()
	noticeText := hintIdle
	if notice != "" && time.Now().Before(until) {
		noticeText = notice
	} else if running {
		noticeText = hintRunning
	}
	if noticeText != t.lastNotice {
		t.lastNotice = noticeText
		// 通知是单行受信 markup：取首个 run 的样式 + 纯文本
		base := gotui.NewStyle().Foreground(gotui.BrightBlack).Dim()
		a.noticeEl.SetText(plainText(noticeText, base))
		a.noticeEl.SetTextStyle(firstRunStyle(noticeText, base))
		dirty = true
	}

	// ── 启动横幅（一次性物化，随对话滚动）──
	t.mu.Lock()
	set := t.bannerSet
	info := t.bannerLines
	t.mu.Unlock()
	if set && !a.bannerDone {
		a.bannerDone = true
		a.msgsEl.AddChild(buildBannerEl(info))
		a.msgsEl.ScrollToBottom()
		dirty = true
	}

	// ── 运行指示器 ──
	if running != t.lastRun {
		t.lastRun = running
		t.spinTickN = 0
		a.setIndicator(running, 0)
		dirty = true
	} else if running {
		t.spinTickN++
		if t.spinTickN%spinnerTicks == 0 {
			a.setIndicator(running, t.spinTickN/spinnerTicks)
			dirty = true
		}
	}

	// ── 帮助内容（弹层打开时才刷新，items 版本号变化才重建）──
	t.mu.Lock()
	items := t.helpItems
	hv := t.helpVersion
	t.mu.Unlock()
	if a.helpOpen.Get() && hv != t.appliedHelpVersion {
		t.appliedHelpVersion = hv
		a.refreshHelp(items)
		dirty = true
	}

	if dirty {
		t.app.MarkDirty()
	}
}

func (a *agentUI) setIndicator(running bool, frame int) {
	if running {
		a.indicatorEl.SetText(string(spinnerFrames[frame%len(spinnerFrames)]) + " ")
		a.indicatorEl.SetTextStyle(gotui.NewStyle().Foreground(mustColor(pretty.TColorLightMagenta)))
		return
	}
	a.indicatorEl.SetText("> ")
	a.indicatorEl.SetTextStyle(gotui.NewStyle().Foreground(mustColor(pretty.TuiSubText)))
}

// todoLineStyle 按行首标记上色：◐ 进行中青色、☐ 待办正文色、头行与计数行暗灰。
func todoLineStyle(line string) gotui.Style {
	if strings.HasPrefix(line, "◐ ") {
		return gotui.NewStyle().Foreground(gotui.Cyan)
	}
	if strings.HasPrefix(line, "☐ ") {
		return gotui.NewStyle().Foreground(mustColor(pretty.TuiMainText))
	}
	return gotui.NewStyle().Foreground(mustColor(pretty.TuiSubText))
}

// refreshHelp 刷新帮助行池：超出的行置空（空文本元素不占行高）。
func (a *agentUI) refreshHelp(items []helpItem) {
	n := len(items)
	if n > helpRowPool {
		n = helpRowPool
	}
	for i := 0; i < helpRowPool; i++ {
		cmdEl := a.helpRows[i*2]
		descEl := a.helpRows[i*2+1]
		if i < n {
			cmdEl.SetText(items[i].cmd)
			descEl.SetText(items[i].desc)
		} else {
			cmdEl.SetText("")
			descEl.SetText("")
		}
	}
}

// buildBannerEl 用 flexbox 拼启动横幅：logo | 信息 | 面板。
// 原版手拼字符串 + TaggedStringWidth 度量 + 三列/堆叠降级的整套逻辑
// 被布局引擎接管：信息列 grow + truncate，窄终端自动压缩，不需要降级分支。
func buildBannerEl(infoLines []string) *gotui.Element {
	subStyle := gotui.NewStyle().Foreground(mustColor(pretty.TuiSubText))

	logo := gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Column),
		gotui.WithWidth(12),
	)
	for i, line := range bannerLogo {
		logo.AddChild(gotui.New(
			gotui.WithText(line),
			gotui.WithTextStyle(gotui.NewStyle().Foreground(mustColor(bannerLogoColors[i]))),
		))
	}

	info := gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Column),
		gotui.WithMinWidth(24),
		gotui.WithFlexGrow(1),
	)
	for _, l := range infoLines {
		info.AddChild(gotui.New(gotui.WithText(l), gotui.WithTextStyle(subStyle), gotui.WithTruncate(true)))
	}

	panel := gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Column),
		gotui.WithBorder(gotui.BorderRounded),
		gotui.WithBorderTitle(" getting started "),
		gotui.WithPadding(1),
	)
	for _, l := range bannerPanelLines {
		panel.AddChild(gotui.New(gotui.WithText(l), gotui.WithTextStyle(subStyle)))
	}

	row := gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Row),
		gotui.WithGap(1),
	)
	row.AddChild(logo)
	row.AddChild(info)
	row.AddChild(panel)
	return row
}
