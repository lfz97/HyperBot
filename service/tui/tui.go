package tui

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"HyperBot/service/engine/runlog"
	"HyperBot/utils/pretty"
	"charm.land/glamour/v2"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// 定义颜色，配色统一来源于 pretty.TuiXxx 常量，确保界面风格统一且美观
var (
	bg          tcell.Color = tcell.GetColor(pretty.TuiBg)          // 整体背景色
	borderColor tcell.Color = tcell.GetColor(pretty.TuiBorderColor) // 边框颜色
	inputAreaBg tcell.Color = tcell.GetColor(pretty.TuiInputAreaBg) // 输入区背景色
)

const (
	// drawInterval 重绘节流间隔。tview 只在 QueueUpdateDraw、按键/鼠标/resize 事件时重绘，
	// 纯流式输出期间这些都不发生，所以由 drawLoop 按固定帧率驱动刷新。
	// pull 架构下它同时是引擎状态的上屏延迟上界：引擎只写 runlog.Store，
	// 本循环每帧比对版本号/快照、把增量渲染出来，帧率即流畅度。
	drawInterval = 30 * time.Millisecond
	// spinnerTicks 指示器每推进一帧占用多少个 drawInterval tick。3 × 30ms = 90ms/帧，
	// 10 帧约 0.9s 转一圈。
	spinnerTicks = 3
)

// EngineView 是 TUI 对引擎的全部依赖（消费方定义的接口）。
// 引擎侧的 *runlog.Store 天然满足它；TUI 不 import engine 包，
// 引擎也不 import 本包——引擎对表现层的依赖为零，控制权全部在 TUI：
// 状态由 TUI 按帧拉取，输入/取消由 TUI 主动调用。
type EngineView interface {
	Version() uint64
	Records(afterSeq int64) []runlog.MsgRecord
	RunState() runlog.RunState
	TodoText() string
	Notice() (runlog.NoticeKind, string, time.Time)
	StartupInfo() ([]string, bool)
	SkillHelpItems() []runlog.HelpItem
	SubmitInput(line string) bool
	Cancel()
}

type Tui struct {
	app       *tview.Application
	appLayout *layout
	engine    EngineView

	// dirty 标记"内容已写入 widget 但尚未上屏"，由 drawLoop 消费。
	// 写入走 QueueUpdate（只改 widget、不重绘）而不是 QueueUpdateDraw：后者每次调用都
	// 阻塞等待一次完整全屏重绘，而 TextView.Draw 内部的 parseAhead 会把整个文本缓冲区
	// 拷贝一遍（t.text.String()），流式输出下就是每 token O(n)、整体 O(n²)。
	dirty    atomic.Bool
	drawOnce sync.Once

	// glamour renderer 构建时要解析一遍 style JSON，按宽度缓存复用
	renderMu sync.Mutex
	renderer *glamour.TermRenderer
	renderW  int
}

type layout struct {
	pages        *tview.Pages
	mainFlex     *tview.Flex // ResizeItem 要在父 Flex 上调，所以得存着
	agentMessage *tview.TextView
	todoBar      *todoBar
	noticeBar    *noticeBar
	indicator    *tview.TextView
	inputArea    *tview.TextArea
	helpTable    *helptable
}

type helptable struct {
	h               *tview.Table
	helpPageVisible bool
	// pull 之后帮助项不再存放于 TUI：默认项在 refreshhelpTable 里写死，
	// 技能项每次打开帮助页时从引擎状态拉取（loadSkills 随时可能重建列表）。
}

type helpItem struct {
	cmd  string
	desc string
}

// ── 输入区左侧的运行指示器 ─────────────────────────────
//
// 做成独立 widget 而不是 TextArea 的 label：TextArea.SetLabel 是无锁写字段
// （textarea.go:792），由 UI 线程在 Draw 中读取，跨 goroutine 调用是数据竞争，
// 每帧都得包一层 QueueUpdate；而 TextView.SetText 自带锁，drawLoop 可以直接调。

const indicatorWidth = 2

var (
	// 空闲态。indicator 开了 SetDynamicColors，所以可以带 tview 颜色标签。
	// 用 TuiSubText 而不是 tview 默认的 label 颜色——后者是 ColorYellow。
	idleIndicator = pretty.TColoredText(pretty.TuiSubText, "> ")
	// 盲文 10 帧，每个都是 1 cell 宽（已实测），加一个空格正好 indicatorWidth 列
	spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
)

// spinnerIndicator 渲染运行态的第 frame 帧。frame 由 drawLoop 单调递增传入，此处取模回绕。
func spinnerIndicator(frame int) string {
	return pretty.TColoredText(pretty.TColorLightMagenta,
		string(spinnerFrames[frame%len(spinnerFrames)])+" ")
}

// ── 输入框上方的两个 bar ─────────────────────────────
//
// TodoBar：纵向清单栏，每个任务一行，有清单时占 N 行、无清单时塌成 0 行。
// NoticeBar：固定 1 行，承载瞬时通知与常驻键位提示，永不塌陷（hint 常驻）。
// pull 之后两者都不再持有跨 goroutine 状态：清单文本与通知槽位都在引擎的
// runlog.Store 里，drawLoop 每帧拉取，widget 写入保持单线程（drawLoop）。

const (
	// noticeTTL 临时通知的停留时长。到期后 NoticeBar 自动回落到兜底提示。
	// 契约不变：TTL 从引擎写入槽位那一刻（SetAt）起算。
	noticeTTL = 4 * time.Second
	// minMessageRows 消息区无论如何至少保留的行数。矮终端下用它钳制 TodoBar 高度：
	// Flex 的 distSize = height - 所有 fixedSize 之和且不做钳制，Box.SetRect 也原样
	// 存负高度，而 pos += size 用原始值 → 不钳制会让 AgentMessage 拿到负高度、
	// pos 倒退、下方三个 item 全部画错行并在屏幕底部留残影。
	minMessageRows = 3
)

// NoticeBar 的常驻兜底提示。全小写英文，沿用被删除的 InputHint 的 [gray::d] 暗灰样式。
// esc to interrupt 只在运行态出现——ESC 中断仅在 agent 运行期间有效，平时显示它是噪音。
const (
	hintIdle    = `[gray::d]ctrl+k for help[-:-:-]`
	hintRunning = `[gray::d]esc to interrupt · ctrl+k for help[-:-:-]`
)

// noticeBar 输入框上方的单行通知栏。notice 与 hint 二选一，一次只显示一种。
type noticeBar struct {
	view *tview.TextView
}

// todoBar 输入框上方的纵向清单栏（每个任务一行）。有清单时占 N 行，无清单时塌成 0 行。
type todoBar struct {
	view *tview.TextView
}

// drawState 是 drawLoop 的私有状态。只被 drawLoop 这一个 goroutine 读写，
// 不是共享状态，因此无需同步（刻意不做成 Tui 的字段，避免误导后人以为要加锁）。
type drawState struct {
	showingRunning bool   // indicator 当前显示的是否为运行态
	spinTick       int    // spinner 帧推进计数
	shownTodo      string // TodoBar 上一帧的原始文本
	shownNotice    string // NoticeBar 上一帧的渲染结果
	seenVersion    uint64 // 已渲染到的消息日志版本
	seenSeq        int64  // 已渲染到的最后一条记录 Seq
	streamRaw      string // 当前正文流的原文累积（ContentDelta 之间），供定稿尾替换
	bannerDone     bool   // 启动横幅已渲染（只在 StartupInfo 就绪后的第一帧渲染一次）
	fatalHandled   bool   // 终态已处理（渲染 + 停止事件循环）
}

// startDrawLoop 启动固定帧率的重绘循环，只启动一次。
// 它是 pull 架构的心跳：每帧从引擎状态拉取五类东西——启动横幅（就绪后一次）、
// 消息日志增量（按版本号）、运行指示器、清单栏、通知栏——外加终态观察。
// 所有 widget 写入因此都是单线程的，配合 TextView.SetText 自带锁，
// 既不需要额外同步也不需要为每件事单起 ticker。
func (t *Tui) startDrawLoop() {
	t.drawOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(drawInterval)
			defer ticker.Stop()

			st := &drawState{}
			for range ticker.C {
				rs := t.engine.RunState()
				t.tickBanner(st)
				t.tickMsgLog(st)
				t.tickIndicator(rs.Running, st)
				t.tickTodoBar(st)
				t.tickNoticeBar(rs.Running, st)
				t.tickFatal(rs.Fatal, st)
				// 没有新内容就不重绘，空闲时不产生任何 CPU 开销
				if t.dirty.CompareAndSwap(true, false) {
					t.app.Draw()
				}
			}
		}()
	})
}

// tickBanner 在引擎就绪 StartupInfo 后的第一帧渲染启动横幅（只渲染一次）。
// 横幅内容是引擎的数据、排版是 TUI 的表达——所以数据走拉取，渲染留在本侧。
func (t *Tui) tickBanner(st *drawState) {
	if st.bannerDone {
		return
	}
	lines, ok := t.engine.StartupInfo()
	if !ok {
		return // 引擎 init 未完成，等下一帧再拉
	}
	st.bannerDone = true
	t.renderStartupBanner(lines)
}

// tickMsgLog 比对消息日志版本号，把新记录渲染进消息区。
// 版本号与记录是两次加锁读取，中间新到的记录下一帧自然会补上——拉的是状态，
// 丢帧无损。空闲（版本未变）时直接返回，零成本。
func (t *Tui) tickMsgLog(st *drawState) {
	ver := t.engine.Version()
	if ver == st.seenVersion {
		return
	}
	recs := t.engine.Records(st.seenSeq)
	if len(recs) > 0 {
		for _, rec := range recs {
			t.renderRecord(rec, st)
		}
		st.seenSeq = recs[len(recs)-1].Seq
	}
	st.seenVersion = ver
}

// renderRecord 把一条领域记录翻译成带色文本并写入消息区。
// 这是"引擎只说发生了什么、TUI 决定怎么画"的落点：所有 pretty.* 配色都活在本侧，
// 引擎日志里只有语义原文。每种 Kind 的渲染与旧版引擎侧 PrintToMsgView 的拼串逐字节对齐。
func (t *Tui) renderRecord(rec runlog.MsgRecord, st *drawState) {
	switch rec.Kind {
	case runlog.KindNewline:
		t.appendMsg("\n")
	case runlog.KindUser:
		t.appendMsg(pretty.TUserInput(rec.Text))
	case runlog.KindSlashEcho:
		t.appendMsg(pretty.TColoredText(pretty.TColorLightGreen, "\n"+rec.Text+"\n"))
	case runlog.KindReasoning:
		t.appendMsg(pretty.TReasoningContent(rec.Text))
	case runlog.KindContentDelta:
		st.streamRaw += rec.Text
		t.appendMsg(rec.Text)
	case runlog.KindContentFinal:
		t.appendFinal(rec.Text, st)
	case runlog.KindTool:
		t.appendMsg(pretty.TToolCompact(rec.Tool.Name, []byte(rec.Tool.In), rec.Tool.Out))
	case runlog.KindWarn:
		t.appendMsg(pretty.TWarningF("%s", rec.Text))
	case runlog.KindErrorLine:
		t.appendMsg(pretty.TErrorF("%s", rec.Text))
	case runlog.KindSummary:
		t.appendMsg(pretty.TColoredText(pretty.TColorGreen, "\n->已生成摘要：\n"+rec.Text+"\n"))
	default:
		t.appendMsg(rec.Text)
		return
	}
	// 正文流边界维护：定稿消费掉累积原文；其余带样式/持久行都意味着"新的一段"，
	// 之前的原文不再连续。Newline 与 Reasoning 刻意不清空——它们出现在思考块
	// 边界上，若插在正文中间，尾替换会像旧版一样自然失败、保住 raw。
	switch rec.Kind {
	case runlog.KindContentFinal,
		runlog.KindUser, runlog.KindSlashEcho, runlog.KindTool,
		runlog.KindWarn, runlog.KindErrorLine, runlog.KindSummary:
		st.streamRaw = ""
	}
}

// appendFinal 渲染定稿正文：流式路径做"原文尾部 → markdown 渲染版"的替换，
// 非流式路径（没有任何累积原文）直接追加渲染版。语义与旧版
// ReplaceTailInMsgView(renderBody(content)) 逐一对齐。
func (t *Tui) appendFinal(full string, st *drawState) {
	rendered := t.renderBody(full)
	raw := st.streamRaw
	t.app.QueueUpdate(func() {
		if raw != "" {
			buf := t.appLayout.agentMessage.GetText(false)
			if newBuf, replaced := mergeTail(buf, raw, rendered); replaced {
				// SetText 只调 resetIndex()，不动 lineOffset / trackEnd，所以滚动位置保得住
				t.appLayout.agentMessage.SetText(newBuf)
				return
			}
		}
		// 非流式、或替换失败（raw 已不在 buffer 末尾——中途被别的记录插过）。
		// 绝不能退化成"再追加一份原文"，只补渲染版即可；替换失败最多退化成
		// "没有 markdown 渲染"，改错位置才是毁掉整屏。
		fmt.Fprint(t.appLayout.agentMessage, rendered)
	})
	t.markDirty()
}

// appendMsg 向消息区追加一段文本。等价旧 PrintToMsgView(content, false)——
// clear 参数全仓库无人传 true（死参数），pull 化时顺手删除。
func (t *Tui) appendMsg(content string) {
	t.app.QueueUpdate(func() {
		fmt.Fprint(t.appLayout.agentMessage, content)
	})
	// 这里不调 ScrollToEnd()。它会把 TextView 的 trackEnd 强行置回 true，于是用户在
	// 流式输出期间往上翻滚查看历史时，下一个 token 就把视图弹回底部。trackEnd 本身就是
	// "是否在底部、是否该跟随"的标记，tview 自己维护（滚轮上翻置 false、翻回底部置 true），
	// 初始值在 NewTui 里开一次即可。
	t.markDirty()
}

// mergeTail 把 buffer 末尾的 raw 替换为 replacement，返回新 buffer 与是否替换成功。
// 只在 raw 正好位于 buffer 末尾时才替换；用 LastIndex 而不是 Replace——短回复
// （如"好的。"）可能在前面出现过（比如用户输入经回显进了同一个 buffer）。
func mergeTail(buf, raw, replacement string) (string, bool) {
	// LastIndex(buf, "") 返回 len(buf) 而不是 -1，不挡会在末尾凭空追加一份正文
	if raw == "" {
		return buf, false
	}
	i := strings.LastIndex(buf, raw)
	if i < 0 || i+len(raw) != len(buf) {
		return buf, false
	}
	return buf[:i] + replacement, true
}

// ── markdown 渲染（自 messageRender.renderBody 迁入：渲染是表达层的事） ──

// glamourTailPad 匹配行尾的"空白 + ANSI 序列"混合填充。glamour 会把标题、表格、
// 代码块的每一行都补满整行宽度，每个空格还裹一层 SGR：实测 153B 的 markdown 渲染后
// 是 12.7KB，其中 92% 是这种填充。消息区 TextView 在进程生命周期内从不清空，且每帧
// 全量重绘、每次替换都要扫一遍整个 buffer，所以必须剥掉。
var glamourTailPad = regexp.MustCompile(`(?:\x1b\[[0-9;]*m|[ \t])+$`)

// renderBody 用 glamour 渲染 markdown，TranslateANSI 转为 tview 颜色标签。
// 流式定稿与非流式两条路径共用，保证两种模式的最终观感一致。
//
// 正文标记必须加在渲染结果上，不能加在 markdown 源码前面：`● ` 会让首行的块级结构失效
// ——实测代码围栏和表格会整个塌成一行、列表首项不再被识别、标题降级成普通段落。
func (t *Tui) renderBody(content string) string {
	out, err := t.renderMarkdown(content)
	if err != nil {
		out = content // 渲染失败退回原文，别把整条回复吞掉
	}

	// glamour 的输出恒以一个换行开头，不剥掉的话标记会独占一行、与正文脱开
	out = strings.TrimRight(strings.TrimLeft(out, "\n\r"), "\n\r ")
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		l = glamourTailPad.ReplaceAllString(l, "")
		// 剥填充会连带剥掉行尾闭合样式的 reset，不补回则颜色泄漏到后续行
		// （实测不补会留下 2~5 个未闭合标签）
		if l != "" && !strings.HasSuffix(l, "\x1b[m") {
			l += "\x1b[m"
		}
		lines[i] = l
	}
	// 标记加在第一个有可见内容的行上：正文以代码块或表格开头时，
	// 剥完填充后首行是空的，直接前置会让 ● 独占一行
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			lines[i] = pretty.TContentNoneStreamTag(l)
			break
		}
	}
	return tview.TranslateANSI(strings.Join(lines, "\n")) + "[-:-:-]"
}

// renderMarkdown 用 glamour 渲染 markdown（原 TuiService.RenderMarkdown 内化为本侧私有方法）。
func (t *Tui) renderMarkdown(in string) (string, error) {
	r, err := t.glamourRenderer()
	if err != nil {
		return "", err
	}
	return r.Render(in)
}

// glamourRenderer 返回按当前消息区宽度缓存的 renderer，宽度变化时才重建。
func (t *Tui) glamourRenderer() (*glamour.TermRenderer, error) {
	// 宽度读取与竞争规避见 banner.go 里 contentWidth 的注释
	w := t.contentWidth()
	if w < 40 {
		w = 80
	}

	t.renderMu.Lock()
	defer t.renderMu.Unlock()
	if t.renderer != nil && t.renderW == w {
		return t.renderer, nil
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
	t.renderer, t.renderW = r, w
	return r, nil
}

// ── 指示器 / 两个 bar 的帧刷新 ─────────────────────────

// tickIndicator 推进运行指示器。
// 用 if/else-if 而非 switch —— 项目风格要求，不要改回 switch。
func (t *Tui) tickIndicator(running bool, st *drawState) {
	if running != st.showingRunning { // 状态翻转：立刻换外观，帧计数归零
		st.showingRunning = running
		st.spinTick = 0
		if running {
			t.setIndicator(spinnerIndicator(0))
		} else {
			t.setIndicator(idleIndicator)
		}
	} else if running { // 持续运行：每 spinnerTicks 个 tick 推进一帧
		st.spinTick++
		if st.spinTick%spinnerTicks == 0 {
			t.setIndicator(spinnerIndicator(st.spinTick / spinnerTicks))
		}
	}
}

// tickNoticeBar 刷新通知栏。TTL 到期、通知到达、running 翻转三件事全走这一条路径，
// 因此不需要任何事件驱动的机制。通知槽位在引擎侧（runlog.Store），TTL 从 SetAt 起算。
func (t *Tui) tickNoticeBar(running bool, st *drawState) {
	nb := t.appLayout.noticeBar
	kind, text, setAt := t.engine.Notice()
	s := hintIdle
	if kind != runlog.NoticeNone && time.Since(setAt) < noticeTTL {
		s = renderNotice(kind, text)
	} else if running {
		s = hintRunning
	}
	if s != st.shownNotice {
		st.shownNotice = s
		nb.view.SetText(s) // TextView.SetText 自带锁，无需 QueueUpdate
		t.dirty.Store(true)
	}
}

// renderNotice 把通知槽位翻译成带色文本。文案配色在这里（表达层），
// 引擎只存 Kind 与原文；无文本的固定文案（新对话/取消）也由本侧拼装。
func renderNotice(kind runlog.NoticeKind, text string) string {
	switch kind {
	case runlog.NoticeNewConversation:
		return pretty.TBarNewConversation()
	case runlog.NoticeCancelled:
		return pretty.TBarCancelled()
	case runlog.NoticeSuccess:
		return pretty.TBarSuccess(text)
	case runlog.NoticeWarning:
		return pretty.TBarWarning(text)
	default:
		return pretty.TColoredText(pretty.TuiSubText, text)
	}
}

// tickTodoBar 刷新清单栏的内容与高度（0 到 N 行，每个任务一行）。
//
// 高度钳制的原因：Flex 的 distSize = height - 所有 fixedSize 之和且不做钳制，Box.SetRect
// 也原样存负高度，而 pos += size 用原始值。终端高度不足时 distSize
// 变负 → AgentMessage 拿到负高度 → pos 倒退 → 下方三个 item 全部画错行、
// 屏幕底部留未绘制残影。行数超过 max 时压到 max（顶部行可见，其余被挡住）；
// max 不足 1 时塌成 0 行。
//
// 多行后必须 ScrollToBeginning()：SetText 只调 resetIndex()，不清
// lineOffset/trackEnd，而 NewTextView() 默认 scrollable=true，滚轮下滚会把
// trackEnd 置真、导致视图永久卡在底部。SetScrollable(false) 不是解法
// （它会顺手把 trackEnd 设成 true，直接底部锚定）。
func (t *Tui) tickTodoBar(st *drawState) {
	tb := t.appLayout.todoBar
	text := t.engine.TodoText()
	if text == st.shownTodo {
		return
	}
	st.shownTodo = text

	rendered := ""
	n := 0
	if text != "" {
		rendered = renderTodoLines(text)
		n = strings.Count(text, "\n") + 1
	}

	// ResizeItem 无锁写 FixedSize/Proportion、GetInnerRect 无锁读布局字段，两者都
	// 由 UI 线程在 Draw 中使用，所以必须一起放进 QueueUpdate（那里就是 UI 线程）。
	t.app.QueueUpdate(func() {
		_, _, _, total := t.appLayout.mainFlex.GetInnerRect()
		// NoticeBar 与 InputRow 各占 1 行，消息区至少留 minMessageRows 行
		if max := total - 2 - minMessageRows; n > max {
			n = max
		}
		if n < 0 {
			n = 0
		}
		tb.view.SetText(rendered)
		tb.view.ScrollToBeginning()
		// proportion 必须是 0：给 1 会让 TodoBar 与 AgentMessage 平分剩余空间
		t.appLayout.mainFlex.ResizeItem(tb.view, n, 0)
	})
	t.dirty.Store(true)
}

// renderTodoLines 把 TodoBar 的多行纯文本转义并逐行上色：
// [TODO] 头行与 (N done) 计数行暗灰、◐ 进行中青色、☐ 待办正文色。
// 必须逐行先 tview.Escape 再包颜色标签："[TODO]" 会被 tview 当成前景色名整个吞掉
// （实测 "[TODO] x" 的 TaggedStringWidth 是 2 而非 8），条目正文由 LLM 撰写、
// 可能含 ASCII 方括号。Escape 与着色都不改变行数，高度计算不受影响。
func renderTodoLines(text string) string {
	colorOf := func(line string) string {
		switch {
		case strings.HasPrefix(line, "◐ "):
			return pretty.TColorCyan
		case strings.HasPrefix(line, "☐ "):
			return pretty.TuiMainText
		default: // [TODO] 头行、计数行
			return pretty.TuiSubText
		}
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = pretty.TColoredText(colorOf(line), tview.Escape(line))
	}
	return strings.Join(lines, "\n")
}

// tickFatal 观察进程终态。引擎置 Fatal 后只写状态、不再推进任何流程，
// 收尾的执行权在本侧：渲染消息 → 强制上屏 → （按需）等任意键 → 停止事件循环。
// 之后的收尾链路是：app.Stop() → tui.Run() 返回 → tview 复原终端 → main() 返回 → 进程退出。
// 不能在回调里直接 os.Exit：screen.Fini() 是在 app.Run() 的返回路径上调的，
// 从回调硬退出会跳过它，终端会留在 alt-screen + raw mode，退出后用户的 shell 是坏的。
func (t *Tui) tickFatal(fatal *runlog.Fatal, st *drawState) {
	if st.fatalHandled || fatal == nil {
		return
	}
	st.fatalHandled = true
	t.appendMsg(renderFatal(fatal))
	// 强制刷一帧：appendMsg 只写入不重绘，不显式 Draw 的话退出信息
	// 可能还没上屏 app 就 Stop 了。
	t.app.Draw()
	t.app.QueueUpdate(func() {
		if !fatal.WaitKey {
			t.app.Stop()
			return
		}
		// 只要有按键就退出程序
		t.app.SetFocus(t.appLayout.agentMessage)
		t.appLayout.agentMessage.SetInputCapture(
			func(event *tcell.EventKey) *tcell.EventKey {
				t.app.Stop()
				return nil
			})
	})
}

// renderFatal 把终态消息按样式上色（原 ShowSuccessInMsgViewAndExit 里 TSuccess
// 在 TUI 侧套色的契约推广到全部样式；引擎只存语义原文）。
func renderFatal(f *runlog.Fatal) string {
	switch f.Style {
	case runlog.FatalSuccess:
		return pretty.TSuccess(f.Text)
	case runlog.FatalExit:
		return pretty.TExit(f.Text)
	case runlog.FatalError:
		return pretty.TErrorF("%s", f.Text)
	default:
		return f.Text
	}
}

// setIndicator 更新指示器文本并请求重绘。只允许 drawLoop 调用。
// TextView.SetText 自带锁，可以直接从本 goroutine 调，不需要 QueueUpdate。
func (t *Tui) setIndicator(s string) {
	t.appLayout.indicator.SetText(s)
	t.dirty.Store(true)
}

// markDirty 标记需要重绘。必须在 widget 写入完成之后调用：QueueUpdate 是阻塞的，
// 返回时内容已经落地，这样 drawLoop 的下一帧才画得到它。反过来（先标记后写入）
// 会出现"这一帧把标记消费掉了、内容却还没写进去"，导致最后一批文本永不上屏。
func (t *Tui) markDirty() {
	t.startDrawLoop()
	t.dirty.Store(true)
}

// Run 启动 tview 事件循环（阻塞到 app.Stop()）。
func (t *Tui) Run() {
	// 帧循环必须在 app.Run() 之前启动：pull 架构下引擎只写状态、不会调用 TUI，
	// 没有任何"推送"会触发第一次 markDirty，帧轮询必须自己跑起来。
	t.startDrawLoop()
	if err := t.app.Run(); err != nil {
		panic("Error running application: " + err.Error())
	}
}

// NewTui 组装界面与交互捕获。view 是引擎侧状态源（*runlog.Store）。
func NewTui(view EngineView) *Tui {
	//设置Agent消息显示区
	AgentMessage := tview.NewTextView().
		SetDynamicColors(true). // 启用颜色
		SetScrollable(true).    // 可滚动
		SetWrap(true)

	AgentMessage.SetBackgroundColor(bg) // 设置背景颜色
	// 打开"跟随底部"。SetScrollable(true) 不会设置 trackEnd（只有传 false 才会），
	// 所以必须显式开一次，否则视图会永远停在顶部、完全不跟随新输出。
	// 之后用户在底部就继续跟随、往上翻就不打扰，全部由 tview 自己维护。
	AgentMessage.ScrollToEnd()

	// 输入区左侧的运行指示器：空闲显示 ">"，agent 运行中显示盲文 spinner，由 drawLoop 驱动。
	// 背景必须是 inputAreaBg：这个位置原本在 TextArea 内部、底色就是 inputAreaBg，
	// 不设的话会继承 InputRow 的 bg，左边出现一块 2 格的色差。
	Indicator := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false).
		SetText(idleIndicator)
	Indicator.SetBackgroundColor(inputAreaBg)

	//设置底部输入区（不再有 label，提示符由 Indicator 承担）
	InputArea := tview.NewTextArea().SetWrap(true)
	InputArea.SetBackgroundColor(inputAreaBg)
	InputArea.SetTextStyle(tcell.StyleDefault.
		Background(inputAreaBg).                        // 输入区背景色
		Foreground(tcell.GetColor(pretty.TuiMainText))) // 文字颜色

	// 消息区与通知栏之间的清单栏：纵向，每个任务一行，有清单时占 N 行、无清单时塌成 0 行。
	// 背景用 bg（不是 inputAreaBg）：它是参考信息，视觉上应融入消息区，
	// 与下方"控制区"（NoticeBar + InputRow）区分开。
	// 高度由 drawLoop 通过 mainFlex.ResizeItem 动态设置，初始 0 行。
	// 不要对它调 SetScrollable(false)——那会把 trackEnd 置真（见 tickTodoBar 注释）。
	TodoBar := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false)
	TodoBar.SetBackgroundColor(bg)

	// 输入框上方的通知栏：瞬时通知 + 常驻键位提示，固定 1 行、永不塌陷。
	// 不设背景色，继承 MainFlex 的 bg——让它融入消息区而不是和 InputRow 连成一块。
	// 内容全部带颜色标签，所以不需要 SetTextColor。
	// 内容靠右：与原来 InputHint 在 InputRow 右侧的位置一致，视线落点不变。
	// 注意通知与兜底提示共用这一个 widget，所以两者都靠右。
	NoticeBar := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(false).
		SetTextAlign(tview.AlignRight).
		SetText(hintIdle)

	InputRow := tview.NewFlex().SetDirection(tview.FlexColumn)
	InputRow.SetBackgroundColor(bg)
	InputRow.AddItem(Indicator, indicatorWidth, 0, false) // 左侧运行指示器
	InputRow.AddItem(InputArea, 0, 1, true)

	//设置整体布局
	MainFlex := tview.NewFlex().SetDirection(tview.FlexRow)
	MainFlex.SetBackgroundColor(bg)
	MainFlex.AddItem(AgentMessage, 0, 1, false) // Agent消息区占剩余空间
	MainFlex.AddItem(TodoBar, 0, 0, false)      // 清单栏，初始 0 行，由 ResizeItem 动态调整
	MainFlex.AddItem(NoticeBar, 1, 0, false)    // 通知栏，固定 1 行、永不塌陷
	MainFlex.AddItem(InputRow, 1, 0, true)      // 底部的输入区

	HelpTable := tview.NewTable()
	HelpTable.SetBackgroundColor(bg)
	HelpTable.SetBorder(true)
	HelpTable.SetBorderColor(borderColor)
	HelpTable.SetTitle(" 斜杠指令 — Ctrl+K / Esc 关闭 ")
	HelpTable.SetTitleAlign(tview.AlignLeft)
	HelpTable.SetSelectable(true, false) // 行可选，列不可选

	HelpTable.SetSelectedStyle(tcell.StyleDefault.
		Background(tcell.GetColor("#2A3A5C")).
		Foreground(tcell.GetColor(pretty.TuiMainText)))

	app := tview.NewApplication()
	pages := tview.NewPages()
	pages.AddPage("AgentPage", MainFlex, true, true)
	app.SetRoot(pages, true) // true = 全屏模式
	app.EnableMouse(true)    //允许接收鼠标事件
	app.EnablePaste(true)    //启用 bracketed paste，避免长文本粘贴时逐字符处理导致 CPU 飙升和界面卡死

	tui := &Tui{
		app: app,
		appLayout: &layout{
			pages:        pages,
			mainFlex:     MainFlex,
			agentMessage: AgentMessage,
			todoBar:      &todoBar{view: TodoBar},
			noticeBar:    &noticeBar{view: NoticeBar},
			indicator:    Indicator,
			inputArea:    InputArea,
			helpTable: &helptable{
				h:               HelpTable,
				helpPageVisible: false,
			},
		},
		engine: view,
	}

	// 注册输入捕获器，每次用户在输入框敲击键盘时都会触发。
	// 原 ListenUserInput 的注册时机在本函数（app.Run 之前直接设置，无需 QueueUpdate）。
	InputArea.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// Ctrl+K 切换帮助页
		if event.Key() == tcell.KeyCtrlK {
			tui.toggleHelpPage()
			return nil
		}

		// Enter 提交输入
		// ModNone = 0，无任何修饰键（Ctrl/Shift/Alt 均未按下），即裸按 Enter。
		// Shift+Enter 落到函数末尾的 return event，由 TextArea 插入换行（手动多行输入）。
		// bracketed paste 保证粘贴里的 \n 走 PasteEvent 通道，不会产生 KeyEnter 事件
		if event.Key() == tcell.KeyEnter && event.Modifiers() == tcell.ModNone {

			//获取输入文本
			text := tui.appLayout.inputArea.GetText()
			// 主动提交给引擎（Store.SubmitInput 内部 select/default，非阻塞）。
			// 引擎忙（无人接收）时提交失败、保留输入框内容，用户输入不丢失——
			// 与旧版 unbuffered chan + default 的行为一致。
			if tui.engine.SubmitInput(text) {
				tui.appLayout.inputArea.SetText("", false)
			}
			return nil //Enter事件不捕获
		}

		//传递事件给 TextArea 默认处理（插入字符、换行等）
		return event
	})

	// 应用级 Esc 捕获：运行期按 Esc 中断当前 agent（cancel 由引擎经 SetActiveCancel 注入，
	// 方向与旧版 SetAppFuncTriggerWithEsc 相反——不再由引擎往 TUI 注册回调）。
	// 非运行期放行，让 Esc 落到帮助页的关闭捕获上（与旧版"仅运行期注册"的行为一致）。
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape && view.RunState().Running {
			view.Cancel() // 执行取消
			return nil
		}
		return event // 其他按键正常传递
	})

	tui.refreshhelpTable()
	return tui
}
