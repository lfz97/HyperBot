package agent

import (
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpcagent/models"
)

func OpenaiAgent(agentName string, systemPrompt string, genConfig model.GenerationConfig, tools []tool.Tool, toolsets []tool.ToolSet, Model string, BaseUrl string, APIkey string) *llmagent.LLMAgent {
	OpenaiModel_p := models.Openai(Model, BaseUrl, APIkey)
	agent_p := llmagent.New(agentName,
		llmagent.WithModel(OpenaiModel_p),
		llmagent.WithGenerationConfig(genConfig),
		llmagent.WithTools(tools),
		llmagent.WithInstruction(systemPrompt), //系统提示词
		llmagent.WithToolSets(toolsets),
	)
	return agent_p
}
