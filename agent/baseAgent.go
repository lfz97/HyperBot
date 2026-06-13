package agent

import (
	"HyperBot/models"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
)

func ConfigBaseAgent(agentName string, Model string, BaseUrl string, APIkey string, ApiType string, opts []llmagent.Option) *llmagent.LLMAgent {

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
