package agent

import (
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
)

func OpenaiAgent(agentName string, Model string, BaseUrl string, APIkey string, opts []llmagent.Option) *llmagent.LLMAgent {

	agent_p := ConfigBaseAgent(agentName, Model, BaseUrl, APIkey, "openai", opts)
	return agent_p
}
