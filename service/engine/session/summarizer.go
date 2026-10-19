package session

import (
	"HyperBot/service/engine/config"
	"HyperBot/service/engine/models"
	"embed"
	"fmt"
	"regexp"
	"strings"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/tiktoken"
	"trpc.group/trpc-go/trpc-agent-go/session/summary"
)

//go:embed prompt/*
var promptFiles embed.FS

var (
	systemSummarizerPrompt string
	userSummarizerPrompt   string
	reThink                = regexp.MustCompile(`<think>[\s\S]*?<\/think>`)
)

const (
	CheckTokenThresholdPercent float64 = 0.6
	maxSummaryWords            int     = 5000
)

func initSummarizerPrompts() {
	systemSummarizerPrompt_b, _ := promptFiles.ReadFile("prompt/system.md")
	systemSummarizerPrompt = string(systemSummarizerPrompt_b)
	userSummarizerPrompt_b, _ := promptFiles.ReadFile("prompt/user.md")
	userSummarizerPrompt = string(userSummarizerPrompt_b)
}

// summarySink 本包对展示端的全部需求：摘要生成后投一条摘要记录。
// 在消费方按需声明小接口（*Engine 天然满足）——
// session 包不感知展示端是谁，也不依赖任何 TUI 类型。
type summarySink interface {
	AppendSummary(text string)
}

func NewSummarizer(m config.Model, sink summarySink) summary.SessionSummarizer {
	return NewSummarizerWithThreshold(m, sink, CheckTokenThresholdPercent)
}

// NewSummarizerWithThreshold 与 NewSummarizer 相同，但用调用方给定的
// 阈值百分比替代包级默认 CheckTokenThresholdPercent（相对 m.ContextWindow）。
// 供阈值评测（cmd/ctxbench）复用生产代码路径；生产路径不传自定义值。
func NewSummarizerWithThreshold(m config.Model, sink summarySink, thresholdPercent float64) summary.SessionSummarizer {
	initSummarizerPrompts()
	//设置tiktoken计算方式，默认的方式太不准确了
	counter, _ := tiktoken.New(m.Model)
	summary.SetTokenCounter(counter)
	var summarizerModel model.Model

	if m.APIType == "openai" {
		summarizerModel = models.Openai(m)
	} else if m.APIType == "anthropic" {
		summarizerModel = models.Anthropic(m)
	}
	opts := []summary.Option{
		// 触发条件：请求总占用超过 contextwindow 的 thresholdPercent（v1.11 语义，对标
		// Codex CLI/Claude Code 的零配置压缩）。请求上下文可得时用最终请求的本地计数
		// （llmflow.go/runner.go 注入 model-visible view），无上下文时退回 delta 估算——与
		// 旧 CheckTokenThreshold 数值一致。fallback 不显式设置：框架会自动用 summarizer
		// 模型（models.Openai 带的 yaml contextwindow）兜底，与显式配置同值。
		// 注意：两机制同时挂会被 s.checks 的 AND 短路求值叠加成"两者都过才触发"，勿混用。
		summary.WithContextThreshold(
			summary.WithContextThresholdRatio(thresholdPercent),
		),
		summary.WithMaxSummaryWords(maxSummaryWords),     //设置摘要的最大长度，单位为词
		summary.WithSystemPrompt(systemSummarizerPrompt), //设置系统提示词，指导模型如何进行摘要，默认为空，可以根据需要自定义
		summary.WithPrompt(userSummarizerPrompt),         //设置用户提示词，指导模型如何根据会话内容生成摘要，默认为空，可以根据需要自定义
		summary.WithSkipRecent(func(events []event.Event) int { //保留最后一轮完整对话
			skip := 0
			for i := len(events) - 1; i >= 0; i-- {
				skip++
				if events[i].Author == "user" {
					return skip
				}
			}
			return 0 // 没有user event则不跳过（极端情况）
		}),
		summary.WithToolResultFormatter(func(msg model.Message) string { //送入摘要前，将tool result进行简化，避免过长的内容导致摘要质量下降
			content := strings.TrimSpace(msg.Content)
			runes := []rune(content)
			if len(runes) > 1000 {
				head := string(runes[:500])
				tail := string(runes[len(runes)-500:])
				content = head + "\n...[output truncated]...\n" + tail
			}
			return fmt.Sprintf("[%s returned: %s]", msg.ToolName, content)
		}),
	}
	if sink != nil {
		opts = append(opts, summary.WithPostSummaryHook(func(s *summary.PostSummaryHookContext) error {
			cleanSummary := reThink.ReplaceAllString(s.Summary, "") //将摘要内容中的<think>...</think>部分去掉
			sink.AppendSummary(cleanSummary)
			return nil
		}))
	}
	// ── 创建 summarizer阈值 ───────────────
	sum := summary.NewSummarizer(
		summarizerModel,
		opts...,
	)
	return sum

}
