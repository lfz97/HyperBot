package session

import (
	"HyperBot/config"
	"context"
	"embed"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/anthropic"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/model/tiktoken"
	trpcsession "trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/session/summary"
)

//go:embed prompt/*
var promptFiles embed.FS

var (
	systemSummarizerPrompt string
	userSummarizerPrompt   string
)

const (
	// summaryMinTokensThreshold 是触发摘要的绝对最小 delta token 数；
	// 当 ContextThresholdRatio * 当前模型上下文窗口小于该值时，使用本值兜底，
	// 避免上下文窗口很小的模型被过早触发。
	summaryMinTokensThreshold int = 16000
	// maxSummaryWords 是单次摘要的最大词数（占位符 {max_summary_words}）。
	maxSummaryWords int = 2000
	// ContextThresholdRatio 是当前模型上下文窗口的占用比例触发阈值。
	// 例如 0.85 表示 delta token 数达到当前模型上下文窗口的 85% 时触发摘要。
	ContextThresholdRatio float64 = 0.85
	// IdleTimeThreshold 是自上次摘要后无活动多久触发摘要。
	IdleTimeThreshold = 10 * time.Minute
)

func initSummarizerPrompts() {
	systemSummarizerPrompt_b, _ := promptFiles.ReadFile("prompt/system.md")
	systemSummarizerPrompt = string(systemSummarizerPrompt_b)
	userSummarizerPrompt_b, _ := promptFiles.ReadFile("prompt/user.md")
	userSummarizerPrompt = string(userSummarizerPrompt_b)
}

// asContextChecker 把普通 summary.Checker 提升为 summary.ContextChecker，
// 以便和 CheckContextThreshold 一起放进 WithChecksAnyContext 做 OR 组合。
func asContextChecker(c summary.Checker) summary.ContextChecker {
	return func(_ context.Context, s *trpcsession.Session) bool { return c(s) }
}

func NewSummarizer(m config.Model) summary.SessionSummarizer {
	initSummarizerPrompts()
	//设置tiktoken计算方式，默认的方式太不准确了
	counter, _ := tiktoken.New(m.Model)
	summary.SetTokenCounter(counter)
	var summarizerModel model.Model

	if m.APIType == "openai" {
		summarizerModel = openai.New(
			m.Model,
			openai.WithBaseURL(m.BaseURL),
			openai.WithAPIKey(m.APIKey),
		)

	} else if m.APIType == "anthropic" {
		summarizerModel = anthropic.New(
			m.Model,
			anthropic.WithBaseURL(m.BaseURL),
			anthropic.WithAPIKey(m.APIKey),
		)
	}
	// ── 创建 summarizer阈值 ───────────────
	sum := summary.NewSummarizer(
		summarizerModel,
		summary.WithToolCallFormatter(toolcallFormatter),     //自定义工具调用在摘要输入中的格式
		summary.WithToolResultFormatter(toolResultFormatter), //自定义工具结果在摘要输入中的格式
		summary.WithChecksAnyContext( // 任一条件满足即触发
			// 自上次摘要后无活动 IdleTimeThreshold 触发摘要。
			asContextChecker(summary.CheckTimeThreshold(IdleTimeThreshold)),
			// 自上次摘要后新增 token 占当前模型上下文窗口的比例达到 ContextThresholdRatio 时触发摘要。
			summary.CheckContextThreshold(
				summary.WithContextThresholdRatio(ContextThresholdRatio),
				summary.WithContextThresholdMinTokens(summaryMinTokensThreshold),
				// 当从 invocation/registry 解析不到模型上下文窗口时，使用 config 配置的窗口大小兜底；
				// 当 m.ContextWindow <= 0 时该 Option 是 no-op，库会回退到默认 8192。
				summary.WithContextThresholdFallbackWindow(m.ContextWindow),
			),
		),
		summary.WithMaxSummaryWords(maxSummaryWords),     //设置摘要的最大长度，单位为词
		summary.WithSystemPrompt(systemSummarizerPrompt), //设置系统提示词，指导模型如何进行摘要，默认为空，可以根据需要自定义
		summary.WithPrompt(userSummarizerPrompt),         //设置用户提示词，指导模型如何根据会话内容生成摘要，默认为空，可以根据需要自定义

	)
	return sum

}
