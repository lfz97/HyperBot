package session

import (
	"HyperBot/config"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/anthropic"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/session/summary"
)

// claude code 模式的摘要生成提示词
const (
	systemSummarizerPrompt string = "CRITICAL: Respond with TEXT ONLY. Do NOT call any tools.\n" +
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

func NewSummarizer(m config.Model) summary.SessionSummarizer {

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
	// ── 创建 summarizer，92% 阈值 ───────────────
	sum := summary.NewSummarizer(
		summarizerModel,
		summary.WithContextThreshold(
			summary.WithContextThresholdRatio(0.92),                     // 92% 触发
			summary.WithContextThresholdMinTokens(2000),                 // 设置距离触发的最小剩余上下文长度，单位为 token
			summary.WithContextThresholdFallbackWindow(m.ContextWindow), // 设置fallback 上下文大小，如果框架中没有内置你使用的模型的上下文窗口大小参数，那么上下文窗口会fallback到这个值
		),
		summary.WithMaxSummaryWords(2000),                //设置摘要的最大长度，单位为词，默认为1000词，可以根据需要调整
		summary.WithSystemPrompt(systemSummarizerPrompt), //设置系统提示词，指导模型如何进行摘要，默认为空，可以根据需要自定义
		summary.WithPrompt(userSummarizerPrompt),         //设置用户提示词，指导模型如何根据会话内容生成摘要，默认为空，可以根据需要自定义
	)

	return sum

}
