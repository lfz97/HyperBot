package agent

import (
	"HyperBot/models"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/codeexecutor/local"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

func ConfigBaseAgent(agentName string, systemPrompt string, genConfig model.GenerationConfig, tools []tool.Tool, toolsets []tool.ToolSet, Model string, BaseUrl string, APIkey string, ApiType string) *llmagent.LLMAgent {

	repo, _ := skill.NewFSRepository("./skills")
	exec := local.New()

	opts := []llmagent.Option{
		llmagent.WithGenerationConfig(genConfig),
		llmagent.WithTools(tools),
		llmagent.WithInstruction(systemPrompt), //系统提示词
		llmagent.WithToolSets(toolsets),
		llmagent.WithRefreshToolSetsOnRun(true),
		llmagent.WithSkills(repo),
		llmagent.WithCodeExecutor(exec),
		llmagent.WithEnableCodeExecutionResponseProcessor(false),
	}

	if ApiType == "openai" {
		OpenaiModel_p := models.Openai(Model, BaseUrl, APIkey)
		opts = append(opts, llmagent.WithModel(OpenaiModel_p))
	} else if ApiType == "anthropic" {
		AnthropicModel_p := models.Anthropic(Model, BaseUrl, APIkey)
		opts = append(opts, llmagent.WithModel(AnthropicModel_p))
	}

	agent_p := llmagent.New(agentName,
		opts...,
	)
	return agent_p
}
