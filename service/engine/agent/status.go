package agent

import (
	"context"
	"fmt"
	"log"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"HyperBot/service/engine/tools/functions"

	"github.com/mackerelio/go-osstat/memory"
	"github.com/rivo/uniseg"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	agentmemory "trpc.group/trpc-go/trpc-agent-go/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

const (
	memoryQuerySentences = 5
	memoryChannelResults = 5 // 每路 5 条，双路合计最多 10 条
	memoryEntryMaxRunes  = 200
)

// Display 是 agent 把文本推给展示端的小接口（按能力命名，将来加展示项直接扩方法）。
// 非交互调用方传 nil：仍注入 prompt，只是不推 UI。
type Display interface {
	SetTodoText(text string)
}

// FlightReporter 把在途后台任务摘要追加到状态栏（engine 实现，见 engine/flight.go）。
// 传 nil 时跳过该段注入——Worker 不关心兄弟任务的进度。
type FlightReporter interface {
	AppendFlightStatus(ctx context.Context, status *string)
}

// 在消息末尾追加当前状态栏（时间、工作目录、内存、todo、相关记忆、在途任务）。
// 追加在末尾而非头部：状态栏每次变化，放头部会破坏自动前缀缓存（实测尾部99%命中 vs 头部0）；
// 因此必须关闭框架的 system 前置重排（openai.WithOptimizeForCache，见 openai.go）。
// 状态栏不进 session（仅存在于当次请求副本），不污染摘要/上下文压缩。
//
// clamp 非 nil 时在注入后做动态 max_tokens 钳制。两个动作必须注册在同一个 *model.Callbacks 上：
// RegisterBeforeModel 是 append 语义，llmagent.WithModelCallbacks 是整体覆盖语义，拆开会互相顶掉。
func SetBeforeModelStatusCallback(display Display, flight FlightReporter, clamp *TokenClampConfig) llmagent.Option {
	modelCallbacks := model.NewCallbacks().RegisterBeforeModel(
		func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
			var status string
			injectBaseStatus(ctx, &status)
			injectTodoStatus(ctx, display, &status)
			injectMemoryStatus(ctx, args.Request.Messages, &status)
			if flight != nil {
				flight.AppendFlightStatus(ctx, &status)
			}

			args.Request.Messages = append(args.Request.Messages, model.NewSystemMessage(status))
			clampMaxTokens(ctx, args.Request, clamp)
			return nil, nil
		},
	)
	return llmagent.WithModelCallbacks(modelCallbacks)
}

// SetWorkerStatusCallback 只注入基础状态栏（时间、工作目录、内存）+ max_tokens 钳制。
// Worker 是无记忆的一次性执行者：不读主对话的 todo 清单、不做记忆检索、
// 不关心在途的兄弟任务——那些是主 agent 的全局视野，混进 Worker 的请求
// 只会稀释它对单一任务的注意力。
func SetWorkerStatusCallback(clamp *TokenClampConfig) llmagent.Option {
	modelCallbacks := model.NewCallbacks().RegisterBeforeModel(
		func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
			var status string
			injectBaseStatus(ctx, &status)

			args.Request.Messages = append(args.Request.Messages, model.NewSystemMessage(status))
			clampMaxTokens(ctx, args.Request, clamp)
			return nil, nil
		},
	)
	return llmagent.WithModelCallbacks(modelCallbacks)
}

// 基础状态栏：时间、工作目录、内存。
func injectBaseStatus(_ context.Context, status *string) {
	datenow := time.Now().Format("2006-01-02 15:04:05")
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "UNKNOWN"
	}
	var memTotalStr string
	var memNowStr string
	memoryInfo, err := memory.Get()
	if err == nil {
		memTotalStr = strconv.FormatUint(memoryInfo.Total/1024/1024, 10)
		memNowStr = strconv.FormatUint(memoryInfo.Used/1024/1024, 10)
	} else {
		memTotalStr = "UNKNOWN"
		memNowStr = "UNKNOWN"
	}
	*status += fmt.Sprintf(`[STATUS] TIMENOW: %s , CWD: %s , MEMORY USAGE: %s/%s MB`, datenow, cwd, memNowStr, memTotalStr)
}

// 追加当前 agent 的 todo 清单（无清单时为空串不追加）。清单变化只在尾部，不破坏前缀缓存。
// 同一份文本推两个去处：prompt 的 [STATUS] 尾部 + display 的 TodoBar。
// 空串也要推给 display：todo_write 在全部 completed 时返回 ""，TodoBar 靠空串收回高度，
// 只在非空时推会让 bar 永远挂着最后一版内容。
func injectTodoStatus(ctx context.Context, display Display, status *string) {
	todoStatus := functionTools.TodoStatusBar(ctx)
	if display != nil {
		display.SetTodoText(todoStatus)
	}
	if todoStatus != "" {
		if *status != "" {
			*status += "\n"
		}
		*status += todoStatus
	}
}

// 每次模型调用独立检索：用户完整输入召回相似任务，当前轮所有 reasoning 的尾部
// 五句召回执行现场经验。reasoning 不存在时只查用户输入，不使用 content 兜底。
func injectMemoryStatus(ctx context.Context, messages []model.Message, status *string) {
	inv, ok := agent.InvocationFromContext(ctx)
	if !ok || inv == nil || inv.MemoryService == nil || inv.Session == nil {
		return
	}
	userQuery, reasoningQuery := buildMemoryQueries(messages)
	if userQuery == "" {
		return
	}
	userKey := agentmemory.UserKey{AppName: inv.Session.AppName, UserID: inv.Session.UserID}
	search := func(query string) ([]*agentmemory.Entry, error) {
		return inv.MemoryService.SearchMemories(ctx, userKey, query,
			agentmemory.WithSearchOptions(agentmemory.SearchOptions{
				Query:               query,
				MaxResults:          memoryChannelResults,
				SimilarityThreshold: 0.03,
			}))
	}

	userEntries, err := search(userQuery)
	if err != nil {
		log.Printf("[MEMORY] turn=%s user search failed: %v", inv.InvocationID, err)
		return
	}
	var reasoningEntries []*agentmemory.Entry
	if reasoningQuery != "" {
		reasoningEntries, err = search(reasoningQuery)
		if err != nil {
			log.Printf("[MEMORY] turn=%s reasoning search failed: %v", inv.InvocationID, err)
			return
		}
	}

	block, logLine := renderMemoryEntries(userEntries, reasoningEntries, reasoningQuery != "")
	log.Printf("[MEMORY] turn=%s user_query=%q reasoning_query=%q %s",
		inv.InvocationID, userQuery, reasoningQuery, logLine)
	appendStatusBlock(status, block)
}

// buildMemoryQueries 收集当前轮所有 assistant 的 reasoning，遇到最近 user 即停。
func buildMemoryQueries(messages []model.Message) (string, string) {
	var reasoningParts []string
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		switch msg.Role {
		case model.RoleUser:
			if content := strings.TrimSpace(msg.Content); content != "" {
				slices.Reverse(reasoningParts)
				reasoning := strings.Join(reasoningParts, " ")
				return content, tailSentences(reasoning, memoryQuerySentences)
			}
		case model.RoleAssistant:
			if reasoning := strings.TrimSpace(msg.ReasoningContent); reasoning != "" {
				reasoningParts = append(reasoningParts, reasoning)
			}
		}
	}
	return "", ""
}

func tailSentences(s string, maxSentences int) string {
	rest := strings.TrimSpace(s)
	if rest == "" || maxSentences <= 0 {
		return ""
	}
	var sentences []string
	state := -1
	for rest != "" {
		var seg string
		seg, rest, state = uniseg.FirstSentenceInString(rest, state)
		if seg == "" {
			break
		}
		if sentence := strings.TrimSpace(seg); sentence != "" {
			sentences = append(sentences, sentence)
		}
	}
	if len(sentences) > maxSentences {
		sentences = sentences[len(sentences)-maxSentences:]
	}
	return strings.Join(sentences, " ")
}

// renderMemoryEntries 分渠道展示并按渠道内最高分归一化；不同 query 的原始分不直接比较。
func renderMemoryEntries(userEntries, reasoningEntries []*agentmemory.Entry, includeReasoning bool) (string, string) {
	seen := make(map[string]bool)
	userEntries = prepareMemoryEntries(userEntries, seen)
	reasoningEntries = prepareMemoryEntries(reasoningEntries, seen)

	var sb strings.Builder
	sb.WriteString("[MEMORY]")
	appendMemoryChannel(&sb, "USER", userEntries)
	logLine := fmt.Sprintf("user=%d %s", len(userEntries), memoryEntryTags(userEntries))
	if includeReasoning {
		appendMemoryChannel(&sb, "REASONING", reasoningEntries)
		logLine += fmt.Sprintf(" reasoning=%d %s", len(reasoningEntries), memoryEntryTags(reasoningEntries))
	}
	return sb.String(), strings.TrimSpace(logLine)
}

func prepareMemoryEntries(entries []*agentmemory.Entry, seen map[string]bool) []*agentmemory.Entry {
	prepared := make([]*agentmemory.Entry, 0, memoryChannelResults)
	for _, entry := range entries {
		if entry == nil || entry.Memory == nil || strings.TrimSpace(entry.Memory.Memory) == "" || seen[entry.ID] {
			continue
		}
		seen[entry.ID] = true
		prepared = append(prepared, entry)
		if len(prepared) == memoryChannelResults {
			break
		}
	}
	return prepared
}

func appendMemoryChannel(sb *strings.Builder, label string, entries []*agentmemory.Entry) {
	sb.WriteString("\n[" + label + "]")
	if len(entries) == 0 {
		sb.WriteString(" 0 hits")
		return
	}
	maxScore := 0.0
	for _, entry := range entries {
		if entry.Score > maxScore {
			maxScore = entry.Score
		}
	}
	for _, entry := range entries {
		percentage := 0
		if maxScore > 0 {
			percentage = int(entry.Score/maxScore*100 + 0.5)
		}
		body := cutAtSentence(entryText(entry), memoryEntryMaxRunes)
		sb.WriteString(fmt.Sprintf("\n- 可能与当前相关（%d%%）：%s", percentage, body))
	}
}

func memoryEntryTags(entries []*agentmemory.Entry) string {
	tags := make([]string, 0, len(entries))
	for _, entry := range entries {
		tags = append(tags, fmt.Sprintf("%s:%.2f", entry.ID, entry.Score))
	}
	return strings.Join(tags, " ")
}

func appendStatusBlock(status *string, block string) {
	if block == "" {
		return
	}
	if *status != "" {
		*status += "\n"
	}
	*status += block
}

func entryText(e *agentmemory.Entry) string {
	if e == nil || e.Memory == nil {
		return ""
	}
	return strings.Join(strings.Fields(e.Memory.Memory), " ")
}

// cutAtSentence 在 maxRunes 内按 UAX #29 句边界截断：优先取不超过上限的最后
// 一个边界；没有则放宽到 1.5 倍取第一个，仍没有才硬切，避免注入半句诱发补全。
func cutAtSentence(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	relaxed := min(len(r), maxRunes*3/2)
	cutIn, cutRelaxed := 0, 0
	rest, state := s, -1
	consumed := 0
	for rest != "" {
		var seg string
		seg, rest, state = uniseg.FirstSentenceInString(rest, state)
		if seg == "" {
			break
		}
		consumed += len([]rune(seg))
		if consumed <= maxRunes {
			cutIn = consumed
		} else if consumed <= relaxed && cutRelaxed == 0 {
			cutRelaxed = consumed
		} else {
			break
		}
	}
	if cutIn > 0 {
		return string(r[:cutIn])
	}
	if cutRelaxed > 0 {
		return string(r[:cutRelaxed])
	}
	return string(r[:maxRunes])
}
