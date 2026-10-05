// Package runlog 引擎侧的唯一状态源：消息日志、运行状态、todo 清单、通知槽位与静态数据，
// 外加消费端→引擎的两个入口（提交输入、取消运行）。
//
// 它是 pull 架构的枢纽：引擎各 goroutine 只在锁内写状态，从不回调任何 UI；
// TUI（或未来的 Web 前端）按版本号拉取增量、按帧拉取快照并自行渲染。
// 本包不 import 任何业务包，被 engine 与 tui 两端同时依赖，自身零依赖——
// 因此 engine 对表现层的依赖在此归零。
package runlog

import (
	"sync"
	"time"
)

// MsgKind 消息记录的类型。消费端（TUI）按 Kind 决定配色与版式，
// 引擎只产出语义原文（Text），不再关心颜色。
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

// Fatal 进程终态：引擎设置后即认为自己的使命结束（引擎侧 goroutine 应驻留不再推进），
// 消费端观察到后负责收尾——渲染消息、按 WaitKey 等待按键、停止事件循环让 main 返回。
type Fatal struct {
	Text    string // 语义原文，样式由 Style 决定
	Style   FatalStyle
	WaitKey bool // true=渲染后等用户按任意键再退出
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

// Store 引擎的全部可观察状态 + 消费端→引擎的入口。
// boot 先创建 Store 交给 TUI，引擎随后绑定（GetEngineService）——
// 两端不互相持有引用，全靠这个中立方，避免了启动顺序上的相互等待。
type Store struct {
	mu      sync.Mutex
	records []MsgRecord
	seq     int64
	version uint64

	runState RunState

	todoText string

	notice    notice
	startup   []string
	startupOK bool
	skills    []HelpItem

	inputCh chan string

	cancelMu sync.Mutex
	cancelFn func()
}

// NewStore 创建状态源。inputCh 刻意无缓冲：引擎在输入循环里接收时 SubmitInput
// 才会成功，引擎忙时提交返回 false——与旧版 TUI 持有 unbuffered chan +
// select/default 的行为完全一致（不投递、不丢失，用户文本留在输入框）。
func NewStore() *Store {
	return &Store{inputCh: make(chan string)}
}

// InputChan 返回引擎消费用户输入的通道。
func (s *Store) InputChan() <-chan string { return s.inputCh }

// SubmitInput 提交一行用户输入。引擎忙（无人接收）时返回 false。
func (s *Store) SubmitInput(line string) bool {
	select {
	case s.inputCh <- line:
		return true
	default:
		return false
	}
}

// SetActiveCancel 注册/注销当前运行的取消函数（对应旧 SetAppFuncTriggerWithEsc/Clear，
// 方向反转：不再由引擎往 TUI 注册回调，而是消费端在 Esc 时调用 Cancel）。
func (s *Store) SetActiveCancel(f func()) {
	s.cancelMu.Lock()
	s.cancelFn = f
	s.cancelMu.Unlock()
}

// Cancel 触发当前运行的取消；无运行中的取消函数时是 no-op（等价于旧版
// "非运行期不注册 Esc 捕获"的行为）。
func (s *Store) Cancel() {
	s.cancelMu.Lock()
	f := s.cancelFn
	s.cancelMu.Unlock()
	if f != nil {
		f()
	}
}

// ── 消息日志 ─────────────────────────────────────────────

// Version 返回全局单调版本号：任何记录追加都会 +1。
// 消费端每帧比对版本号，有变化才拉取增量——空闲时零成本。
func (s *Store) Version() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.version
}

// Records 返回 Seq 大于 afterSeq 的全部记录（按 Seq 升序）。
func (s *Store) Records(afterSeq int64) []MsgRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]MsgRecord, 0, len(s.records))
	for i := range s.records {
		if s.records[i].Seq > afterSeq {
			out = append(out, s.records[i])
		}
	}
	return out
}

// Append 追加一条文本记录。
func (s *Store) Append(kind MsgKind, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	s.records = append(s.records, MsgRecord{Seq: s.seq, Kind: kind, Text: text})
	s.version++
}

// AppendTool 追加一条工具调用记录。
func (s *Store) AppendTool(name, in, out string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	s.records = append(s.records, MsgRecord{
		Seq:  s.seq,
		Kind: KindTool,
		Tool: &ToolLine{Name: name, In: in, Out: out},
	})
	s.version++
}

// AppendSummary 追加一条摘要提示（session 包消费方小接口 summarySink 的实现）。
func (s *Store) AppendSummary(text string) { s.Append(KindSummary, text) }

// ── 运行状态 ─────────────────────────────────────────────

// RunState 返回运行状态快照。
func (s *Store) RunState() RunState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runState
}

// SetRunning 标记 agent 是否在运行（指示器/提示条/Esc 行为的依据）。
func (s *Store) SetRunning(running bool) {
	s.mu.Lock()
	s.runState.Running = running
	s.mu.Unlock()
}

// SetFatal 设置进程终态。引擎侧设置后应永久驻留（select{}），
// 把"进程退出"的执行权交给消费端——引擎不再知道终端的存在。
func (s *Store) SetFatal(style FatalStyle, text string, waitKey bool) {
	s.mu.Lock()
	s.runState.Fatal = &Fatal{Text: text, Style: style, WaitKey: waitKey}
	s.mu.Unlock()
}

// ── todo 清单 ────────────────────────────────────────────

// TodoText 返回当前清单文本（多行纯文本，空串=无清单）。
func (s *Store) TodoText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.todoText
}

// SetTodoText 更新清单文本（满足 agent.Display 消费方小接口）。
// ⚠️ 空串也必须写入：清单全部完成后框架会返回 ""，消费端靠空串把清单栏塌回 0 行。
func (s *Store) SetTodoText(text string) {
	s.mu.Lock()
	s.todoText = text
	s.mu.Unlock()
}

// ── 通知栏槽位 ───────────────────────────────────────────

// SetNotice 写入通知栏槽位（瞬时通知；文案原样存储，配色由消费端按 Kind 决定）。
func (s *Store) SetNotice(kind NoticeKind, text string) {
	s.mu.Lock()
	s.notice = notice{Kind: kind, Text: text, SetAt: time.Now()}
	s.mu.Unlock()
}

// Notice 返回通知栏槽位快照（Kind/Text/SetAt）。
func (s *Store) Notice() (NoticeKind, string, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.notice.Kind, s.notice.Text, s.notice.SetAt
}

// ── 静态数据 ─────────────────────────────────────────────

// StartupInfo 返回启动横幅信息行；引擎尚未就绪时第二返回值为 false，
// 消费端应等下一帧再拉（横幅只在数据齐了之后出现一次）。
func (s *Store) StartupInfo() ([]string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startup, s.startupOK
}

// SetStartupInfo 就绪启动横幅信息行（引擎 init 完成后的第一轮调用）。
func (s *Store) SetStartupInfo(lines []string) {
	s.mu.Lock()
	s.startup = lines
	s.startupOK = true
	s.mu.Unlock()
}

// SkillHelpItems 返回技能帮助项快照（默认项 /new /exit 由消费端自己持有）。
func (s *Store) SkillHelpItems() []HelpItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]HelpItem(nil), s.skills...)
}

// SetSkillHelpItems 整体替换技能帮助项（loadSkills 每次刷新都重建，语义同旧
// ResetHelpItems+AddHelpItems，只是不再经过 TUI 的两个推送方法）。
func (s *Store) SetSkillHelpItems(items []HelpItem) {
	s.mu.Lock()
	s.skills = append([]HelpItem(nil), items...)
	s.mu.Unlock()
}
