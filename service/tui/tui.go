// Package tui 是 HyperBot 的终端界面层，基于 charm.land/bubbletea/v2（Elm 架构）实现。
//
// 与引擎的协作模型（单向数据流，职责分层）：
//
//	引擎 goroutine                          bubbletea 主循环
//	──────────────────────────              ─────────────────────────────────
//	TuiService 方法（任意线程）── Send ──▶ tea.Msg ──▶ model.Update ──▶ model.View
//	Tui.ListenUserInput() ◀── chan ────  回车把输入行投递给引擎
//
// 本包方法都只做一次 Program.Send（线程安全、非阻塞、启动前调用会缓存），
// UI 状态全部收敛在 model 上、只由主循环串行修改 —— 无锁、无共享可变状态。
//
// 版本定位：最小化跑通版。样式从简 —— Span 的颜色/加粗属性被忽略（纯文本渲染）、
// markdown 按源码显示、无帮助面板和滚动键位，目标是打通链路，不是对齐旧 UI。
package tui

import (
	"HyperBot/service/engine/requirements"
	"HyperBot/utils/pretty"

	"charm.land/bubbletea/v2"
)

// 编译期断言：引擎契约（TuiService）变更时在这里报错，而不是运行期缺方法。
var _ requirements.TuiService = (*Tui)(nil)

// Tui 是引擎侧的 TUI 服务入口。boot.go 的用法：引擎跑在后台 goroutine，
// 主 goroutine 调 Run()。
type Tui struct {
	program   *tea.Program  // 启动前 Send 的消息由 bubbletea 缓存，主循环起来后处理
	inputChan chan string   // 用户输入行 → 引擎（ListenUserInput 的返回值）
	done      chan struct{} // Run 返回时 close；Exit* 方法用它等待 UI 真正退出
}

// GetTuiService 构造 TUI 服务。
func GetTuiService() *Tui {
	t := &Tui{
		inputChan: make(chan string),
		done:      make(chan struct{}),
	}
	t.program = tea.NewProgram(newModel(t.inputChan))
	return t
}

// Run 阻塞运行主循环。返回即代表用户已退出，终端模式由 bubbletea 复原。
func (t *Tui) Run() {
	defer close(t.done) // 即便 panic 也要放行 Exit* 里的等待者
	if _, err := t.program.Run(); err != nil {
		panic("tui: 主循环异常退出: " + err.Error())
	}
}

// ── TuiService 契约实现（接口定义见 engine/requirements/tuiservice.go）──
// 每个方法 = 一次 Send，绝不在引擎 goroutine 里直接改 UI 状态。

// PrintToMsgView 消息区追加一组 Span 文本；clear=true 先清空（/new 场景）。
func (t *Tui) PrintToMsgView(content []pretty.Span, clear bool) {
	t.program.Send(spansMsg{spans: content, clear: clear})
}

// MarkdownDelta 流式追加 assistant 正文（最小版按源码文本渲染 markdown）。
func (t *Tui) MarkdownDelta(content string) {
	if content != "" {
		t.program.Send(markdownDeltaMsg(content))
	}
}

// MarkdownDone assistant 正文流式结束。
func (t *Tui) MarkdownDone() { t.program.Send(markdownDoneMsg{}) }

// SetTodoText 更新 todo 块文本，空串隐藏整块。
func (t *Tui) SetTodoText(text string) { t.program.Send(todoMsg(text)) }

// SetAgentRunning 切换 agent 运行态指示。
func (t *Tui) SetAgentRunning(running bool) { t.program.Send(runningMsg(running)) }

// ShowStartupBanner 把启动横幅物化进消息区（随对话滚动）。
func (t *Tui) ShowStartupBanner(infoLines []string) { t.program.Send(bannerMsg(infoLines)) }

// ShowNotice 在状态行显示一条临时通知，noticeTTL 后自动回落。
func (t *Tui) ShowNotice(msg pretty.Span) {
	t.program.Send(noticeMsg{text: msg.Text, ttl: noticeTTL})
}

// AddHelpItems 累积 slash 命令清单；ResetHelpItems 重置。
// 契约要求方法存在；最小版只存不渲染（旧版 ctrl+k 帮助面板尚未迁移）。
func (t *Tui) AddHelpItems(items []map[string]string) { t.program.Send(helpItemsMsg{items: items}) }
func (t *Tui) ResetHelpItems()                        { t.program.Send(helpItemsMsg{reset: true}) }

// SetAppFuncTriggerWithEsc 注册 esc 触发的引擎回调（运行中中断），nil 即清除。
func (t *Tui) SetAppFuncTriggerWithEsc(f func()) { t.program.Send(escFnMsg(f)) }
func (t *Tui) ClearAppFuncTrigger()              { t.program.Send(escFnMsg(nil)) }

// ListenUserInput 返回用户输入通道。引擎侧空闲时读取；没有读取者时（自动
// turn 期间）输入提交会被丢弃并保留输入框文本（见 model.go 的 enter 处理）。
func (t *Tui) ListenUserInput() chan string { return t.inputChan }

// ── 退出类方法：投递退出消息后阻塞，等主循环真正退出 ──
//
// 契约（沿用旧实现）：引擎初始化失败时调用后不再返回——若直接返回，引擎
// goroutine 会带着未初始化的状态继续跑。这里用 <-t.done（Run 返回即 close）
// 替代旧版的 select{} 永久阻塞：UI 退出 → 本方法返回 → main 结束 → 进程退出。

func (t *Tui) ShowErrorInMsgViewAndExit(errmsg []pretty.Span) { t.exitWith(errmsg, true) }
func (t *Tui) ShowSuccessInMsgViewAndExit(successMsg string) {
	t.exitWith(pretty.TSuccess(successMsg), true)
}
func (t *Tui) ShowMsgAndExitNoTrigger(msg []pretty.Span) { t.exitWith(msg, false) }

// exitWith waitKey=true 打印消息后等任意键（让用户看清错误）；false 渲染
// 一帧后自动退出。
func (t *Tui) exitWith(spans []pretty.Span, waitKey bool) {
	t.program.Send(exitRequestMsg{spans: spans, waitKey: waitKey})
	<-t.done
}
