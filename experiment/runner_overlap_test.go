// Package experiment 验证一个问题：runner.Run 在上一轮还没结束时再次调用，
// 框架层会发生什么（错误？排队？交错写 session？）。
// 这是在评估"去掉 engine 层 agentLoop、让每次用户输入直接调 Run"的可行性。
package experiment

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

// gatedModel 每次调用先登记 request、广播"已进入模型"，然后阻塞等待对应的
// release 通道，放行后返回固定文本。这样能精确控制每个 run 何时结束。
type gatedModel struct {
	name string

	mu       sync.Mutex
	requests []*model.Request
	started  []chan struct{} // 第 n 次调用已进入（带缓冲，广播一次）
	callSeq  int

	releases []chan struct{} // 预置 3 个放行闸
	answers  []string        // 每次调用的固定回答
}

func newGatedModel(answers ...string) *gatedModel {
	m := &gatedModel{name: "gated-model", answers: answers}
	for range answers {
		m.releases = append(m.releases, make(chan struct{}))
	}
	return m
}

func (m *gatedModel) release(n int) { close(m.releases[n]) }

// callStarted 阻塞等待第 n 次模型调用登记后再返回其信号通道。
func (m *gatedModel) callStarted(n int) <-chan struct{} {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		if len(m.started) > n {
			ch := m.started[n]
			m.mu.Unlock()
			return ch
		}
		m.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	panic(fmt.Sprintf("gatedModel: model call #%d never started within 10s", n+1))
}

func (m *gatedModel) GenerateContent(
	_ context.Context,
	req *model.Request,
) (<-chan *model.Response, error) {
	m.mu.Lock()
	n := m.callSeq
	m.callSeq++
	m.requests = append(m.requests, req)
	started := make(chan struct{})
	m.started = append(m.started, started)
	m.mu.Unlock()
	close(started) // 广播：本次模型调用已进入

	<-m.releases[n] // 阻塞直到实验方放行

	ch := make(chan *model.Response, 1)
	ch <- &model.Response{
		ID:   fmt.Sprintf("resp-%d", n+1),
		Done: true,
		Choices: []model.Choice{{
			Index:   0,
			Message: model.NewAssistantMessage(m.answers[n]),
		}},
	}
	close(ch)
	return ch, nil
}

func (m *gatedModel) Info() model.Info { return model.Info{Name: m.name} }

func (m *gatedModel) dumpRequests() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for i, req := range m.requests {
		var b strings.Builder
		fmt.Fprintf(&b, "model call #%d saw %d messages:\n", i+1, len(req.Messages))
		for j, msg := range req.Messages {
			content := msg.Content
			if len(content) > 60 {
				content = content[:60] + "..."
			}
			fmt.Fprintf(&b, "  [%d] %-9s %q\n", j, msg.Role, content)
		}
		out = append(out, b.String())
	}
	return out
}

// drainRun 收集一个 run 的事件流直到关闭，记录关键事件摘要。
func drainRun(name string, ch <-chan *event.Event) []string {
	var summary []string
	for ev := range ch {
		switch {
		case ev.Error != nil:
			summary = append(summary, fmt.Sprintf("%s: ERROR event: %v (terminal=%v)",
				name, ev.Error, ev.IsTerminalError()))
		case ev.IsRunnerCompletion():
			summary = append(summary, fmt.Sprintf("%s: RunnerCompletion", name))
		default:
			if ev.Response != nil && len(ev.Response.Choices) > 0 {
				c := ev.Response.Choices[0]
				content := c.Message.Content
				if c.Delta.Content != "" {
					content = c.Delta.Content
				}
				if len(content) > 40 {
					content = content[:40] + "..."
				}
				summary = append(summary, fmt.Sprintf("%s: response %q (partial=%v)",
					name, content, ev.Response.IsPartial))
			}
		}
	}
	summary = append(summary, fmt.Sprintf("%s: event channel CLOSED", name))
	return summary
}

// TestRunOverlap 是主实验：Run#1 未结束时发起 Run#2（同一 user+session）。
func TestRunOverlap(t *testing.T) {
	gm := newGatedModel(
		"answer-ONE",  // run#1 的模型回答
		"answer-TWO",  // run#2 的模型回答
		"answer-THREE", // run#3 复查最终 session 顺序
	)

	ag := llmagent.New("overlap-agent", llmagent.WithModel(gm))
	r := runner.NewRunner("overlap-app", ag)
	defer r.Close()

	const (
		userID    = "u1"
		sessionID = "s1"
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// ---- Run #1 ----
	t.Log(">> 发起 Run#1 (Q1)")
	ch1, err := r.Run(ctx, userID, sessionID, model.NewUserMessage("Q1"))
	if err != nil {
		t.Fatalf("Run#1 failed: %v", err)
	}
	var ev1 []string
	go func() { ev1 = drainRun("run1", ch1) }()

	<-gm.callStarted(0) // 等模型调用#1进入（run#1 处于 in-flight）

	// ---- Run #2：在 run#1 未结束时发起 ----
	t.Log(">> Run#1 仍在进行中，发起 Run#2 (Q2)")
	ch2, err := r.Run(ctx, userID, sessionID, model.NewUserMessage("Q2"))
	if err != nil {
		t.Logf("!! Run#2 直接返回错误（框架拒绝并发）: %v", err)
		return
	}
	t.Log(">> Run#2 未被拒绝，拿到了事件通道")
	var ev2 []string
	go func() { ev2 = drainRun("run2", ch2) }()

	select {
	case <-gm.callStarted(1):
		t.Log(">> Run#2 已真正进入模型调用（与 Run#1 并行执行中）")
	case <-time.After(5 * time.Second):
		t.Fatal("Run#2 的模型调用 5s 内未出现：Run#2 被阻塞在某处（可能排队等锁）")
	}

	// ---- 放行 Run#1 ----
	t.Log(">> 放行 Run#1 的模型 → Run#1 完成")
	gm.release(0)
	time.Sleep(500 * time.Millisecond) // 给 run#1 事件循环时间落盘 assistant-1

	// ---- 放行 Run#2 ----
	t.Log(">> 放行 Run#2 的模型 → Run#2 完成")
	gm.release(1)
	time.Sleep(500 * time.Millisecond)

	// ---- Run #3：dump 最终 session 状态 ----
	t.Log(">> 发起 Run#3 复查最终 session 内容")
	ch3, err := r.Run(ctx, userID, sessionID, model.NewUserMessage("Q3"))
	if err != nil {
		t.Fatalf("Run#3 failed: %v", err)
	}
	<-gm.callStarted(2)
	gm.release(2)
	for range ch3 {
	}

	// ---- 汇报 ----
	t.Log("\n========== 实验结果 ==========")
	t.Log("1) 各模型调用看到的上下文（关键证据：Run#2 是否看到 assistant-1）：")
	for _, d := range gm.dumpRequests() {
		t.Log(d)
	}
	t.Log("2) Run#1 事件流：")
	for _, s := range ev1 {
		t.Log("   " + s)
	}
	t.Log("3) Run#2 事件流：")
	for _, s := range ev2 {
		t.Log("   " + s)
	}
}

// TestSameRequestIDConcurrent 辅助实验：同一 RequestID 并发 Run 才是框架唯一的
// 并发拒绝点（registerRun 按 requestID 去重；默认每次 Run 自动生成新 uuid）。
func TestSameRequestIDConcurrent(t *testing.T) {
	gm := newGatedModel("a", "b")
	ag := llmagent.New("dup-agent", llmagent.WithModel(gm))
	r := runner.NewRunner("dup-app", ag)
	defer r.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const reqID = "fixed-request-id"
	_, err1 := r.Run(ctx, "u", "s", model.NewUserMessage("m1"), agent.WithRequestID(reqID))
	if err1 != nil {
		t.Fatalf("first Run failed: %v", err1)
	}
	_, err2 := r.Run(ctx, "u", "s", model.NewUserMessage("m2"), agent.WithRequestID(reqID))
	t.Logf("第二个同 RequestID 的 Run 返回: err=%v", err2)
}
