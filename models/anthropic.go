package models

import (
	"trpc.group/trpc-go/trpc-agent-go/model/anthropic"
	"trpcagent/config"
)

// 兼容anthropic模型的接口，方便后续替换模型提供商
func Anthropic(BaseUrl string, APIkey string) *anthropic.Model {
	modelInstance := anthropic.New(
		config.MinimaxModel,
		anthropic.WithBaseURL(BaseUrl),
		anthropic.WithAPIKey(APIkey),
	)
	return modelInstance
}
