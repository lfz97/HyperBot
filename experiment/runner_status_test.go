// 实验4：RunStatus 原语的语义验证——拒绝策略（busy 时打回提交）的依赖。
// 验证点：
//  1) 无 run / 已结束 → RunStatus(id) 返回 false
//  2) run 进行中 → 返回 true，且带 SessionKey 等信息
//  3) false 的翻转时机：事件流抽干后（即 run 完全收尾、session 落盘后）
//  4) 只能按 requestID 查，没有按 session 查的口子
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

func TestRunStatusLifecycle(t *testing.T) {
	gm := newGatedModel("A1", "A2")
	ag := llmagent.New("status-agent", llmagent.WithModel(gm))
	r := runner.NewRunner("status-app", ag)
	defer r.Close()

	// NewRunner 返回的是基础 Runner 接口，RunStatus 在 ManagedRunner 上——需要类型断言。
	mr, ok := r.(runner.ManagedRunner)
	if !ok {
		t.Fatal("runner 未实现 ManagedRunner（RunStatus/Cancel 不可用）")
	}

	const (
		reqID  = "exp-status-1"
		userID = "u1"
		sid    = "s1"
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// ① 还没有任何 run：未知 requestID → false
	if _, exists := mr.RunStatus(reqID); exists {
		t.Fatal("① 无 run 时 RunStatus 应返回 false")
	}
	t.Log("① 无 run: RunStatus=false ✓（拒绝判定的初始态）")

	// ② 发起 Run#1（带自铸 requestID），模型挂起 → in-flight
	ch1, err := r.Run(ctx, userID, sid,
		model.NewUserMessage("Q1"), agent.WithRequestID(reqID))
	if err != nil {
		t.Fatalf("Run#1: %v", err)
	}
	<-gm.callStarted(0)

	st, exists := mr.RunStatus(reqID)
	if !exists {
		t.Fatal("② run 进行中 RunStatus 应返回 true")
	}
	t.Logf("② run 进行中: RunStatus=true ✓ SessionKey={%s %s %s} StartedAt=%v EventCount=%d",
		st.SessionKey.AppName, st.SessionKey.UserID, st.SessionKey.SessionID,
		st.StartedAt.Format("15:04:05"), st.EventCount)

	// ③ 完成后：事件流抽干 → RunStatus 翻转为 false
	gm.release(0)
	for ev := range ch1 {
		_ = ev
	}
	if _, exists = mr.RunStatus(reqID); exists {
		t.Fatal("③ run 结束后 RunStatus 应返回 false")
	}
	t.Log("③ 事件流抽干后: RunStatus=false ✓（翻转会发生在 unregisterRun，紧挨着 channel close）")

	// ④ 拒绝策略的闭环演示：busy 判定 + 决定不打第二次 Run
	ch2, err := r.Run(ctx, userID, sid,
		model.NewUserMessage("Q2"), agent.WithRequestID("exp-status-2"))
	if err != nil {
		t.Fatalf("Run#2: %v", err)
	}
	<-gm.callStarted(1)
	if _, busy := mr.RunStatus("exp-status-2"); busy {
		t.Log("④ busy 判定可用：查询方此刻应决定'拒绝提交'，而不是调第二次 Run ✓")
	}
	gm.release(1)
	for range ch2 {
	}
}
