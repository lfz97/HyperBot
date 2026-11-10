// 实验9：空内容 user 消息作为重试 invocation 的可行性——零手术干净重试。
//
// 依据（v1.12.0 runner.go 改写路径）：
//   persistedCurrentTurnMessages = filterPayloadMessages(currentTurnMessages)
//   → 无 payload 消息不落盘；invocation.Message 仍为该空消息。
// 判定：terminal error 后以空 user 消息重跑，模型视图是否恰为 [user "Q1"]
// （session 已含失败的 Q1，无 assistant 落盘——失败痕迹本就隐形）。
// 若成立：干净重试 = Run(空消息)，无需 [RETRY] 标记、session 拦截、BeforeModel 剥离。
package experiment

import (
	"context"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

func TestCleanRetryEmptyMessage(t *testing.T) {
	gm := newGatedModel("unused", "A1-EMPTY-RETRY")
	fm := &failOnceModel{gatedModel: gm}
	ag := llmagent.New("empty-agent", llmagent.WithModel(fm))
	r := runner.NewRunner("empty-app", ag)
	defer r.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 第 1 次：terminal error（零 assistant 落盘，session = [user Q1, error 事件]）
	ch1, err := r.Run(ctx, "u", "s", model.NewUserMessage("Q1"))
	if err != nil {
		t.Fatalf("Run#1: %v", err)
	}
	<-fm.callStarted(0)
	fm.release(0)
	for range ch1 {
	}

	// 第 2 次：空内容 user 消息（engine 重试内部调用，不经过 SubmitInput 的空输入过滤）
	ch2, err := r.Run(ctx, "u", "s", model.NewUserMessage(""))
	if err != nil {
		t.Fatalf("Run#2（空消息）: %v", err)
	}
	<-fm.callStarted(1)
	fm.release(1)
	for range ch2 {
	}

	t.Log("===== 空 invocation 消息重试的模型视图 =====")
	reqs := fm.snapshotRequests()
	nUser, userContent := 0, ""
	for i, msg := range reqs[1].Messages {
		content := msg.Content
		if len(content) > 50 {
			content = content[:50] + "..."
		}
		t.Logf("   [%d] %-9s %q", i, msg.Role, content)
		if msg.Role == model.RoleUser {
			nUser++
			userContent += msg.Content
		}
	}
	if nUser != 1 || userContent != "Q1" {
		t.Fatalf("视图不干净：user 消息 %d 条，拼接内容 %q（期望单条 Q1）", nUser, userContent)
	}
	t.Log("PASS: 空消息重试视图干净——零手术方案成立")
}

// TestCleanRetryEmptyViaRewriter 变体：[RETRY] 标记 + rewriter 返回空内容消息。
// 若框架对空 invocation 有防御，这是绕过路径（rewriter 返回值走改写路径）。
func TestCleanRetryEmptyViaRewriter(t *testing.T) {
	const retryMarker = "[RETRY]"
	gm := newGatedModel("unused", "A1-REWRITER-RETRY")
	fm := &failOnceModel{gatedModel: gm}

	ag := llmagent.New("rw-agent", llmagent.WithModel(fm))
	r := runner.NewRunner("rw-app", ag)
	defer r.Close()

	rewriter := agent.WithUserMessageRewriter(
		func(_ context.Context, args *agent.UserMessageRewriteArgs) ([]model.Message, error) {
			if args.OriginalMessage.Content == retryMarker {
				// 标记 → 返回无 payload 消息：filterPayloadMessages 会把它
				// 从落盘清单滤掉，session 零写入。
				return []model.Message{{Role: model.RoleUser, Content: ""}}, nil
			}
			return []model.Message{args.OriginalMessage}, nil
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch1, err := r.Run(ctx, "u", "s", model.NewUserMessage("Q1"))
	if err != nil {
		t.Fatalf("Run#1: %v", err)
	}
	<-fm.callStarted(0)
	fm.release(0)
	for range ch1 {
	}

	ch2, err := r.Run(ctx, "u", "s", model.NewUserMessage(retryMarker), rewriter)
	if err != nil {
		t.Fatalf("Run#2: %v", err)
	}
	<-fm.callStarted(1)
	fm.release(1)
	for range ch2 {
	}

	t.Log("===== rewriter 空消息变体的模型视图 =====")
	reqs := fm.snapshotRequests()
	nUser, userContent := 0, ""
	for i, msg := range reqs[1].Messages {
		content := msg.Content
		if len(content) > 50 {
			content = content[:50] + "..."
		}
		t.Logf("   [%d] %-9s %q", i, msg.Role, content)
		if msg.Role == model.RoleUser {
			nUser++
			userContent += msg.Content
		}
	}
	if nUser != 1 || userContent != "Q1" {
		t.Fatalf("视图不干净：user 消息 %d 条，拼接内容 %q（期望单条 Q1）", nUser, userContent)
	}
	t.Log("PASS: rewriter 变体视图干净")
}
