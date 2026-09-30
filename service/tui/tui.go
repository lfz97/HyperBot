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
	// spinnerInterval spinner 推进周期（约 0.9s 一圈）。同时兼任通知 TTL 的检查时钟。
	spinnerInterval = 90 * time.Millisecond
	// noticeTTL 临时通知停留时长，到期回落兜底提示。
	noticeTTL = 4 * time.Second
	// exitFlushDelay 无按键退出前，保证退出消息至少渲染过几帧。
	exitFlushDelay = 200 * time.Millisecond
)

var spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// NoticeBar 常驻兜底提示（tview 标签形式，由 tagbridge 解析上色）。
const (
	hintIdle    = `[gray::d]ctrl+k for help[-:-:-]`
	hintRunning = `[gray::d]ctrl+c/esc to interrupt · ctrl+k for help[-:-:-]`
)

// segKind 消息段类型。连续的 PrintToMsgView 调用会合并进最近的 segText 段
// （对应 tview 版"同一个 TextView 缓冲"的语义），ReplaceTail 把尾段转成 markdown。
type segKind int

const (
	segText     segKind = iota // 受信 tview 标签文本（流式原文/工具块/用户回显/退出消息），tagbridge 解析
	segMarkdown                // 渲染完成的 markdown 源码（go-tui 内置 Markdown 组件渲染）
)

type msgSeg struct {
	id   int
	kind segKind
	text string
}

type helpItem struct{ cmd, desc string }

type Tui struct {
	app       *gotui.App
	ui        *agentUI
	inputChan chan string

	// ── staging：引擎 goroutine 写（走 mu，永不阻塞），主循环渲染时读取。
	// 引擎侧改动后调 app.MarkDirty()（atomic 标志，跨 goroutine 安全），
	// 主循环下一帧在 Render() 里以 staging 为唯一事实来源重建整棵元素树。
	// 这是 go-tui 的标准反应式模型：Render 必须每帧构建新树（状态驱动的
	// 重渲染），有状态 widget（TextArea/Markdown）通过 app.Mount 跨帧复用。
	mu          sync.Mutex
	segs        []*msgSeg
	buf         string // 平面文本缓冲，复刻 TextView.GetText 的 LastIndex 语义
	todoText    string
	noticeMsg   string
	noticeUntil time.Time
	helpItems   []helpItem
	bannerLines []string
	bannerSet   bool
	escFn       func()

	segID int // 消息段稳定 id，作为 Markdown 组件的 mount key

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
		// 30fps：流式输出期间每帧重建整棵树（含全量文本重排），60fps 没有必要
		gotui.WithFrameRate(30),
	)
	if err != nil {
		panic("tui: 创建 go-tui App 失败: " + err.Error())
	}
	t.app = app
	t.ResetHelpItems()
	return t
}

// markDirty 引擎 goroutine 通知主循环重绘。dirty 是 atomic 标志，
// 主循环每帧检查并消费，多次调用自动合并成一帧。
func (t *Tui) markDirty() {
	if t.app != nil {
		t.app.MarkDirty()
	}
}

// submitInput textarea 的提交回调（主循环执行）。
// 与 tview 版语义一致：引擎未在监听（自动 turn 期间）时保留文本，
// 只有投递成功才清空输入框。
func (t *Tui) submitInput(text string) {
	select {
	case t.inputChan <- text:
		t.ui.ta.Clear()
	default:
	}
}

// ── snapshot 访问器（模板每帧调用，带锁读 staging）──

// segsSnapshot 返回消息段的值拷贝：引擎侧会对尾段做 in-place 追加
// （流式 delta 合并），直接共享指针会和主循环的读构成数据竞争。
func (t *Tui) segsSnapshot() []*msgSeg {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]*msgSeg, len(t.segs))
	for i, s := range t.segs {
		cp := *s
		out[i] = &cp
	}
	return out
}

func (t *Tui) bannerSetSnapshot() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.bannerSet
}

func (t *Tui) bannerLinesSnapshot() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.bannerLines
}

func (t *Tui) todoTextSnapshot() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.todoText
}

func (t *Tui) todoLinesSnapshot() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.Split(t.todoText, "\n")
}

func (t *Tui) helpItemsSnapshot() []helpItem {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]helpItem(nil), t.helpItems...)
}

// ── TuiService 接口实现 ─────────────────────────────
// 线程契约与 tview 版一致：引擎侧任意 goroutine 可调、不阻塞；
// UI 变更统一在主循环的 Render 里落地。

func (t *Tui) PrintToMsgView(content string, clear bool) {
	t.mu.Lock()
	if clear {
		t.resetMsgsLocked()
	}
	if content != "" {
		t.buf += content
		// 连续文本合并进同一个 segText 段：流式 delta 不膨胀段数量，
		// ReplaceTail 的"尾段后缀"判断也依赖这一合并语义
		if n := len(t.segs); n > 0 && t.segs[n-1].kind == segText {
			t.segs[n-1].text += content
		} else {
			t.segID++
			t.segs = append(t.segs, &msgSeg{id: t.segID, kind: segText, text: content})
		}
	}
	t.mu.Unlock()
	t.markDirty()
}

// resetMsgsLocked 清空平面缓冲与段列表。元素树由 Render 每帧重建，
// 这里只动数据。
func (t *Tui) resetMsgsLocked() {
	t.buf = ""
	t.segs = nil
}

// ReplaceTailInMsgView 把消息区末尾的文本段替换成 markdown 段，返回是否替换成功。
// 平面缓冲上的判断与 tview 版逐字节等价（LastIndex + 尾部对齐）；
// 元素层要求尾部正好是最后一个 segText 段——连续 delta 已合并进单段，
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
	if n == 0 || t.segs[n-1].kind != segText || !strings.HasSuffix(t.segs[n-1].text, raw) {
		return false
	}
	t.buf = t.buf[:i] + replacement
	last := t.segs[n-1]
	last.text = strings.TrimSuffix(last.text, raw)
	if last.text == "" {
		// 尾段被完整替换：原位变成 markdown 段，mount key 不变
		last.kind = segMarkdown
		last.text = replacement
	} else {
		t.segID++
		t.segs = append(t.segs, &msgSeg{id: t.segID, kind: segMarkdown, text: replacement})
	}
	return true
}

// SetTodoText 更新清单栏文本，空串整栏隐藏。
// 收到的是框架/LLM 的多行纯文本，按行渲染。
func (t *Tui) SetTodoText(text string) {
	t.mu.Lock()
	t.todoText = text
	t.mu.Unlock()
	t.markDirty()
}

// ShowNotice 显示一条临时通知，noticeTTL 后回落兜底提示。
// msg 是 pretty.TBarXxx 生成的受信 tview 标签 markup，tagbridge 负责解析上色。
func (t *Tui) ShowNotice(msg string) {
	t.mu.Lock()
	t.noticeMsg = msg
	t.noticeUntil = time.Now().Add(noticeTTL)
	t.mu.Unlock()
	t.markDirty()
}

// SetAgentRunning 标记 agent 运行态（指示器与提示行随渲染帧切换）。
func (t *Tui) SetAgentRunning(running bool) {
	t.running.Store(running)
	t.markDirty()
}

// ShowStartupBanner 在消息区顶部物化启动横幅（随对话滚动）。
func (t *Tui) ShowStartupBanner(infoLines []string) {
	t.mu.Lock()
	t.bannerLines = infoLines
	t.bannerSet = true
	t.mu.Unlock()
	t.markDirty()
}

func (t *Tui) AddHelpItems(items []map[string]string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, i := range items {
		for k, v := range i {
			t.helpItems = append(t.helpItems, helpItem{cmd: k, desc: v})
		}
	}
}

func (t *Tui) ResetHelpItems() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.helpItems = []helpItem{
		{"/new", "开始新对话"},
		{"/exit", "退出程序"},
	}
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

// RenderMarkdown markdown 由消息区的内置 Markdown 组件原生渲染
// （代码高亮、表格、列表），这里原样返回源码即可。
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
// 继续跑。现在的链路是：exiting 置位 → 主循环下一帧剥掉输入框并启用"任意键退出"
// → app.Stop() → Run() 返回 → Close() 复原终端 → main() 返回 → 进程退出。
//
// Open() 幂等：错误可能发生在 Run() 启动之前（启动期配置错误），
// 这里先确保终端初始化并渲染出第一帧。
func (t *Tui) showMsgAndExit(msg string, waitForKey bool) {
	t.PrintToMsgView(msg, false)
	t.exiting.Store(true)
	t.markDirty()
	_ = t.app.Open()
	if !waitForKey {
		time.Sleep(exitFlushDelay)
		t.app.Stop()
	}
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
