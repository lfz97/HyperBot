package handler

import (
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

const (
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorReset  = "\033[0m"
)

type EndInfo struct {
	Code           ExitCode
	Reason         string
	RecoverMessage []model.Message //非正常结束时的历史消息，用于恢复对话
}
type ExitCode int

const (
	ExitCodeNormal ExitCode = 0
	ExitCodeNew    ExitCode = 1
	ExitCodeInt    ExitCode = 2
	ExitCodeError  ExitCode = 3
	ExitCodeExit   ExitCode = 4
)

type AgentRunner struct {
	Runner runner.Runner
	Stream bool
}
