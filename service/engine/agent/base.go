package agent

import (
	"HyperBot/service/engine/config"
	"HyperBot/service/engine/models"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
)

func ConfigBaseAgent(agentName string, m config.Model, opts []llmagent.Option) *llmagent.LLMAgent {

	if m.APIType == "openai" {
		OpenaiModel_p := models.Openai(m.Model, m.BaseURL, m.APIKey)
		opts = append(opts, llmagent.WithModel(OpenaiModel_p))
	} else if m.APIType == "anthropic" {
		AnthropicModel_p := models.Anthropic(m)
		opts = append(opts, llmagent.WithModel(AnthropicModel_p))
	}

	agent_p := llmagent.New(agentName,
		opts...,
	)
	return agent_p

}
