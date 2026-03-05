package models

import (
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpcagent/config"
)

func Deepseek() *openai.Model {
	modelInstance := openai.New(
		config.DeepSeekModel,
		openai.WithBaseURL(config.DeepSeekBaseURL),
		openai.WithAPIKey(config.DeepSeekAPIKey),
	)
	return modelInstance
}
