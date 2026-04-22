package session

import (
	"HyperBot/config"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/anthropic"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/model/tiktoken"
	"trpc.group/trpc-go/trpc-agent-go/session/summary"
)

// claude code 模式的摘要生成提示词
const (
	systemSummarizerPrompt string = "CRITICAL: Respond with TEXT ONLY. Do NOT call any tools.\n" +
		"CRITICAL:MAX SUMMARY WORDS: {max_summary_words}\n" +
		"- You already have all the context you need in the conversation above.\n" +
		"- Tool calls will be REJECTED and will waste your only turn.\n" +
		"- Your entire response must be plain text: an <analysis> block followed by a <summary> block.\n\n" +
		"IMPORTANT: Before providing your final summary, wrap your analysis in <analysis> tags.\n" +
		"Then provide your summary in a <summary> block with these sections:\n" +
		"1. Primary Request and Intent\n" +
		"2. Key Technical Concepts\n" +
		"3. Files and Code Sections (with full snippets)\n" +
		"4. Errors and Fixes\n" +
		"5. Problem Solving\n" +
		"6. All User Messages\n" +
		"7. Pending Tasks\n" +
		"8. Current Work\n" +
		"9. Optional Next Step (with direct quotes from conversation)"
	userSummarizerPrompt string = "{conversation_text}\n\n" +
		"REMINDER: Do NOT call any tools. Respond with plain text only.\n" +
		"Output format:\n\n" +
		"<analysis>\n" +
		"[Your analysis process, then strip this part when formatting]\n" +
		"</analysis>\n\n" +
		"<summary>\n" +
		"1. Primary Request and Intent:\n" +
		"[...]\n\n" +
		"2. Key Technical Concepts:\n" +
		"- [...]\n\n" +
		"3. Files and Code Sections:\n" +
		"- [File Name]\n" +
		"  - [Why important]\n" +
		"  - [Code snippet]\n\n" +
		"4. Errors and fixes:\n" +
		"- [...]\n\n" +
		"5. Problem Solving:\n" +
		"[...]\n\n" +
		"6. All user messages:\n" +
		"- [...]\n\n" +
		"7. Pending Tasks:\n" +
		"- [...]\n\n" +
		"8. Current Work:\n" +
		"[...]\n\n" +
		"9. Optional Next Step:\n" +
		"[...]\n" +
		"</summary>"
)

const (
	CheckTokenThreshold       int = 100000
	summaryMinTokensThreshold int = 16000
	maxSummaryWords           int = 2000
	EventThreshold            int = 20
)

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
