package agent

import (
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpcagent/config"
	"trpcagent/models"
)

func MinimaxAgent(agentName string, systemPrompt string, genConfig model.GenerationConfig, tools []tool.Tool, toolsets []tool.ToolSet) *llmagent.LLMAgent {
	MinimaxModel_p := models.Anthropic(config.MinimaxBaseURL, config.MinimaxAPIKey)
	agent_p := llmagent.New(agentName,
		llmagent.WithModel(MinimaxModel_p),
		llmagent.WithGenerationConfig(genConfig),
		llmagent.WithTools(tools),
		llmagent.WithInstruction(systemPrompt), //系统提示词
		llmagent.WithToolSets(toolsets),
	)
	return agent_p
}
