package main

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/model"

	"trpcagent/agent"
	//"trpcagent/functionTools"
	"trpcagent/handler"
	"trpcagent/toolsets"
	"trpcagent/utils"

	"encoding/json"

	"os"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

func SaveHistoryToJsonFile(history [][]model.Choice, path string) error {
	fd, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	data, err := json.Marshal(history)
	if err != nil {
		return err
	}
	fd.Write(data)
	return nil
}

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

	dshistory := handler.AgentRunIteratively(ctx, runnerds_p, "ABC", "001")

	/*
		MinimaxAgent_p := agent.MinimaxAgent(
			"图书管理员",
			"你是图书管理员",
			model.GenerationConfig{
				Stream: true,
			},
			[]tool.Tool{
				functionTools.CreateCalculatorTool(),
				functionTools.CreateGetWeatherTool(),
				functionTools.GetBookSearchTool(),
			},
			[]tool.ToolSet{
				toolsets.BochaMCP(),
			})
		runnermx_p := runner.NewRunner("图书管理员", MinimaxAgent_p)
		mxhistory := handler.AgentRunIteratively(ctx, runnermx_p, "ABC", "001")
	*/
	/*=============================测试区域======================================*/
	err := utils.SaveHistoryToJsonFile(dshistory, "dshistory.json")
	if err != nil {
		panic(err)
	}

}
