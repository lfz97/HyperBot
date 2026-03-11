package models

import (
	"strings"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
)

// 兼容openai模型的接口，方便后续替换模型提供商
func Openai(Model string, BaseUrl string, APIkey string) *openai.Model {
	opts := []openai.Option{
		openai.WithBaseURL(BaseUrl),
		openai.WithAPIKey(APIkey),
	}
	if strings.Contains(Model, "deepseek") == true {
		opts = append(opts, openai.WithVariant(openai.VariantDeepSeek))
	}
	modelInstance := openai.New(
		Model,
		opts...,
	)
	return modelInstance
}
