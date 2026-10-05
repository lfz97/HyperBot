package engine

// 本文件是引擎对上层 UI 暴露的可观察状态与方法（pull 契约）。
// 不设中间层：状态就是 Engine 的字段（定义见 init.go 的 Engine struct），
// 一把 mu 串行化引擎各 goroutine 的写入与 UI goroutine 的读取；
// UI 按帧拉取快照/增量（Version/Records/RunState/...），输入与取消经
// SubmitInput/Cancel 进入引擎。渲染、配色、按键解释全部是 UI 侧的事。

import (
	"time"
)

// MsgKind 消息记录的类型。消费端（TUI）按 Kind 决定配色与版式，
// 引擎只产出语义原文（Text），不关心颜色。
type MsgKind uint8

const (
	KindNewline      MsgKind = iota // 裸换行（思考块前后的版式空行）
	KindUser                        // 用户输入回显
	KindSlashEcho                   // /exit /new 斜杠命令回显
	KindReasoning                   // 思考内容（流式 delta 或非流式整块）
	KindContentDelta                // 正文流式增量（原文）
	KindContentFinal                // 正文定稿（完整原文；消费端渲染 markdown 并做尾部替换）
	KindTool                        // 工具调用行（紧凑格式）
	KindWarn                        // 重试提示等警告行
	KindErrorLine                   // 错误提示行
	KindSummary                     // 会话摘要提示
)

// ToolLine 一条工具调用记录的载荷（入参/出参由引擎按 param mapper 摘要）。
type ToolLine struct {
	Name string
	In   string
	Out  string
}

// MsgRecord 消息日志中的一条记录。
type MsgRecord struct {
	Seq    int64
	Kind   MsgKind
	Text   string    // 主载荷（语义原文）
	Tool   *ToolLine // Kind==KindTool 时非空
	Branch string    // 预留：产出分支标识（子 agent / cronagent），主会话恒为空
}

// NoticeKind 通知栏槽位的类型（瞬时通知，与消息区的持久行不同）。
type NoticeKind uint8

const (
	NoticeNone            NoticeKind = iota // 无通知（零值）
	NoticeNewConversation                   // 新对话开始（文案由消费端固定）
	NoticeCancelled                         // 会话被取消（同上）
	NoticeSuccess                           // 带文本的成功提示
	NoticeWarning                           // 带文本的警告提示
)

// FatalStyle 终态消息的样式，消费端据此上色。
type FatalStyle uint8

const (
	FatalPlain   FatalStyle = iota // 原样输出
	FatalError                     // 错误样式
	FatalSuccess                   // 成功样式
	FatalExit                      // 退出告别样式
)

// Fatal 进程终态：引擎设置后即认为自己的使命结束（引擎侧 goroutine 经 parkWithFatal
// 永久驻留），消费端观察到后负责收尾——渲染消息、按 WaitKey 等待按键、停止事件循环
// 让 main 返回。
type Fatal struct {
	Text    string     // 语义原文，样式由 Style 决定
	Style   FatalStyle
	WaitKey bool       // true=渲染后等用户按任意键再退出
}

// RunState 运行状态快照（幂等，last-write-wins）。
type RunState struct {
	Running bool
	Fatal   *Fatal // 非 nil 表示进程应结束
}

// HelpItem 帮助页的一行（斜杠命令 + 描述）。
type HelpItem struct{ Cmd, Desc string }

// notice 通知栏槽位内容。SetAt 供消费端计算 TTL（旧契约 4s，语义不变）。
type notice struct {
	Kind  NoticeKind
	Text  string
	SetAt time.Time
}

// ── 消息日志 ─────────────────────────────────────────────

// Version 返回全局单调版本号：任何记录追加都会 +1。
// 消费端每帧比对版本号，有变化才拉取增量——空闲时零成本。
func (e *Engine) Version() uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.version
}

// Records 返回 Seq 大于 afterSeq 的全部记录（按 Seq 升序）。
func (e *Engine) Records(afterSeq int64) []MsgRecord {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]MsgRecord, 0, len(e.records))
	for i := range e.records {
		if e.records[i].Seq > afterSeq {
			out = append(out, e.records[i])
		}
	}
	return out
}

// appendRecord 追加一条文本记录（引擎内部写入口；跨包子包经下方意图方法）。
func (e *Engine) appendRecord(kind MsgKind, text string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seq++
	e.records = append(e.records, MsgRecord{Seq: e.seq, Kind: kind, Text: text})
	e.version++
}

// AppendTool 追加一条工具调用记录（messagerender.logSink 的实现）。
func (e *Engine) AppendTool(name, in, out string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seq++
	e.records = append(e.records, MsgRecord{
		Seq:  e.seq,
		Kind: KindTool,
		Tool: &ToolLine{Name: name, In: in, Out: out},
	})
	e.version++
}

// AppendSummary 追加一条摘要提示（session.summarySink 消费方小接口的实现）。
func (e *Engine) AppendSummary(text string) { e.appendRecord(KindSummary, text) }

// ── messagerender.logSink 的意图化实现：渲染包只说发生了什么 ──

// AppendNewline 追加一条版式空行。
func (e *Engine) AppendNewline() { e.appendRecord(KindNewline, "\n") }

// AppendReasoning 追加思考内容。
func (e *Engine) AppendReasoning(text string) { e.appendRecord(KindReasoning, text) }

// AppendContentDelta 追加正文流式增量。
func (e *Engine) AppendContentDelta(text string) { e.appendRecord(KindContentDelta, text) }

// AppendContentFinal 追加正文定稿（完整原文）。
func (e *Engine) AppendContentFinal(text string) { e.appendRecord(KindContentFinal, text) }

// ── 运行状态 ─────────────────────────────────────────────

// RunState 返回运行状态快照。
func (e *Engine) RunState() RunState {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.runState
}

// setRunning 标记 agent 是否在运行（指示器/提示条/Esc 行为的依据）。
func (e *Engine) setRunning(running bool) {
	e.mu.Lock()
	e.runState.Running = running
	e.mu.Unlock()
}

// setFatal 设置进程终态（配套 parkWithFatal 的驻留由调用方完成）。
func (e *Engine) setFatal(style FatalStyle, text string, waitKey bool) {
	e.mu.Lock()
	e.runState.Fatal = &Fatal{Text: text, Style: style, WaitKey: waitKey}
	e.mu.Unlock()
}

// ── todo 清单 ────────────────────────────────────────────

// TodoText 返回当前清单文本（多行纯文本，空串=无清单）。
func (e *Engine) TodoText() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.todoText
}

// SetTodoText 更新清单文本（满足 agent.Display 消费方小接口）。
// ⚠️ 空串也必须写入：清单全部完成后框架会返回 ""，消费端靠空串把清单栏塌回 0 行。
func (e *Engine) SetTodoText(text string) {
	e.mu.Lock()
	e.todoText = text
	e.mu.Unlock()
}

// ── 通知栏槽位 ───────────────────────────────────────────

// setNotice 写入通知栏槽位（瞬时通知；文案原样存储，配色由消费端按 Kind 决定）。
func (e *Engine) setNotice(kind NoticeKind, text string) {
	e.mu.Lock()
	e.notice = notice{Kind: kind, Text: text, SetAt: time.Now()}
	e.mu.Unlock()
}

// Notice 返回通知栏槽位快照（Kind/Text/SetAt）。
func (e *Engine) Notice() (NoticeKind, string, time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.notice.Kind, e.notice.Text, e.notice.SetAt
}

// ── 静态数据 ─────────────────────────────────────────────

// StartupInfo 返回启动横幅信息行；引擎尚未就绪时第二返回值为 false，
// 消费端应等下一帧再拉（横幅只在数据齐了之后出现一次）。
func (e *Engine) StartupInfo() ([]string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.startup, e.startupOK
}

// setStartupInfo 就绪启动横幅信息行（引擎 init 完成后的第一轮调用）。
func (e *Engine) setStartupInfo(lines []string) {
	e.mu.Lock()
	e.startup = lines
	e.startupOK = true
	e.mu.Unlock()
}

// SkillHelpItems 返回技能帮助项快照（默认项 /new /exit 由消费端自己持有）。
func (e *Engine) SkillHelpItems() []HelpItem {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]HelpItem(nil), e.skills...)
}

// setSkillHelpItems 整体替换技能帮助项（loadSkills 每次刷新都重建）。
func (e *Engine) setSkillHelpItems(items []HelpItem) {
	e.mu.Lock()
	e.skills = append([]HelpItem(nil), items...)
	e.mu.Unlock()
}

// ── 消费端 → 引擎 ────────────────────────────────────────

// SubmitInput 提交一行用户输入。引擎忙（无人接收）时返回 false，
// 消费端（TUI）应保留输入框内容——与旧版 unbuffered chan + default 行为一致。
func (e *Engine) SubmitInput(line string) bool {
	select {
	case e.inputCh <- line:
		return true
	default:
		return false
	}
}

// setActiveCancel 注册/注销当前运行的取消函数（对应旧 SetAppFuncTriggerWithEsc/Clear，
// 方向反转：不再由引擎往 TUI 注册回调，而是消费端在 Esc 时调用 Cancel）。
func (e *Engine) setActiveCancel(f func()) {
	e.cancelMu.Lock()
	e.cancelFn = f
	e.cancelMu.Unlock()
}

// Cancel 触发当前运行的取消；无运行中的取消函数时是 no-op。
func (e *Engine) Cancel() {
	e.cancelMu.Lock()
	f := e.cancelFn
	e.cancelMu.Unlock()
	if f != nil {
		f()
	}
}
