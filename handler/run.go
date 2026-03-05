package handler

import (
	"context"
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/agent"

	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpcagent/myutils"
)

const (
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorReset  = "\033[0m"
)

func AgentRunOnce(Ctx context.Context, r runner.Runner, sessionID string, userID string, msg string) [][]model.Choice {

	history := [][]model.Choice{}
	eventChan, err := r.Run(Ctx, userID, sessionID, model.NewUserMessage(msg), agent.WithRequestID("request-ID"))
	if err != nil {
		panic(err)
	}

	// 处理流式输出
	startReasoning := false
	for event := range eventChan {

		if event.Error != nil {
			fmt.Printf("错误: %s\n", event.Error.Message)
			continue
		}

		if len((*(*event).Response).Choices) > 0 {
			// 将每次输出的Choice追加到历史记录中
			history = append(history, (*(*event).Response).Choices)

			Choice := (*(*event).Response).Choices[0]
			/*------------------处理流式的响应*---------------------------------------------------------------------------*/

			//处理思考信息
			if Choice.Delta.ReasoningContent != "" && !startReasoning {

				fmt.Printf("%s%s%s", colorGreen, "\n开始推理...\n", colorReset)
				startReasoning = true

			} else if Choice.Delta.ReasoningContent != "" && startReasoning {

				fmt.Printf("%s%s%s", colorYellow, Choice.Delta.ReasoningContent, colorReset)

			} else if Choice.Delta.ReasoningContent == "" && startReasoning {
				startReasoning = false

				fmt.Printf("%s%s%s", colorGreen, "\n推理结束，开始输出结果...\n", colorReset)

			}

			//处理正文

			if Choice.Delta.Content != "" {
				fmt.Printf("%s", Choice.Delta.Content)
			}

			/*------------------处理非流式的响应*---------------------------------------------------------------------------*/
			//处理思考信息
			if Choice.Message.ReasoningContent != "" {

				fmt.Printf("%s%s%s", colorGreen, "\n开始推理...\n", colorReset)
				fmt.Printf("%s%s%s", colorYellow, Choice.Message.ReasoningContent, colorReset)
				fmt.Printf("%s%s%s", colorGreen, "\n推理结束，开始输出结果...\n", colorReset)

			}

			//处理正文
			if Choice.Message.Content != "" {
				fmt.Printf("%s", Choice.Message.Content)

			}
			/*------------------此处统一处理工具信息*---------------------------------------------------------------------------*/

			//流式的
			if len(Choice.Delta.ToolCalls) != 0 {
				for _, toolCall := range Choice.Delta.ToolCalls {
					fmt.Printf("%sToolCall: %s%s\n", colorBlue, toolCall.Function.Name, colorReset)
					fmt.Printf("%sToolCall Arguments: %s%s\n", colorBlue, string(toolCall.Function.Arguments), colorReset)
				}
			}

			//非流式的
			if len(Choice.Message.ToolCalls) != 0 {
				for _, toolCall := range Choice.Message.ToolCalls {
					fmt.Printf("%sToolCall: %s%s\n", colorBlue, toolCall.Function.Name, colorReset)
					fmt.Printf("%sToolCall Arguments: %s%s\n", colorBlue, string(toolCall.Function.Arguments), colorReset)
				}
			}

		}
		// event.IsRunnerCompletion()判断是否完成输出
		if event.IsRunnerCompletion() {
			break
		}

	}
	return history
}

func AgentRunIteratively(Ctx context.Context, r runner.Runner, sessionID string, userID string) [][]model.Choice {
	historyAll := [][]model.Choice{}
	fmt.Println(colorBlue + "\n新对话已开始" + colorReset)

	for {
		userPrompt, err := myutils.StdinInput(colorBlue + "\nUser(欲退出请输入" + colorGreen + "`/exit`):" + colorReset)
		if err != nil {
			fmt.Printf(colorRed+"读取输入错误: %v\n"+colorReset, err)
			continue
		}
		if userPrompt == "/exit" {
			fmt.Println(colorBlue + "对话已结束" + colorReset)
			break

		}

		rs := AgentRunOnce(Ctx, r, sessionID, userID, userPrompt)
		historyAll = append(historyAll, rs...)
	}
	return historyAll
}
