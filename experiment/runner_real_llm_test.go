// 实验3：真实 LLM（qwen3.8-max，流式）+ Langfuse 追踪下的重叠 Run。
// 与实验1/2的区别：不再用 mock 控制时序，而是等 Run#1 流出第一个事件后
// 发起 Run#2，观察真实网络/流式下的上下文可见性与 session 顺序。
// trace 可在 Langfuse（jp.cloud.langfuse.com）按 appName/sessionID 查到。
package experiment

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"HyperBot/service/engine/config"
	"HyperBot/service/engine/models"
	"HyperBot/service/engine/observability"

	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"
	inmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
)

const realConfigPath = "/home/lfz/hyperbot/.hyperbot/hyperbot.yaml"

// recordingModel 包一层真实模型：记录每次请求看到的 messages 与时间线。
type recordingModel struct {
	inner model.Model

	mu        sync.Mutex
	requests  []*model.Request
	startedAt []time.Time
	lastResp  []time.Time // 每次调用收到最后一个响应的时间
}

func (m *recordingModel) Info() model.Info { return m.inner.Info() }

func (m *recordingModel) GenerateContent(
	ctx context.Context,
	req *model.Request,
) (<-chan *model.Response, error) {
	m.mu.Lock()
	n := len(m.requests)
	m.requests = append(m.requests, req)
	m.startedAt = append(m.startedAt, time.Now())
	m.lastResp = append(m.lastResp, time.Time{})
	m.mu.Unlock()

	innerCh, err := m.inner.GenerateContent(ctx, req)
	if err != nil {
		return nil, err
	}

	out := make(chan *model.Response)
	go func() {
		defer close(out)
		for resp := range innerCh {
			m.mu.Lock()
			m.lastResp[n] = time.Now()
			m.mu.Unlock()
			out <- resp
		}
	}()
	return out, nil
}

func (m *recordingModel) snapshot() (reqs []*model.Request, starts, lasts []time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.requests, m.startedAt, m.lastResp
}

func (m *recordingModel) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}

// timedRun 记录一个 run 的时间线：首事件、事件数、完成时间、最终文本。
type timedRun struct {
	name string

	mu         sync.Mutex
	firstEvent time.Time
	lastEvent  time.Time
	nEvents    int
	nPartial   int
	text       strings.Builder
	done       bool // 收到 RunnerCompletion
	finished   bool // 事件通道已关闭（drain 结束）
	errText    string

	firstEventOnce sync.Once
	finishedCh     chan struct{}
}

func newTimedRun(name string) *timedRun {
	return &timedRun{name: name, finishedCh: make(chan struct{})}
}

func (tr *timedRun) recordEvent(now time.Time, isPartial bool, delta, message string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if tr.firstEvent.IsZero() {
		tr.firstEvent = now
	}
	tr.lastEvent = now
	tr.nEvents++
	if isPartial {
		tr.nPartial++
		tr.text.WriteString(delta)
	} else if message != "" {
		tr.text.Reset()
		tr.text.WriteString(message)
	}
}

func (tr *timedRun) markDone() {
	tr.mu.Lock()
	tr.done = true
	tr.mu.Unlock()
}

func (tr *timedRun) markFinished() {
	tr.mu.Lock()
	tr.finished = true
	tr.mu.Unlock()
	close(tr.finishedCh)
}

func (tr *timedRun) awaitFirstEvent(t0 time.Time, timeout time.Duration) (time.Duration, error) {
	deadline := time.After(timeout)
	for {
		tr.mu.Lock()
		fe := tr.firstEvent
		tr.mu.Unlock()
		if !fe.IsZero() {
			return fe.Sub(t0), nil
		}
		select {
		case <-deadline:
			return 0, fmt.Errorf("timeout")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (tr *timedRun) awaitFinished() {
	<-tr.finishedCh
}

func (tr *timedRun) report(t0 time.Time) string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	sample := tr.text.String()
	if len(sample) > 60 {
		sample = sample[:60] + "..."
	}
	errInfo := ""
	if tr.errText != "" {
		errInfo = " err=" + tr.errText
	}
	return fmt.Sprintf("%s: 首事件 t=+%v, 最后事件 t=+%v, 事件数 %d (partial %d), RunnerCompletion=%v%s, 文本 %q",
		tr.name,
		tr.firstEvent.Sub(t0).Round(time.Millisecond),
		tr.lastEvent.Sub(t0).Round(time.Millisecond),
		tr.nEvents, tr.nPartial, tr.done, errInfo, sample)
}

// drainTimedRun 消费事件流并记录时间线，结束后关闭 finishedCh。
func drainTimedRun(name string, ch <-chan *event.Event, tr *timedRun) {
	for ev := range ch {
		now := time.Now()
		if ev.Error != nil {
			tr.mu.Lock()
			tr.errText = fmt.Sprintf("%v (terminal=%v)", ev.Error, ev.IsTerminalError())
			tr.mu.Unlock()
			continue
		}
		if ev.IsRunnerCompletion() {
			tr.markDone()
			continue
		}
		if ev.Response != nil && len(ev.Response.Choices) > 0 {
			c := ev.Response.Choices[0]
			tr.recordEvent(now, ev.Response.IsPartial, c.Delta.Content, c.Message.Content)
			continue
		}
		tr.recordEvent(now, false, "", "")
	}
	tr.markFinished()
}

// TestRunOverlapRealLLM 主实验：真实模型 + Langfuse。
func TestRunOverlapRealLLM(t *testing.T) {
	cfg, err := config.LoadConfig(realConfigPath)
	if err != nil {
		t.Skipf("无法加载真实配置 %s: %v", realConfigPath, err)
	}
	// 降档思考强度，控制实验时长；其余参数（流式、网关、key）全部沿用生产配置。
	cfg.Model.ReasoningEffort = "low"

	// ---- Langfuse（与 engine.Init 相同的启动方式）----
	var ob observability.ObserverService
	if cfg.Observability.Enabled {
		ob = observability.NewObservabilityService(
			observability.ServiceType(cfg.Observability.Service),
			cfg.Observability.Public, cfg.Observability.Secret,
			cfg.Observability.Host, cfg.Observability.Insecure)
		if ob == nil {
			t.Log("!! observability service 不支持，tracing 关闭")
		} else if err := ob.Start(); err != nil {
			t.Logf("!! observability start failed: %v（tracing 关闭）", err)
			ob = nil
		} else {
			defer func() { _ = ob.Stop() }() // Stop 自带 5s 超时 flush
			t.Log(">> Langfuse tracing 已启用")
		}
	}

	// ---- 装配：真实模型 + 录制包装 + in-memory session ----
	rec := &recordingModel{inner: models.Openai(cfg.Model)}
	ag := llmagent.New("overlap-real-agent",
		llmagent.WithModel(rec),
		llmagent.WithGenerationConfig(model.GenerationConfig{
			MaxTokens:       &cfg.Model.MaxTokens,
			Stream:          cfg.Model.Stream,
			ReasoningEffort: cfg.Model.ReasoningEffortPtr(),
		}),
		llmagent.WithGlobalInstruction("You are a test agent. Answer concisely in Chinese."),
	)
	sessSvc := inmemory.NewSessionService()
	r := runner.NewRunner("hyperbot-overlap-exp", ag, runner.WithSessionService(sessSvc))
	defer r.Close()

	const (
		userID    = "overlap-exp-user"
		sessionID = "overlap-real-001"
	)
	t.Logf(">> Langfuse 检索信息: appName=hyperbot-overlap-exp sessionID=%s", sessionID)

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	t0 := time.Now()

	// ---- Run#1 ----
	ch1, err := r.Run(ctx, userID, sessionID, model.NewUserMessage("Q1: 请用大约100字介绍一下你自己。"))
	if err != nil {
		t.Fatalf("Run#1: %v", err)
	}
	tr1 := newTimedRun("run1")
	go drainTimedRun("run1", ch1, tr1)

	// 等 run#1 的模型调用真正发出（进入网关请求阶段）
	deadline := time.After(30 * time.Second)
	for rec.callCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("Run#1 模型调用 30s 未发出")
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Logf(">> Run#1 已发出模型调用 (t=+%v)", time.Since(t0).Round(time.Millisecond))

	// 等 run#1 流出第一个事件（真实流式已在产出）再发 Run#2
	if _, err := tr1.awaitFirstEvent(t0, 120*time.Second); err != nil {
		t.Fatal("Run#1 120s 内没有任何事件（首 token 未到）")
	}
	t.Logf(">> Run#1 首事件已到 (t=+%v)，此刻发起 Run#2", time.Since(t0).Round(time.Millisecond))

	// ---- Run#2：run#1 流式中发起 ----
	ch2, err := r.Run(ctx, userID, sessionID, model.NewUserMessage("Q2: 用一句话说明什么是数据库索引。"))
	if err != nil {
		t.Fatalf("Run#2 直接被拒绝: %v（与 mock 实验结论不符，值得关注）", err)
	}
	t.Log(">> Run#2 未被拒绝，已拿到事件通道")
	tr2 := newTimedRun("run2")
	go drainTimedRun("run2", ch2, tr2)

	// 等两个 run 的事件流都结束
	doneAll := make(chan struct{})
	go func() { tr1.awaitFinished(); tr2.awaitFinished(); close(doneAll) }()
	select {
	case <-doneAll:
	case <-ctx.Done():
		t.Fatalf("等待 run 完成超时: run1=%v run2=%v", tr1.report(t0), tr2.report(t0))
	}
	t.Logf(">> 两个 run 事件流均结束 (t=+%v)", time.Since(t0).Round(time.Millisecond))

	// ---- Run#3：dump 最终 session 顺序 ----
	ch3, err := r.Run(ctx, userID, sessionID, model.NewUserMessage("Q3: 请只回复 OK"))
	if err != nil {
		t.Fatalf("Run#3: %v", err)
	}
	tr3 := newTimedRun("run3")
	go drainTimedRun("run3", ch3, tr3)
	tr3.awaitFinished()

	// ---- 汇报 ----
	t.Log("\n========== 实验3结果（真实 LLM + 流式 + Langfuse）==========")

	t.Log("1) 各模型调用看到的上下文（关键：Run#2 是否看到 A1；Run#3 看到的最终顺序）：")
	reqs, starts, _ := rec.snapshot()
	for i, req := range reqs {
		var b strings.Builder
		fmt.Fprintf(&b, "model call #%d (发出于 t=+%v):\n", i+1, starts[i].Sub(t0).Round(time.Millisecond))
		for j, msg := range req.Messages {
			content := msg.Content
			if len(content) > 40 {
				content = content[:40] + "..."
			}
			fmt.Fprintf(&b, "   [%d] %-9s %q\n", j, msg.Role, content)
		}
		t.Log(b.String())
	}

	t.Log("2) 事件流时间线（重叠区间直观展示）：")
	t.Log("   " + tr1.report(t0))
	t.Log("   " + tr2.report(t0))

	// 3) session 最终内容
	sess, serr := sessSvc.GetSession(context.Background(), session.Key{
		AppName: "hyperbot-overlap-exp", UserID: userID, SessionID: sessionID,
	})
	if serr != nil || sess == nil {
		t.Logf("3) session 读取失败: %v", serr)
	} else {
		t.Logf("3) session 共 %d 个事件（事件顺序即落盘顺序）", len(sess.Events))
	}
}
