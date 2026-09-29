package tui

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"HyperBot/utils/pretty"
	gotui "github.com/grindlemire/go-tui"
)

const (
	// tickInterval staging → 元素的物化心跳。沿用 tview 版 drawLoop 的 30ms：
	// 流式输出期间框架自身没有重绘事件源，节流全靠这个 tick。
	tickInterval = 30 * time.Millisecond
	// spinnerTicks 每 3 个 tick（90ms）推进一帧 spinner，10 帧约 0.9s 一圈。
	spinnerTicks = 3
	// noticeTTL 临时通知停留时长，到期回落兜底提示。
	noticeTTL = 4 * time.Second
	// indicatorWidth 输入行左侧指示器宽度。
	indicatorWidth = 2
	// minMessageRows 矮终端下消息区至少保留的行数（钳制清单栏高度）。
	minMessageRows = 3
	// 两个行池的大小：todo 每任务一行、帮助每条目一行。
	todoLinePool = 24
	helpRowPool  = 32
)

var spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// NoticeBar 常驻兜底提示（保留 tview 标签形式，由 tagbridge 解析上色）。
const (
	hintIdle    = `[gray::d]ctrl+k for help[-:-:-]`
	hintRunning = `[gray::d]esc to interrupt · ctrl+k for help[-:-:-]`
)

// segKind 消息段类型。PrintToMsgView 的连续调用会合并进最近的 segRaw 段
// （对应 tview 版"同一个 TextView 缓冲"的语义），ReplaceTail 把尾段原位转成 markdown。
type segKind int

const (
	segRaw      segKind = iota // 流式原文（纯文本，随 delta 增量增长）
	segMarkdown                // 渲染完成的 markdown（go-tui 内置渲染）
	segRich                    // tview 标签文本（工具块/用户回显/摘要/退出消息）
)

type msgSeg struct {
	id   int
	kind segKind
	text string        // raw：累计原文；markdown：源码；rich：标签文本
	el   *gotui.Element // raw/rich 的保留元素
	md   *gotui.Markdown
}

type helpItem struct{ cmd, desc string }

type Tui struct {
	app *gotui.App
	ui  *agentUI
	inputChan chan string

	// ── staging：引擎 goroutine 写（全部走 mu，永不阻塞），
	// tick 在主循环物化到元素。这是 tview 版"mutex 字段 + drawLoop"的直译，
	// 也是对 go-tui QueueUpdate"队列满则丢弃"语义的规避：消息内容绝不能丢。──
	mu          sync.Mutex
	segs        []*msgSeg
	buf         string // 平面文本缓冲，复刻 TextView.GetText 的 LastIndex 语义
	msgVersion  uint64
	todoText    string
	noticeMsg   string
	noticeUntil time.Time
	helpItems   []helpItem
	helpVersion int
	bannerLines []string
	bannerSet   bool
	escFn       func()

	// ── tick 私有：仅主循环读写，无锁 ──
	appliedVersion     uint64
	lastTodo           string
	lastNotice         string
	appliedHelpVersion int
	bannerDone         bool
	lastRun            bool
	spinTickN          int
	segID              int

	running atomic.Bool
	exiting atomic.Bool
}

// GetTuiService 构造 TUI 服务。引擎把它当 requirements.TuiService 用，
// boot.go 在独立 goroutine 里跑引擎、主 goroutine 跑 Run()。
func GetTuiService() *Tui {
	t := &Tui{inputChan: make(chan string)}
	t.ui = newAgentUI(t)
	app, err := gotui.NewApp(
		gotui.WithRootComponent(t.ui),
		gotui.WithMouse(),
		gotui.WithGlobalKeyHandler(t.globalKeys),
	)
	if err != nil {
		panic("tui: 创建 go-tui App 失败: " + err.Error())
	}
	t.app = app
	t.ResetHelpItems()
	return t
}

// globalKeys 全局按键拦截：exiting 时任意键退出；Ctrl+K 切帮助页；
// Esc 转发引擎注册的中断回调（弹层打开时让位给 closeOnEscape）。
func (t *Tui) globalKeys(ke gotui.KeyEvent) bool {
	if t.exiting.Load() {
		t.app.Stop()
		return true
	}
	if ke.Key == gotui.KeyRune && ke.Rune == 'k' && ke.Mod == gotui.ModCtrl {
		t.ui.helpOpen.Update(func(b bool) bool { return !b })
		// helpOpen 未走 AppBinder 绑定，不会自动标脏，这里显式刷一帧
		t.app.MarkDirty()
		return true
	}
	if ke.Key == gotui.KeyEscape {
		if t.ui.helpOpen.Get() {
			return false // 弹层打开时交给 modal 的 closeOnEscape
		}
		t.mu.Lock()
		f := t.escFn
		t.mu.Unlock()
		if f != nil {
			f()
			return true
		}
	}
	return false
}

// submitInput textarea 的提交回调（主循环执行）。
// default 分支与 tview 版语义一致：引擎未在监听（自动 turn 期间）时保留文本，
// 只有投递成功才清空输入框。
func (t *Tui) submitInput(text string) {
	select {
	case t.inputChan <- text:
		t.ui.textarea.Clear()
	default:
	}
}

func (t *Tui) tick() { t.ui.tickAndFlush() }

// ── TuiService 接口实现 ─────────────────────────────
// 所有方法的线程契约与 tview 版一致：引擎侧任意 goroutine 可调、不阻塞；
// 真正的 UI 变更统一在 tick（主循环）落地。

func (t *Tui) PrintToMsgView(content string, clear bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if clear {
		t.resetMsgsLocked()
	}
	if content == "" {
		return
	}
	t.buf += content
	// 连续的流式 delta 合并进同一个 raw 段：tick 里只需 SetText 一次，
	// 元素数量不随 token 数膨胀
	if n := len(t.segs); n > 0 && t.segs[n-1].kind == segRaw {
		t.segs[n-1].text += content
	} else {
		t.segID++
		t.segs = append(t.segs, &msgSeg{id: t.segID, kind: segRaw, text: content})
	}
	t.msgVersion++
}

// resetMsgsLocked 清空平面缓冲与段列表。目前引擎没有 clear=true 的调用方，
// 但元素层的 AddChild 没有逆操作，旧元素只能置空文本（占零行高）。
func (t *Tui) resetMsgsLocked() {
	t.buf = ""
	for _, s := range t.segs {
		if s.el != nil {
			s.el.SetText("")
		}
	}
	t.segs = nil
	t.msgVersion++
}

// ReplaceTailInMsgView 把消息区末尾的 raw 文本替换成渲染版，返回是否替换成功。
// 平面缓冲上的判断与 tview 版逐字节等价（LastIndex + 尾部对齐）；
// 元素层额外要求尾部正好是最后一个 raw 段——连续 delta 已合并进单段，
// 正常流式路径必然满足，极端交错场景宁可放弃替换也不改历史。
func (t *Tui) ReplaceTailInMsgView(raw string, replacement string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if raw == "" {
		return false
	}
	i := strings.LastIndex(t.buf, raw)
	if i < 0 || i+len(raw) != len(t.buf) {
		return false
	}
	n := len(t.segs)
	if n == 0 || t.segs[n-1].kind != segRaw || !strings.HasSuffix(t.segs[n-1].text, raw) {
		return false
	}
	t.buf = t.buf[:i] + replacement
	last := t.segs[n-1]
	last.text = strings.TrimSuffix(last.text, raw)
	t.segID++
	t.segs = append(t.segs, &msgSeg{id: t.segID, kind: segMarkdown, text: replacement})
	t.msgVersion++
	return true
}

// SetTodoText 更新清单栏文本，空串整栏塌成 0 行。
// 收到的是框架/LLM 的多行纯文本——go-tui 没有标签语法，无需转义。
func (t *Tui) SetTodoText(text string) {
	t.mu.Lock()
	t.todoText = text
	t.mu.Unlock()
}

// ShowNotice 显示一条临时通知，noticeTTL 后回落兜底提示。
// msg 是 pretty.TBarXxx 生成的受信 tview 标签 markup，tagbridge 负责解析上色。
func (t *Tui) ShowNotice(msg string) {
	t.mu.Lock()
	t.noticeMsg = msg
	t.noticeUntil = time.Now().Add(noticeTTL)
	t.mu.Unlock()
}

// SetAgentRunning 标记 agent 运行态。只写原子标志，指示器切换由 tick 完成。
func (t *Tui) SetAgentRunning(running bool) {
	t.running.Store(running)
}

// ShowStartupBanner 物化启动横幅。布局交给 flexbox：
// 信息列 grow + truncate，窄终端自动压缩，无需原版的宽度度量与堆叠降级。
func (t *Tui) ShowStartupBanner(infoLines []string) {
	t.mu.Lock()
	t.bannerLines = infoLines
	t.bannerSet = true
	t.mu.Unlock()
}

func (t *Tui) AddHelpItems(items []map[string]string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, i := range items {
		for k, v := range i {
			t.helpItems = append(t.helpItems, helpItem{cmd: k, desc: v})
		}
	}
	t.helpVersion++
}

func (t *Tui) ResetHelpItems() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.helpItems = []helpItem{
		{"/new", "开始新对话"},
		{"/exit", "退出程序"},
	}
	t.helpVersion++
}

func (t *Tui) SetAppFuncTriggerWithEsc(f func()) {
	t.mu.Lock()
	t.escFn = f
	t.mu.Unlock()
}

func (t *Tui) ClearAppFuncTrigger() {
	t.mu.Lock()
	t.escFn = nil
	t.mu.Unlock()
}

// RenderMarkdown go-tui 分支：markdown 由消息区的内置 markdown 元素原生渲染
// （glow 风格主题、代码高亮、表格网格），这里原样返回源码即可。
// 旧的 glamour → 剥填充 → TranslateANSI → tview 标签管线整体删除。
func (t *Tui) RenderMarkdown(in string) (string, error) {
	return in, nil
}

func (t *Tui) ListenUserInput() chan string {
	return t.inputChan
}

// showMsgAndExit 打印一条消息并结束程序。waitForKey=true 时等用户按任意键再退出。
//
// 末尾的 select{} 是故意的（沿用 tview 版的收尾契约）：调用方（引擎 init 序列）
// 打完致命错误后并没有 return，一旦返回，引擎 goroutine 会带着未初始化的状态
// 继续跑。现在的链路是：globalKeys 看到 exiting 标志 → app.Stop() → Run() 返回
// → Close() 复原终端 → main() 返回 → 进程退出。
func (t *Tui) showMsgAndExit(msg string, waitForKey bool) {
	t.PrintToMsgView(msg, false)
	if waitForKey {
		t.exiting.Store(true)
		return
	}
	// 渲染是帧驱动的，没有 tview 的手动 Draw 可用：
	// 睡过 4 个 tick 保证退出消息先物化上屏，再停事件循环。
	time.Sleep(4 * tickInterval)
	t.app.Stop()
	select {}
}

func (t *Tui) ShowErrorInMsgViewAndExit(errmsg string) {
	t.showMsgAndExit(errmsg, true)
}

func (t *Tui) ShowSuccessInMsgViewAndExit(sussessmsg string) {
	t.showMsgAndExit(pretty.TSuccess(sussessmsg), true)
}

func (t *Tui) ShowMsgAndExitNoTrigger(msg string) {
	t.showMsgAndExit(msg, false)
}

// Run 阻塞运行事件循环。Close 幂等，Run 内部异常退出也能复原终端。
func (t *Tui) Run() {
	defer t.app.Close()
	if err := t.app.Run(); err != nil {
		panic("Error running application: " + err.Error())
	}
}
