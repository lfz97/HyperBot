// 实验2：Run#1 与 Run#2 同时完成时，session 里的 assistant 顺序是否确定。
// 多次迭代统计分布——如果顺序随迭代变化，说明并发 Run 的 session 顺序
// 是非确定性的（取决于事件循环 goroutine 调度）。
package experiment

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

// TestRunOverlapSimultaneousCompletion 同时放行两个 run，迭代观察落盘顺序。
func TestRunOverlapSimultaneousCompletion(t *testing.T) {
	const iterations = 30
	orderCount := map[string]int{}

	for i := 0; i < iterations; i++ {
		order := oneOverlapIteration(t, i)
		orderCount[order]++
	}

	t.Log("\n========== 实验2结果：同时完成时的 session 顺序分布 ==========")
	for order, n := range orderCount {
		t.Logf("  %-20s %d/%d 次", order, n, iterations)
	}
}

// oneOverlapIteration 跑一次完整的重叠场景，返回两个 assistant 的落盘顺序。
func oneOverlapIteration(t *testing.T, iter int) string {
	gm := newGatedModel("A1", "A2", "dump")
	ag := llmagent.New("race-agent", llmagent.WithModel(gm))
	r := runner.NewRunner(fmt.Sprintf("race-app-%d", iter), ag)
	defer r.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sessionID := fmt.Sprintf("s-%d", iter)

	// 两个 run 先后发起（run#1 挂起在模型上，run#2 再发起、也挂起）
	ch1, err := r.Run(ctx, "u", sessionID, model.NewUserMessage("Q1"))
	if err != nil {
		t.Fatalf("iter %d: Run#1: %v", iter, err)
	}
	<-gm.callStarted(0)
	ch2, err := r.Run(ctx, "u", sessionID, model.NewUserMessage("Q2"))
	if err != nil {
		t.Fatalf("iter %d: Run#2: %v", iter, err)
	}
	<-gm.callStarted(1)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); drainRun("run1", ch1) }()
	go func() { defer wg.Done(); drainRun("run2", ch2) }()

	// 关键点：背靠背放行，不给 run#1 留任何时间优势——两个完成事件同场竞速。
	gm.release(0)
	gm.release(1)
	wg.Wait()

	// dump：看最终 session 里 A1/A2 谁在前
	ch3, err := r.Run(ctx, "u", sessionID, model.NewUserMessage("Q3"))
	if err != nil {
		t.Fatalf("iter %d: Run#3: %v", iter, err)
	}
	<-gm.callStarted(2)
	gm.release(2)
	for range ch3 {
	}

	var roles []string
	for _, msg := range gm.requests[2].Messages {
		roles = append(roles, string(msg.Role)+":"+truncate(msg.Content, 12))
	}
	return strings.Join(roles, " ")
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
