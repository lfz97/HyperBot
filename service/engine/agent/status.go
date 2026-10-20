package agent

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"HyperBot/service/engine/tools/functions"

	"github.com/mackerelio/go-osstat/memory"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	agentmemory "trpc.group/trpc-go/trpc-agent-go/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

const (
	memoryQueryRunes    = 100 // user prompt 取头部、assistant content 取尾部的字数
	memoryMaxResults    = 10
	memoryEntryMaxRunes = 200
)

// Display 是 agent 把文本推给展示端的小接口（按能力命名，将来加展示项直接扩方法）。
// 非交互调用方传 nil：仍注入 prompt，只是不推 UI。
type Display interface {
	SetTodoText(text string)
}

// 在消息末尾追加当前状态栏（时间、工作目录、内存、todo、相关记忆）。
// 追加在末尾而非头部：状态栏每次变化，放头部会破坏自动前缀缓存（实测尾部99%命中 vs 头部0）；
// 因此必须关闭框架的 system 前置重排（openai.WithOptimizeForCache，见 openai.go）。
// 状态栏不进 session（仅存在于当次请求副本），不污染摘要/上下文压缩。
//
// clamp 非 nil 时在注入后做动态 max_tokens 钳制。两个动作必须注册在同一个 *model.Callbacks 上：
// RegisterBeforeModel 是 append 语义，llmagent.WithModelCallbacks 是整体覆盖语义，拆开会互相顶掉。
func SetBeforeModelStatusCallback(display Display, clamp *TokenClampConfig) llmagent.Option {

	modelCallbacks := model.NewCallbacks().RegisterBeforeModel(
		func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
			var status string
			injectBaseStatus(ctx, &status)
			injectTodoStatus(ctx, display, &status)
			injectMemoryStatus(ctx, args.Request.Messages, &status)

			args.Request.Messages = append(args.Request.Messages, model.NewSystemMessage(status)) //在末尾追加状态栏
			clampMaxTokens(ctx, args.Request, clamp)                                              //钳制在状态栏注入之后：状态栏消息也计入输入
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
// ⚠️ 空串也要推给 display：todo_write 在全部 completed 时返回 ""，TodoBar 靠空串收回高度，
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

// 从消息尾部回溯拼出检索 query（user 头100字 + 最近 assistant 尾100字），去 memory service
// 检索最多 10 条追加到状态栏。检索失败或无结果时静默不追加。
func injectMemoryStatus(ctx context.Context, messages []model.Message, status *string) {
	inv, ok := agent.InvocationFromContext(ctx)
	if !ok || inv == nil || inv.MemoryService == nil || inv.Session == nil {
		return
	}
	query := buildMemoryQuery(messages)
	if query == "" {
		return
	}
	userKey := agentmemory.UserKey{AppName: inv.Session.AppName, UserID: inv.Session.UserID}
	entries, err := inv.MemoryService.SearchMemories(ctx, userKey, query,
		agentmemory.WithSearchOptions(agentmemory.SearchOptions{
			Query:               query,
			MaxResults:          memoryMaxResults,
			SimilarityThreshold: 0.03, // 默认 0.3 对此场景太苛刻，调低召回
		}))
	if err != nil || len(entries) == 0 {
		return
	}
	var sb strings.Builder
	sb.WriteString("[MEMORY]")
	count := 0
	for _, e := range entries {
		if e == nil || e.Memory == nil || strings.TrimSpace(e.Memory.Memory) == "" {
			continue
		}
		line := strings.Join(strings.Fields(e.Memory.Memory), " ")
		sb.WriteString("\n- " + headRunes(line, memoryEntryMaxRunes))
		count++
		if count >= memoryMaxResults {
			break
		}
	}
	if count == 0 {
		return
	}
	if *status != "" {
		*status += "\n"
	}
	*status += sb.String()
}

// buildMemoryQuery 从尾到头回溯消息队列，返回 "user头100字 assistant尾100字"。
// 跳过 system/tool；assistant 仅取文本 content，多段拼接后再截尾。
func buildMemoryQuery(messages []model.Message) string {
	var userPrompt string
	var assistantParts []string
	foundUser := false
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		switch msg.Role {
		case model.RoleUser:
			if foundUser {
				continue
			}
			if content := strings.TrimSpace(msg.Content); content != "" {
				userPrompt = content
				foundUser = true
			}
		case model.RoleAssistant:
			if content := strings.TrimSpace(msg.Content); content != "" {
				assistantParts = append(assistantParts, content)
			}
		}
		// 末尾是 user：继续向上直到拿到上一轮 assistant；末尾是 assistant/tool：拿到本轮 user 即停
		if foundUser && len(assistantParts) > 0 {
			break
		}
	}
	// assistantParts 是倒序收集的，翻转回时间顺序再截尾
	slices.Reverse(assistantParts)
	assistantContent := strings.Join(assistantParts, "\n")

	parts := []string{}
	if userPrompt != "" {
		parts = append(parts, headRunes(userPrompt, memoryQueryRunes))
	}
	if assistantContent != "" {
		parts = append(parts, tailRunes(assistantContent, memoryQueryRunes))
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func headRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func tailRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}
