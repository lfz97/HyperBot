package models

import (
	"trpc.group/trpc-go/trpc-agent-go/model/anthropic"
	"trpcagent/config"
)

/*
func Minimax() *openai.Model {
	modelInstance := openai.New(
		config.MinimaxModel,
		openai.WithBaseURL(config.MinimaxBaseURL),
		openai.WithAPIKey(config.MinimaxAPIKey),
	)
	return modelInstance
}
*/

func Minimax() *anthropic.Model {
	modelInstance := anthropic.New(
		config.MinimaxModel,
		anthropic.WithBaseURL(config.MinimaxBaseURL),
		anthropic.WithAPIKey(config.MinimaxAPIKey),
	)
	return modelInstance
}
