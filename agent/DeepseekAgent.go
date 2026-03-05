package agent

import (
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpcagent/models"
)

func DeepseekAgent(agentName string, systemPrompt string, genConfig model.GenerationConfig, tools []tool.Tool, toolsets []tool.ToolSet) *llmagent.LLMAgent {
	deepseekModel_p := models.Deepseek()
	agent_p := llmagent.New(agentName,
		llmagent.WithModel(deepseekModel_p),
		llmagent.WithGenerationConfig(genConfig),
		llmagent.WithTools(tools),
		llmagent.WithInstruction(systemPrompt), //系统提示词
		llmagent.WithToolSets(toolsets),
	)
	return agent_p
}
