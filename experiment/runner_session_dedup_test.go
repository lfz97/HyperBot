// 实验7：干净重试的最小实现验证——session 层幂等去重是否足够。
//
// 假设：模型请求由 session 事件构建（实验6旁证："Q1\n\nQ1" 是两条已落盘 user
// 事件被合并的结果）。若成立，则"跳过重复 user 消息落盘"即可让重试视图干净，
// 无需 [RETRY] 标记、无需 BeforeModel 改写请求。
//
// 装置：failOnceModel（第 1 次调用 terminal error）+ dedupeSessionService
// （尾部已有同内容未回复 user 时跳过落盘）。
// 判定：重试调用的模型视图若为单个 "Q1"（无合并、无重复）→ 假设成立。
package experiment

import (
	"context"
	"strings"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"
	inmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
)

// dedupeSessionService 包装内存 session service：AppendEvent 时若传入的是
// user 消息事件、且自己记录的尾部（自上一条 assistant 起）已有同内容 user
// 消息，则跳过落盘（幂等）。其余全部透传。
type dedupeSessionService struct {
	session.Service

	// roleSeq 记录已透传的消息事件序列（role:content），用于判定"尾部同内容未回复"。
	roleSeq []string
}

func (s *dedupeSessionService) eventUserContent(evt *event.Event) (string, bool) {
	if evt == nil || evt.Response == nil || len(evt.Response.Choices) == 0 {
		return "", false
	}
	m := evt.Response.Choices[0].Message
	if m.Role != model.RoleUser {
		return "", false
	}
	return m.Content, true
}

func (s *dedupeSessionService) AppendEvent(
	ctx context.Context, sess *session.Session, evt *event.Event, opts ...session.Option,
) error {
	if content, ok := s.eventUserContent(evt); ok {
		if len(s.roleSeq) > 0 && s.roleSeq[len(s.roleSeq)-1] == "user:"+content {
			return nil // 尾部已是同内容未回复 user：幂等跳过（干净重试的机制核心）
		}
		s.roleSeq = append(s.roleSeq, "user:"+content)
	} else if evt.Response != nil && len(evt.Response.Choices) > 0 &&
		evt.Response.Choices[0].Message.Role == model.RoleAssistant {
		s.roleSeq = append(s.roleSeq, "assistant")
	}
	return s.Service.AppendEvent(ctx, sess, evt, opts...)
}

func TestCleanRetryViaSessionDedup(t *testing.T) {
	gm := newGatedModel("unused", "A1-CLEAN-RETRY")
	fm := &failOnceModel{gatedModel: gm}
	ag := llmagent.New("dedup-agent", llmagent.WithModel(fm))

	svc := &dedupeSessionService{Service: inmemory.NewSessionService()}
	r := runner.NewRunner("dedup-app", ag, runner.WithSessionService(svc))
	defer r.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 第 1 次：terminal error（零 assistant 落盘）
	ch1, err := r.Run(ctx, "u", "s", model.NewUserMessage("Q1"))
	if err != nil {
		t.Fatalf("Run#1: %v", err)
	}
	<-fm.callStarted(0)
	fm.release(0)
	for range ch1 {
	}

	// 第 2 次：同 prompt 重跑（干净重试的候选形态：不加标记、不改钩子）
	ch2, err := r.Run(ctx, "u", "s", model.NewUserMessage("Q1"))
	if err != nil {
		t.Fatalf("Run#2: %v", err)
	}
	<-fm.callStarted(1)
	fm.release(1)
	for range ch2 {
	}

	t.Log("===== session 层幂等去重后的重试模型视图 =====")
	reqs := fm.snapshotRequests()
	for i, msg := range reqs[1].Messages {
		content := msg.Content
		if len(content) > 50 {
			content = content[:50] + "..."
		}
		t.Logf("   [%d] %-9s %q", i, msg.Role, content)
	}

	// 判定：视图里 user 消息内容为 "Q1\n\nQ1"——记录一个重要事实：
	// 模型请求 = session 历史 + 当前 invocation 消息（同内容连续 user 被拼接）。
	// 即使 session 侧去重生效，invocation 消息仍独立进入请求——
	// 因此干净重试必须同时处理请求侧（BeforeModel），见 TestCleanRetryMarkerDesign。
	if len(reqs[1].Messages) != 1 || reqs[1].Messages[0].Content != "Q1\n\nQ1" {
		t.Fatalf("视图=%+v, 期望单条 \"Q1\\n\\nQ1\"（session+invocation 拼接证据）", reqs[1].Messages)
	}
	t.Log("记录: 请求 = session + invocation（去重 session 后仍拼接 → BeforeModel 剥离是必需件）")
}

// TestCleanRetryMarkerDesign 完整三件套：[RETRY] 标记 + session 层跳过标记落盘 +
// BeforeModel 从请求剔除标记消息。判定：重试视图恰好回到 [user "Q1"]。
func TestCleanRetryMarkerDesign(t *testing.T) {
	const retryMarker = "[RETRY]"

	gm := newGatedModel("unused", "A1-MARKER-RETRY")
	fm := &failOnceModel{gatedModel: gm}

	// 件③：BeforeModel 剥离。注意框架会把连续 user 消息合并为一条（"\n\n" 拼接，
	// 实测 "Q1\n\n[RETRY]"），所以按分段过滤：user 消息按 \n\n 拆开，丢掉恰为
	// 标记的段，重拼；全为标记段的整条丢弃。
	strip := llmagent.WithModelCallbacks(model.NewCallbacks().RegisterBeforeModel(
		func(_ context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
			keptMsgs := args.Request.Messages[:0]
			for _, m := range args.Request.Messages {
				if m.Role == model.RoleUser && strings.Contains(m.Content, retryMarker) {
					parts := strings.Split(m.Content, "\n\n")
					keptParts := parts[:0]
					for _, p := range parts {
						if p != retryMarker {
							keptParts = append(keptParts, p)
						}
					}
					if len(keptParts) == 0 {
						continue // 整条都是标记：丢弃
					}
					m.Content = strings.Join(keptParts, "\n\n")
				}
				keptMsgs = append(keptMsgs, m)
			}
			args.Request.Messages = keptMsgs
			return nil, nil
		},
	))
	ag := llmagent.New("marker-agent", llmagent.WithModel(fm), strip)

	// 件②：session 层跳过标记消息落盘（事前不落盘，而非事后移除——
	// 框架无 delete-event API，事后移除需要裸 SQL 动框架表，脆）。
	svc := &markerSkipSessionService{Service: inmemory.NewSessionService()}
	r := runner.NewRunner("marker-app", ag, runner.WithSessionService(svc))
	defer r.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 第 1 次：terminal error
	ch1, err := r.Run(ctx, "u", "s", model.NewUserMessage("Q1"))
	if err != nil {
		t.Fatalf("Run#1: %v", err)
	}
	<-fm.callStarted(0)
	fm.release(0)
	for range ch1 {
	}

	// 第 2 次：[RETRY] 标记重试
	ch2, err := r.Run(ctx, "u", "s", model.NewUserMessage(retryMarker))
	if err != nil {
		t.Fatalf("Run#2: %v", err)
	}
	<-fm.callStarted(1)
	fm.release(1)
	for range ch2 {
	}

	t.Log("===== 三件套标记重试的模型视图 =====")
	reqs := fm.snapshotRequests()
	nUser := 0
	for i, msg := range reqs[1].Messages {
		t.Logf("   [%d] %-9s %q", i, msg.Role, msg.Content)
		if msg.Role == model.RoleUser {
			nUser++
		}
	}
	if nUser != 1 || reqs[1].Messages[len(reqs[1].Messages)-1].Content != "Q1" {
		t.Fatalf("重试视图不干净：user 数=%d（期望 1，内容恰为 Q1）", nUser)
	}
	if svc.sawMarker {
		t.Log("标记已拦截（未落盘）✓")
	}
	t.Log("PASS: 三件套成立——[RETRY] 标记 + session 跳过 + BeforeModel 剥离 = 干净重试")
}

// markerSkipSessionService 跳过内容为 [RETRY] 的 user 消息落盘，其余透传。
type markerSkipSessionService struct {
	session.Service
	sawMarker bool
}

func (s *markerSkipSessionService) AppendEvent(
	ctx context.Context, sess *session.Session, evt *event.Event, opts ...session.Option,
) error {
	if evt != nil && evt.Response != nil && len(evt.Response.Choices) > 0 {
		m := evt.Response.Choices[0].Message
		if m.Role == model.RoleUser && m.Content == "[RETRY]" {
			s.sawMarker = true
			return nil // 件②：标记消息不落盘
		}
	}
	return s.Service.AppendEvent(ctx, sess, evt, opts...)
}
