package main

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/model"

	"trpcagent/agent"
	//"trpcagent/functionTools"
	"trpcagent/handler"
	"trpcagent/toolsets"
	"trpcagent/toolsets/localexec"

	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// 程序启动前的环境检查和初始化
func checkEnv() {
	// 替换SystemPrompt中的系统相关占位符
	sysenvCheck()
}

func run() {
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
			localexec.LocalExec(),
		})
	runnerds_p := runner.NewRunner("图书管理员", deepseekAgent_p)

	handler.AgentRunIteratively(ctx, runnerds_p, "ABC", "001", "request-001")
}

func main() {
	checkEnv()
	run()

}
