package main

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/model"

	"trpcagent/agent"
	//"trpcagent/functionTools"
	"trpcagent/handler"
	"trpcagent/toolsets"
	"trpcagent/utils"

	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

func main() {
	ctx := context.Background()

	deepseekAgent_p := agent.DeepseekAgent(
		"渗透王",
		SystemPrompt,
		model.GenerationConfig{
			Stream: true,
		},
		[]tool.Tool{
			//functionTools.CreateCalculatorTool(),
			//functionTools.CreateGetWeatherTool(),
			//functionTools.GetBookSearchTool(),
		}, []tool.ToolSet{
			toolsets.BochaMCP(),
			toolsets.ShellMCP(),
		})
	runnerds_p := runner.NewRunner("图书管理员", deepseekAgent_p)

	//dshistory := handler.AgentRunIteratively(ctx, runnerds_p, "ABC", "001")
	history1 := handler.AgentRunOnce(ctx, runnerds_p, "ABC", "001", "查一下2026.3.6上海天气")
	history2 := handler.AgentRunOnce(ctx, runnerds_p, "ABC", "001", "刚刚我们聊了什么？")
	historyAll := append(history1, history2...)

	/*=============================测试区域======================================*/
	err := utils.SaveHistoryToJsonFile(historyAll, "history2.json")
	if err != nil {
		panic(err)
	}

}
