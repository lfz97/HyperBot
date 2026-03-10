package agent

import (
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpcagent/models"
)

func AnthropicAgent(agentName string, systemPrompt string, genConfig model.GenerationConfig, tools []tool.Tool, toolsets []tool.ToolSet, Model string, BaseUrl string, APIkey string) *llmagent.LLMAgent {
	AnthropicModel_p := models.Anthropic(Model, BaseUrl, APIkey)
	agent_p := llmagent.New(agentName,
		llmagent.WithModel(AnthropicModel_p),
		llmagent.WithGenerationConfig(genConfig),
		llmagent.WithTools(tools),
		llmagent.WithInstruction(systemPrompt), //系统提示词
		llmagent.WithToolSets(toolsets),
	)
	return agent_p
}
