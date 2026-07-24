package agent

import (
	"HyperBot/config"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
)

func AnthropicAgent(agentName string, m config.Model, opts []llmagent.Option) *llmagent.LLMAgent {

	agent_p := ConfigBaseAgent(agentName, m, opts)

	return agent_p
}
