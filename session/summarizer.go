package session

import (
	"HyperBot/config"
	"embed"
	"time"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/anthropic"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/model/tiktoken"
	"trpc.group/trpc-go/trpc-agent-go/session/summary"
)

//go:embed prompt/*
var promptFiles embed.FS

var (
	systemSummarizerPrompt string
	userSummarizerPrompt   string
)

const (
	CheckTokenThreshold       int = 100000
	summaryMinTokensThreshold int = 16000
	maxSummaryWords           int = 2000
	EventThreshold            int = 20
)

func initSummarizerPrompts() {
	systemSummarizerPrompt_b, _ := promptFiles.ReadFile("prompt/system.md")
	systemSummarizerPrompt = string(systemSummarizerPrompt_b)
	userSummarizerPrompt_b, _ := promptFiles.ReadFile("prompt/user.md")
	userSummarizerPrompt = string(userSummarizerPrompt_b)
}

func NewSummarizer(m config.Model) summary.SessionSummarizer {

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
		summary.WithChecksAny( // 任一条件满足即触发
			summary.CheckEventThreshold(EventThreshold),      // 自上次摘要后新增
			summary.CheckTokenThreshold(CheckTokenThreshold), // 自上次摘要后新增 n 个 token 后触发
			summary.CheckTimeThreshold(10*time.Minute),       //n 分钟无活动
		),
		summary.WithMaxSummaryWords(maxSummaryWords),     //设置摘要的最大长度，单位为词
		summary.WithSystemPrompt(systemSummarizerPrompt), //设置系统提示词，指导模型如何进行摘要，默认为空，可以根据需要自定义
		summary.WithPrompt(userSummarizerPrompt),         //设置用户提示词，指导模型如何根据会话内容生成摘要，默认为空，可以根据需要自定义

	)
	return sum

}
