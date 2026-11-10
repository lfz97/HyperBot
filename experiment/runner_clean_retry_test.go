// 实验6：terminal error 后的"干净重试"可行性——同 prompt 重跑时：
//  1) 第二次模型调用看到的上下文（Q 是否重复？失败痕迹是否可见？）
//  2) 与 RunWithMessages 种子路径对比（seedSessionHistory 只对空 session 生效的推论验证）
package experiment

import (
	"context"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

// failOnceModel 第一次调用返回 terminal error 响应，之后正常回答。
type failOnceModel struct{ *gatedModel }

func (m *failOnceModel) GenerateContent(
	ctx context.Context, req *model.Request,
) (<-chan *model.Response, error) {
	m.mu.Lock()
	n := m.callSeq
	m.callSeq++
	m.requests = append(m.requests, req)
	started := make(chan struct{})
	m.started = append(m.started, started)
	m.mu.Unlock()
	close(started)

	<-m.releases[n]

	ch := make(chan *model.Response, 1)
	if n == 0 {
		ch <- &model.Response{
			ID:   "resp-fail",
			Done: true,
			Error: &model.ResponseError{
				Type:    "api_error",
				Message: "simulated 500 (terminal)",
			},
		}
	} else {
		ch <- &model.Response{
			ID:   "resp-ok",
			Done: true,
			Choices: []model.Choice{{
				Index:   0,
				Message: model.NewAssistantMessage(m.answers[n]),
			}},
		}
	}
	close(ch)
	return ch, nil
}

func TestCleanRetrySamePrompt(t *testing.T) {
	gm := newGatedModel("unused-fail-slot", "A1-RETRY") // 2 个闸：调用#1 失败（failOnceModel 覆写），调用#2 成功
	fm := &failOnceModel{gatedModel: gm}
	ag := llmagent.New("retry-agent", llmagent.WithModel(fm))
	r := runner.NewRunner("retry-app", ag)
	defer r.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 第一次：terminal error（零 assistant 输出）
	ch1, err := r.Run(ctx, "u", "s", model.NewUserMessage("Q1"))
	if err != nil {
		t.Fatalf("Run#1: %v", err)
	}
	<-fm.callStarted(0)
	fm.release(0)
	sawTerminal := false
	for ev := range ch1 {
		if ev.Error != nil && ev.IsTerminalError() {
			sawTerminal = true
		}
	}
	if !sawTerminal {
		t.Fatal("Run#1 未产生 terminal error（实验前提不成立）")
	}

	// 重试方式 A：同 prompt 直接再 Run（engine turn 重试的现状形态——纠正 prompt 换成原 prompt）
	ch2, err := r.Run(ctx, "u", "s", model.NewUserMessage("Q1"))
	if err != nil {
		t.Fatalf("Run#2: %v", err)
	}
	<-fm.callStarted(1)
	fm.release(1)
	for range ch2 {
	}

	// dump：第二次模型调用看到的上下文
	t.Log("===== 同 prompt 重跑后，重试调用的模型视图 =====")
	dumpModelView(t, fm, 1)

	// 重试方式 B：RunWithMessages 种子（session 非空——验证 seedSessionHistory 是否失效）
	gmB := newGatedModel("B1", "B2")
	agB := llmagent.New("seed-agent", llmagent.WithModel(gmB))
	rB := runner.NewRunner("seed-app", agB)
	defer rB.Close()
	chB1, err := rB.Run(ctx, "u", "s", model.NewUserMessage("Q1"))
	if err != nil {
		t.Fatalf("Run#B1: %v", err)
	}
	<-gmB.callStarted(0)
	gmB.release(0)
	for range chB1 {
	}
	// 带"完整历史"种子再跑：若 seed 只对空 session 生效，Q1 仍会被再追加一次
	chB2, err := runner.RunWithMessages(ctx, rB, "u", "s", []model.Message{
		model.NewUserMessage("Q1"),
	})
	if err != nil {
		t.Fatalf("Run#B2: %v", err)
	}
	<-gmB.callStarted(1)
	gmB.release(1)
	for range chB2 {
	}
	t.Log("===== RunWithMessages 种子（非空 session）的模型视图 =====")
	dumpModelView(t, gmB, 1)
}

func dumpModelView(t *testing.T, m modelModel, call int) {
	t.Helper()
	reqs := m.snapshotRequests()
	if call >= len(reqs) {
		t.Fatalf("只有 %d 次调用，无法 dump 第 %d 次", len(reqs), call+1)
	}
	for i, msg := range reqs[call].Messages {
		content := msg.Content
		if len(content) > 40 {
			content = content[:40] + "..."
		}
		t.Logf("   [%d] %-9s %q", i, msg.Role, content)
	}
}

// modelModel 让 dumpModelView 兼容不同的模型测试类型。
type modelModel interface {
	snapshotRequests() []*model.Request
}

func (m *gatedModel) snapshotRequests() []*model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.requests
}
