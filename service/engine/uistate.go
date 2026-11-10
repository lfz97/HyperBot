package engine

// 引擎对上层 UI 暴露的可观察状态与方法（pull + JSON 契约）。
// 上下游零 import：跨界只有 stdlib 类型与 JSON 文本，schema 自描述（type/style/kind 字段）。
// 消息日志每条一行 JSON：{"type":"delta"|"message","msg":框架 model.Message 原样}，
// 或 {"type":"user"|"slash"|"warn"|"error"|"summary","text":"..."}；引擎只透传原始 message，渲染归 UI。

import (
	"encoding/json"
	"strings"
	"time"

	"HyperBot/service/engine/config"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// 记录 type 字段取值。
const (
	RecDelta   = "delta"   // 流式增量（msg=框架 message）
	RecMessage = "message" // 完整消息（msg=框架 message）
	RecUser    = "user"
	RecSlash   = "slash"
	RecWarn    = "warn"
	RecError   = "error"
	RecSummary = "summary"
)

// 终态 style 字段取值。
const (
	FatalPlain   = "plain"
	FatalError   = "error"
	FatalSuccess = "success"
	FatalExit    = "exit"
)

// 引擎启动状态（FirstStartupJSON 的 state 字段取值）。
const (
	BootBooting      = "booting" //bootstrap 检查/装配未完成，消费端继续等下一帧
	BootConfigNeeded = "config"  //首次启动：等待 TUI 配置页提交模型配置
	BootReady        = "ready"   //正常状态：配置就绪（首次提交完成或非首次启动）
)

// 通知 kind 字段取值。
const (
	NoticeNone            = "none"
	NoticeNewConversation = "new_conversation"
	NoticeCancelled       = "cancelled"
	NoticeSuccess         = "success"
	NoticeWarning         = "warning"
	NoticeClear           = "cleared"
)

// Fatal 进程终态（RunStateJSON 里 fatal 对象的形状）。
type Fatal struct {
	Text    string `json:"text"`
	Style   string `json:"style"`   // FatalXxx 常量
	WaitKey bool   `json:"waitKey"` // true=渲染后等用户按任意键再退出
}

// RunState 运行状态快照（幂等，last-write-wins）。
type RunState struct {
	Running bool   `json:"running"`
	Fatal   *Fatal `json:"fatal"` // 非 nil 表示进程应结束
}

// SkillItem 帮助页的一行（JSON 形状 {"cmd","desc"}）。
type SkillItem struct {
	Cmd  string `json:"cmd"`
	Desc string `json:"desc"`
}

// notice 通知栏槽位内容。SetAt 供消费端计算 TTL（旧契约 4s，语义不变）。
type notice struct {
	Kind  string    `json:"kind"`
	Text  string    `json:"text"`
	SetAt time.Time `json:"setAt"`
}

// ── 消息日志 ─────────────────────────────────────────────

// Version 返回全局单调版本号：任何记录追加都会 +1。
// 消费端每帧比对版本号，有变化才拉取增量——空闲时零成本。
func (e *Engine) Version() uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.version
}

// Records 返回全部消息日志记录（每条一行 JSON，按写入顺序）。
func (e *Engine) Records() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.records...)
}

// 清空记录。必须递增 version：TUI 按 version != seenVersion 决定是否重组视图，
// 只清 records 不 bump，聊天区不会刷新。
func (e *Engine) clearRecords() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.records = []string{}
	e.version++
}

// appendRecord 追加一条已序列化的 JSON 记录（引擎内部写入口）。
func (e *Engine) appendRecord(rawJSON string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.records = append(e.records, rawJSON)
	e.version++
}

// appendTyped 追加一条 {"type":typ,"text":...} 形状的记录（引擎内部文本事件用）。
func (e *Engine) appendTyped(typ, text string) {
	b, err := json.Marshal(struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	}{typ, text})
	if err != nil {
		return // 纯 string 字段不会失败，防御性兜底
	}
	e.appendRecord(string(b))
}

// AppendSummary 追加一条摘要提示（session.summarySink 消费方小接口的实现）。
func (e *Engine) AppendSummary(text string) { e.appendTyped(RecSummary, text) }

// AppendRecordJSON 追加一条已序列化的 JSON 记录（emitChoice 的透传序列化产物走这里）。
func (e *Engine) AppendRecordJSON(raw string) { e.appendRecord(raw) }

// wireRecord 消息记录的 JSON 形状：type 承载消息类型，msg 为框架 message 原样。
type wireRecord struct {
	Type string         `json:"type"`
	Msg  *model.Message `json:"msg,omitempty"`
}

func wireMsg(typ string, m model.Message) string {
	b, err := json.Marshal(wireRecord{Type: typ, Msg: &m})
	if err != nil {
		return "" // 纯 string/json 字段的 message 不会 marshal 失败，防御性兜底
	}
	return string(b)
}

// emitChoice 把框架 Response choice 透传序列化进消息日志，增量/完整靠 type 字段区分，
// 怎么渲染是 UI 的事。ShowReasoning=false 时剥掉 reasoning_content；空增量/空定稿不入日志。
func (e *Engine) emitChoice(choice model.Choice, isPartial bool) {
	if (*(*e).AgentRunner_p).Stream {
		d := choice.Delta
		if !(*(*e).Config_p).Model.ShowReasoning {
			d.ReasoningContent = ""
		}
		if d.Content != "" || d.ReasoningContent != "" || len(d.ToolCalls) != 0 || d.Role == "tool" {
			e.AppendRecordJSON(wireMsg("delta", d))
		}
	}
	if !isPartial {
		m := choice.Message
		if !(*(*e).Config_p).Model.ShowReasoning {
			m.ReasoningContent = ""
		}
		if strings.TrimSpace(m.Content) != "" || len(m.ToolCalls) != 0 || m.Role == "tool" {
			e.AppendRecordJSON(wireMsg("message", m))
		}
	}
}

// ── 运行状态 ─────────────────────────────────────────────

// RunStateJSON 返回运行状态快照 JSON（{"running":bool,"fatal":{...}|null}）。
func (e *Engine) RunStateJSON() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	b, err := json.Marshal(e.runState)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// setRunning 标记 agent 是否在运行（指示器/提示条/Esc 行为的依据）。
// 生产路径的置位/复位走 tryClaimRunning/endTurn（单临界区成对操作），
// 本方法保留给测试直接模拟忙碌态。
func (e *Engine) setRunning(running bool) {
	e.mu.Lock()
	e.runState.Running = running
	e.mu.Unlock()
}

// setFatal 设置进程终态（配套 parkWithFatal 的驻留由调用方完成）。
func (e *Engine) setFatal(style, text string, waitKey bool) {
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

// setNotice 写入通知栏槽位（瞬时通知；文案原样存储，配色由消费端按 kind 决定）。
func (e *Engine) setNotice(kind, text string) {
	e.mu.Lock()
	e.notice = notice{Kind: kind, Text: text, SetAt: time.Now()}
	e.mu.Unlock()
}

// NoticeJSON 返回通知栏槽位快照 JSON（{"kind","text","setAt":RFC3339}）。
func (e *Engine) NoticeJSON() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	b, err := json.Marshal(e.notice)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// ── 首次启动状态 ──────────────────────────────────────────

// FirstStartupJSON 返回首次启动状态快照 JSON：
// {"state":"booting"|"config"|"ready","fields":{"model":...}}。
// fields 仅在 state=config 时携带（模板 model 块的预填值）。
// 消费端（TUI）每帧拉取，state 离开 booting 即可停止拉取。
func (e *Engine) FirstStartupJSON() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	b, err := json.Marshal(struct {
		State  string            `json:"state"`
		Fields map[string]string `json:"fields,omitempty"`
	}{e.bootState, e.firstStartupFields})
	if err != nil {
		return `{"state":"` + e.bootState + `"}`
	}
	return string(b)
}

// setBootState 推进启动状态，并成对写入配置页预填值快照（nil = 清除）。
// bootState 与 firstStartupFields 是同一可观察状态的两个字段，FirstStartupJSON
// （TUI pull goroutine，逐帧）在同一个临界区里成对读取——写入也必须在同一
// 临界区成对完成，否则读取方会看到"状态已切换而快照未就绪/未清除"的中间态。
// 所有对这两个字段的写都走这里；在别处裸写即数据竞争。
func (e *Engine) setBootState(state string, fields map[string]string) {
	e.mu.Lock()
	e.firstStartupFields = fields
	e.bootState = state
	e.mu.Unlock()
}

// templateModelFields 快照模板 model 块为配置页预填值（纯函数）。
// 在进入 config 态前调用并经 setBootState 定格；键集与 engineCore.go 的
// modelFormKeys（提交侧白名单）成对，增删字段需两处同步。
// Config_p 此刻只在引擎 goroutine 上读写，无需加锁。
func templateModelFields(m config.Model) map[string]string {
	return map[string]string{
		"model":   m.Model,
		"baseurl": m.BaseURL,
		"apikey":  m.APIKey,
		"apitype": m.APIType,
	}
}

// bootStateSnapshot 读取启动状态（Run 的横幅 gate 用）。
func (e *Engine) bootStateSnapshot() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.bootState
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

// SkillItemsJSON 返回技能帮助项 JSON（[{"cmd","desc"}]；默认项 /new /exit 由消费端自持）。
func (e *Engine) SkillItemsJSON() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.skills == nil {
		return "[]" // nil 切片会 marshal 成 null，契约上空就是 []
	}
	b, err := json.Marshal(e.skills)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// setSkillHelpItems 整体替换技能帮助项（loadSkills 每次刷新都重建）。
func (e *Engine) setSkillItems(items []SkillItem) {
	e.mu.Lock()
	e.skills = append([]SkillItem(nil), items...)
	e.mu.Unlock()
}

// ── 消费端 → 引擎 ────────────────────────────────────────

// SubmitInput 提交一行用户输入：空闲则同步占坑并异步发起一轮 turn（直通 runner）；
// 忙碌（turn 进行中，含重试退避）返回 false，消费端（TUI）应保留输入框内容。
// 占坑必须同步发生在 go turn 之前：engine 侧标志位是唯一的提交门——runner 的
// 注册表要等 Run 深处 registerRun 才落账（实测注册前窗口 <3ms，见
// window_measure_test.go），靠查 runner 判忙存在双放行窗口。
// 斜杠命令不经过这里：语义与注册表在前端（service/tui/handler.go），后端只暴露能力接口。
func (e *Engine) SubmitInput(line string) bool {
	if strings.TrimSpace(line) == "" {
		//空输入：充值预算即返（前端已兜底，双保险；不占坑不发起 turn）
		(*e).errBudget.recharge()
		return true
	}
	if !e.tryClaimRunning() {
		return false
	}
	//任何用户输入都充值错误预算（挪自原 agentLoop；占坑成功后失败路径只剩 turn 内部）
	(*e).errBudget.recharge()
	(*e).appendTyped(RecUser, line)
	go e.turn(line)
	return true
}

// Interrupt 提交一次中断（前端 Esc 时调用）。两级生效：
//  1. runner.Cancel(curRequestID)——run 已注册（绝大多数时刻），框架内传播取消；
//  2. pendingCancel 粘滞位——Esc 落在 runner 注册表空窗（注册前窗口或重试退避
//     间隙，Cancel 查不到）时由 turn 在下一个 Run 边界消费。
// 非运行期返回 false 可安全忽略。返回值语义是"信号已接收"，不是"已停止"——
// 停止判定只认 TUI 帧拉取的 running 状态（Esc 丢失于空窗时指示器继续转，
// 用户再按一次即命中已注册的 run，无需显式重试逻辑）。
func (e *Engine) Interrupt() bool {
	e.mu.Lock()
	if !e.runState.Running {
		e.mu.Unlock()
		return false
	}
	e.pendingCancel = true
	id := e.curRequestID
	e.mu.Unlock()

	if id != "" && (*e).AgentRunner_p != nil && (*(*e).AgentRunner_p).Runner != nil {
		(*(*e).AgentRunner_p).Runner.Cancel(id) //注册表命中则立即生效；miss 交给 pendingCancel
	}
	return true
}

// ── turn 生命周期状态机（curRequestID / pendingCancel / running 的唯一入口）──

// tryClaimRunning 原子地"查忙+占坑"：check-then-set 收在同一临界区，
// SubmitInput 的提交门因此无竞态（调用方仅 TUI 单事件循环，这里仍收敛成
// 原子操作——防御未来出现第二调用方）。
func (e *Engine) tryClaimRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.runState.Running {
		return false
	}
	e.runState.Running = true
	return true
}

// endTurn 收尾 turn：pendingCancel 与 running 必须同一临界区复位。拆开的活窗口：
// 先放 running 再清 pending 之间，Interrupt 会重新置位一个永远无人消费的
// 取消意图，泄漏到下一轮导致下一条输入被立即取消。顺序反过来同理。
func (e *Engine) endTurn() {
	e.mu.Lock()
	e.pendingCancel = false
	e.runState.Running = false
	e.mu.Unlock()
}

// consumePendingCancel 读取并清除取消意图（turn 在每个 Run 边界调用）。
func (e *Engine) consumePendingCancel() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	pending := e.pendingCancel
	e.pendingCancel = false
	return pending
}

// setCurRequestID 记账当前 Run 的 requestID（turn 逐次尝试时更新）。
func (e *Engine) setCurRequestID(id string) {
	e.mu.Lock()
	e.curRequestID = id
	e.mu.Unlock()
}

// ── 会话恢复弹框 ──────────────────────────────────────────

// ResumeItem 会话恢复候选列表的一行（JSON 形状，消费端见 service/tui）。
// 数据经 ListResumeItems 同步拉取，引擎不持有弹框 UI 状态。
type ResumeItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`     // 会话内首条用户输入（截断）
	UpdatedAt string `json:"updatedAt"` // 已格式化 "01-02 15:04"
	Current   bool   `json:"current"`   // 是否当前会话
}

// isRunning 是否有对话在跑（turn 内/重试中）。命令类接口据此拒绝并发操作。
func (e *Engine) isRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.runState.Running
}
