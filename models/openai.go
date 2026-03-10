package models

import (
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
)

// 兼容openai模型的接口，方便后续替换模型提供商
func Openai(Model string, BaseUrl string, APIkey string) *openai.Model {
	modelInstance := openai.New(
		Model,
		openai.WithBaseURL(BaseUrl),
		openai.WithAPIKey(APIkey),
	)
	return modelInstance
}
