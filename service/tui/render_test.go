package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// fakeEngine 测试用的引擎桩：实现 EngineView 全部方法。
type fakeEngine struct {
	version  uint64
	records  []string
	runState string
	todo     string
	notice   string
	startup  []string
	ok       bool
	helps    string

	rejectSubmit bool
	submitted    []string
	interrupts   int
}

func (f *fakeEngine) Version() uint64               { return f.version }
func (f *fakeEngine) Records() []string             { return f.records }
func (f *fakeEngine) RunStateJSON() string          { return f.runState }
func (f *fakeEngine) TodoText() string              { return f.todo }
func (f *fakeEngine) NoticeJSON() string            { return f.notice }
func (f *fakeEngine) StartupInfo() ([]string, bool) { return f.startup, f.ok }
func (f *fakeEngine) HelpItemsJSON() string         { return f.helps }
func (f *fakeEngine) SubmitInput(line string) bool {
	if f.rejectSubmit {
		return false
	}
	f.submitted = append(f.submitted, line)
	return true
}
func (f *fakeEngine) Interrupt() bool {
	f.interrupts++
	return true
}

// keyPress 构造一个按键事件。
func keyPress(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod}
}

// recJSON 构造一条 wire 记录。
func recJSON(t *testing.T, typ, text string, msg any) string {
	t.Helper()
	type rec struct {
		Type string          `json:"type"`
		Msg  json.RawMessage `json:"msg,omitempty"`
		Text string          `json:"text,omitempty"`
	}
	r := rec{Type: typ, Text: text}
	if msg != nil {
		b, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("marshal msg: %v", err)
		}
		r.Msg = b
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal rec: %v", err)
	}
	return string(b)
}

func TestRenderFatalStyles(t *testing.T) {
	cases := []struct{ style, in string }{
		{"success", "已创建默认配置"},
		{"exit", "对话已结束"},
		{"error", "加载配置文件错误"},
		{"plain", "原样输出"},
	}
	for _, c := range cases {
		got := renderFatal(c.in, c.style)
		want := c.in
		if c.style == "success" || c.style == "exit" {
			want = successText(c.in)
		} else if c.style == "error" {
			want = errText(c.in)
		}
		if got != want {
			t.Fatalf("style %q: got %q want %q", c.style, got, want)
		}
	}
}

func TestRenderNoticeKinds(t *testing.T) {
	cases := []struct {
		kind, text, want string
	}{
		{"new_conversation", "", noticeNewConversation()},
		{"cancelled", "", noticeCancelled()},
		{"success", "done", noticeSuccess("done")},
		{"warning", "warn", noticeWarning("warn")},
	}
	for _, c := range cases {
		if got := renderNotice(c.kind, c.text); got != c.want {
			t.Fatalf("kind %q: got %q want %q", c.kind, got, c.want)
		}
	}
}

// TestComposeViewReplay 锁定 composeView 的记录重放：用户回显、错误上色、
// 流式尾段、工具行缓冲匹配。
func TestComposeViewReplay(t *testing.T) {
	f := &fakeEngine{
		records: []string{
			recJSON(t, "user", "你好", nil),
			recJSON(t, "message", "", model.Message{Role: "assistant", Content: "定稿"}),
			recJSON(t, "error", "boom", nil),
		},
		runState: `{"running":false,"fatal":null}`,
	}
	// delta 记录里的 msg 承载流式增量（ComposeView 只看 Role/Content/ToolCalls 字段）。
	f.records = append(f.records, recJSON(t, "delta", "", model.Message{Role: "assistant", Content: "流式"}))
	f.version = uint64(len(f.records))

	tui := NewTui(f)
	tui.widthAtomic.Store(80)
	st := newPullState()
	got := tui.composeView(st)

	if !strings.Contains(got, "▶ 你好") {
		t.Fatalf("用户回显缺失: %q", got)
	}
	if !strings.Contains(got, "boom") {
		t.Fatalf("错误文本缺失: %q", got)
	}
	// 流式尾段 live：delta 的 Content 必须出现在渲染结果里
	if !strings.Contains(got, "流式") {
		t.Fatalf("流式尾段缺失: %q", got)
	}
}

// TestComposeToolLine 工具调用缓冲 → 结果匹配 → 单行工具行。
func TestComposeToolLine(t *testing.T) {
	f := &fakeEngine{
		records: []string{
			recJSON(t, "message", "", model.Message{
				Role: "assistant",
				ToolCalls: []model.ToolCall{{
					ID: "t1",
					Function: model.FunctionDefinitionParam{
						Name:      "ReadFile",
						Arguments: []byte(`{"path":"a.txt"}`),
					},
				}},
			}),
			recJSON(t, "message", "", model.Message{
				Role:    "tool",
				ToolID:  "t1",
				Content: "hello",
			}),
		},
		runState: `{"running":false,"fatal":null}`,
	}
	tui := NewTui(f)
	tui.widthAtomic.Store(80)
	st := newPullState()
	got := tui.composeView(st)

	if !strings.Contains(got, "ReadFile") {
		t.Fatalf("工具行缺失: %q", got)
	}
	if !strings.Contains(got, "hello") {
		t.Fatalf("工具结果缺失: %q", got)
	}
}

// TestPullOnceNoticeTTL 通知槽位：TTL 内显示通知、到期回落 hint。
// 帧恒投递（去重交给框架的 viewEquals），这里只断言帧内容的取舍。
func TestPullOnceNoticeTTL(t *testing.T) {
	tui := NewTui(&fakeEngine{runState: `{"running":false,"fatal":null}`})
	tui.widthAtomic.Store(80)

	// 无通知：回落 idle hint
	frame, ok := tui.pullOnce()
	if !ok || frame.notice != composeHint(false) {
		t.Fatalf("空闲通知不符: %q ok=%v", frame.notice, ok)
	}

	// 4.5 秒前设置的通知：已过期 → 仍然回落 hint
	old := time.Now().Add(-4500 * time.Millisecond).Format(time.RFC3339)
	tui.engine = &fakeEngine{
		runState: `{"running":false,"fatal":null}`,
		notice:   `{"kind":"success","text":"done","setAt":"` + old + `"}`,
	}
	frame, ok = tui.pullOnce()
	if !ok || frame.notice != composeHint(false) {
		t.Fatalf("过期通知不符: %q ok=%v", frame.notice, ok)
	}

	// 刚设置的通知：正常显示
	fresh := time.Now().Format(time.RFC3339)
	tui.engine = &fakeEngine{
		runState: `{"running":false,"fatal":null}`,
		notice:   `{"kind":"success","text":"done","setAt":"` + fresh + `"}`,
	}
	frame, ok = tui.pullOnce()
	if !ok || frame.notice != noticeSuccess("done") {
		t.Fatalf("新鲜通知不符: %q ok=%v", frame.notice, ok)
	}
}

// TestPullDeliversViewChange 流式场景的端到端断言：仅 view 变化（running/
// notice/todo 恒定）的轮次，重组结果必须反映到帧里——历史上帧去重逻辑
// 写反时这类帧被静默丢弃（回归防线；去重现已整体移除、交给框架）。
func TestPullDeliversViewChange(t *testing.T) {
	f := &fakeEngine{
		records:  []string{recJSON(t, "user", "第一条", nil)},
		version:  1,
		runState: `{"running":true,"fatal":null}`,
	}
	tui := NewTui(f)
	tui.widthAtomic.Store(80)

	frame1, ok := tui.pullOnce()
	if !ok || !strings.Contains(frame1.view, "第一条") {
		t.Fatalf("第一帧不符: ok=%v view=%q", ok, frame1.view)
	}

	// 引擎追加新记录，其余状态全部不变（模拟流式 delta）
	f.records = append(f.records, recJSON(t, "delta", "", model.Message{Role: "assistant", Content: "流式增量"}))
	f.version = 2
	tui.pull.lastCompose = time.Now().Add(-time.Second) // 绕过 100ms 节流

	frame2, ok := tui.pullOnce()
	if !ok {
		t.Fatalf("仅 view 变化的帧必须投递（流式更新的回归测试）")
	}
	if frame2.view == frame1.view {
		t.Fatalf("第二帧视图未更新: %q", frame2.view)
	}
	if !strings.Contains(frame2.view, "流式增量") {
		t.Fatalf("第二帧缺少新内容: %q", frame2.view)
	}
}

// TestKeyHandling 按键语义：enter 提交、引擎忙保留输入、esc 中断（仅运行态）。
func TestKeyHandling(t *testing.T) {
	f := &fakeEngine{runState: `{"running":false,"fatal":null}`}
	tui := NewTui(f)
	tui.Init()

	// 录入文本并提交
	tui.ta.InsertString("/new")
	_, _ = tui.Update(keyPress(tea.KeyEnter, 0))
	if len(f.submitted) != 1 || f.submitted[0] != "/new" {
		t.Fatalf("提交不符: %v", f.submitted)
	}
	if tui.ta.Value() != "" {
		t.Fatalf("提交后输入框未清空: %q", tui.ta.Value())
	}

	// 引擎忙：SubmitInput 拒绝时保留输入
	f.rejectSubmit = true
	tui.ta.InsertString("保留我")
	_, _ = tui.Update(keyPress(tea.KeyEnter, 0))
	if tui.ta.Value() != "保留我" {
		t.Fatalf("引擎忙时输入被丢弃: %q", tui.ta.Value())
	}
	f.rejectSubmit = false

	// esc 非运行态：不中断
	_, _ = tui.Update(keyPress(tea.KeyEscape, 0))
	if f.interrupts != 0 {
		t.Fatalf("非运行态 esc 不应中断: %d", f.interrupts)
	}

	// 运行态 esc：中断
	tui.running = true
	_, _ = tui.Update(keyPress(tea.KeyEscape, 0))
	if f.interrupts != 1 {
		t.Fatalf("运行态 esc 应中断: %d", f.interrupts)
	}

	// ctrl+k：打开浮层；再按关闭
	_, _ = tui.Update(keyPress('k', tea.ModCtrl))
	if !tui.helps.isVisible() {
		t.Fatalf("ctrl+k 应打开帮助浮层")
	}
	_, _ = tui.Update(keyPress('k', tea.ModCtrl))
	if tui.helps.isVisible() {
		t.Fatalf("ctrl+k 应关闭帮助浮层")
	}

	// 终态等键：任意键退出（waitingKey 置位后 esc 直接 Quit）
	tui.waitingKey = true
	model, _ := tui.Update(keyPress(tea.KeyEscape, 0))
	if _, ok := model.(*TUI); !ok {
		t.Fatalf("终态任意键应触发退出模型")
	}
}

// TestDrawAndOverlay 整屏拼版不 panic、帮助浮层叠加生效。
func TestDrawAndOverlay(t *testing.T) {
	f := &fakeEngine{runState: `{"running":false,"fatal":null}`}
	tui := NewTui(f)
	tui.Init()
	tui.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tui.applyViewText("hello view")

	out := tui.draw()
	if !strings.Contains(out, "hello view") {
		t.Fatalf("视图内容缺失")
	}

	// 打开帮助浮层
	tui.helps.refresh(f)
	tui.helps.toggleVisibility()
	out = tui.draw()
	if !strings.Contains(out, "Key Bindings") {
		t.Fatalf("帮助浮层缺失")
	}
}
