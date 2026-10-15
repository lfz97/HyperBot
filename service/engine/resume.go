package engine

// /resume 会话恢复：列出当前 cwd 会话库里的候选、切换并重放。
//
// 编排在引擎侧（与 /exit /new /clear 同层），但刻意非阻塞：命令只负责
// 列候选推给前端弹框（openResumeMenu），agentLoop 随即回到等输入——串行
// 主循环不被 UI 交互时长绑架；选择完成后由前端调 CheckoutSession 收尾，
// running 检查兜底无头模式下的并发切换。TUI 只负责展示与提交
//（方向键/Enter/Esc），见 service/tui。
//
// 切换本体只是改 SessionId：下一轮 turn 起框架按新 id 走 GetSession，历史
// 事件与摘要自动进上下文（sqlite 持久化）；此处重放事件到 records 纯粹是
// 给前端画历史，与上下文恢复无关。

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

// openResumeMenu /resume 命令主体：列候选推给前端弹框即返回——主循环
// 不等前端（agentLoop 回到等输入），选择完成由前端调 CheckoutSession 收尾。
func (e *Engine) openResumeMenu() {
	items, err := e.listResumeItems()
	if err != nil {
		(*e).appendTyped(RecError, "读取会话列表失败："+err.Error())
		return
	}
	if len(items) == 0 {
		(*e).setNotice(NoticeWarning, "当前目录还没有历史会话")
		return
	}
	(*e).setResumeItems(items) // 弹框显隐由前端按本槽位驱动
}

// CheckoutSession 提交弹框选择（前端 enter/esc 调用；""=取消）。
//
// running 时拒绝切换：SessionId 的读写无锁，turn 进行中切换是 data race。
// TUI 场景下弹框拦截输入本不会有并发，这里是给无头模式/未来输入入口的兜底；
// 拒绝时弹框保持，用户稍后重选。
func (e *Engine) CheckoutSession(id string) error {
	if id == "" { // Esc：收起弹框，无副作用
		(*e).clearResumeItems()
		(*e).setNotice(NoticeWarning, "已取消恢复会话")
		return nil
	}
	(*e).mu.Lock()
	running := (*e).runState.Running
	(*e).mu.Unlock()
	if running {
		(*e).setNotice(NoticeWarning, "对话进行中，请稍后再恢复会话")
		return fmt.Errorf("对话进行中")
	}
	if err := e.switchSession(id); err != nil {
		(*e).appendTyped(RecError, "恢复会话失败："+err.Error())
		return err // 弹框保持，用户可重选
	}
	(*e).clearResumeItems() // 成功才收起弹框
	(*e).setNotice(NoticeSuccess, "已恢复会话 "+shortID(id))
	return nil
}

// listResumeItems 拉全量会话（带事件：主题需要首条用户输入），
// 按最近更新降序，当前会话打标。
func (e *Engine) listResumeItems() ([]ResumeItem, error) {
	sessions, err := (*e).SessionService_p.ListSessions(
		context.Background(),
		session.UserKey{AppName: (*e).Agentname, UserID: (*(*e).Config_p).User.UserID},
	)
	if err != nil {
		return nil, err
	}
	cur := (*(*e).AgentRunner_p).SessionId
	type entry struct {
		item      ResumeItem
		updatedAt time.Time
	}
	entries := make([]entry, 0, len(sessions))
	for _, s := range sessions {
		entries = append(entries, entry{
			item: ResumeItem{
				ID:        s.ID,
				Title:     firstUserText(s),
				UpdatedAt: s.UpdatedAt.Local().Format("01-02 15:04"),
				Current:   s.ID == cur,
			},
			updatedAt: s.UpdatedAt,
		})
	}
	// 按真实时间排序而非格式化文本：跨年时 "01-02 15:04" 的字典序会乱。
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].updatedAt.After(entries[j].updatedAt)
	})
	items := make([]ResumeItem, len(entries))
	for i, en := range entries {
		items[i] = en.item
	}
	return items, nil
}

// firstUserText 会话内首条用户输入作主题；压平换行、按 rune 截断。
// 流式场景 user 事件内容在 Message，兜底 Delta。
func firstUserText(s *session.Session) string {
	for i := range s.Events {
		ev := &s.Events[i]
		if ev.Author != "user" || ev.Response == nil {
			continue
		}
		for _, ch := range ev.Response.Choices {
			text := ch.Message.Content
			if text == "" {
				text = ch.Delta.Content
			}
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			text = strings.ReplaceAll(text, "\n", " ")
			if runes := []rune(text); len(runes) > 40 {
				text = string(runes[:40]) + "…"
			}
			return text
		}
	}
	return "(无文本消息)"
}

// switchSession 切到目标会话并重放事件到 records（给前端画历史）。
func (e *Engine) switchSession(id string) error {
	sess, err := (*e).SessionService_p.GetSession(
		context.Background(),
		session.Key{AppName: (*e).Agentname, UserID: (*(*e).Config_p).User.UserID, SessionID: id},
	)
	if err != nil {
		return err
	}
	if sess == nil {
		return fmt.Errorf("会话 %s 不存在", id)
	}
	(*(*e).AgentRunner_p).SessionId = id
	(*e).clearRecords()
	for i := range sess.Events {
		e.replayEvent(&sess.Events[i])
	}
	return nil
}

// replayEvent 把一条持久化事件转成 records 行，形状与 emitChoice 的定稿
// 路径对齐（user 文本走 RecUser，其余走 wireMsg(RecMessage)）。工具调用/
// 结果按原顺序重放，render.go 的 toolBuf 配对逻辑无需感知来源。
func (e *Engine) replayEvent(ev *event.Event) {
	if ev.Response == nil {
		return
	}
	if ev.Author == "user" {
		for _, ch := range ev.Response.Choices {
			text := ch.Message.Content
			if text == "" {
				text = ch.Delta.Content
			}
			if strings.TrimSpace(text) != "" {
				(*e).appendTyped(RecUser, text)
				return // 用户事件只有一条文本，取到即止
			}
		}
		return
	}
	for _, ch := range ev.Response.Choices {
		m := ch.Message
		// 与 emitChoice 的 message 过滤条件一致：空定稿不入日志
		if strings.TrimSpace(m.Content) == "" && len(m.ToolCalls) == 0 && m.Role != "tool" {
			continue
		}
		e.AppendRecordJSON(wireMsg(RecMessage, m))
	}
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
