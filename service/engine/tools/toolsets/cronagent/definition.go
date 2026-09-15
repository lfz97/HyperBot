package cronagent

import (
	"context"

	"HyperBot/service/engine/config"

	"trpc.group/trpc-go/trpc-agent-go/tool"
)

type CronAgentToolSet struct {
	name string
	mgr  *manager
}

func (l *CronAgentToolSet) Tools(context.Context) []tool.Tool {
	return getTools(l.mgr)
}

// Close 停掉所有 schedule 的调度 goroutine 并取消在跑的那一轮。
// 刻意不把 mgr 置 nil：Tools() 可能在本方法之后仍被框架调用，置 nil 会让
// 后续工具调用在 nil manager 上 panic。Clear 也不落盘，存档保持原样。
func (l *CronAgentToolSet) Close() error {
	l.mgr.Clear()
	return nil
}

func (l *CronAgentToolSet) Name() string {
	return l.name
}

// LoadError 返回启动时恢复存档的非致命错误。存档已被挪到 .fix<时间戳>，
// 工具集本身可用（空集合），调用方应当把这条错误展示给用户而不是终止启动。
func (l *CronAgentToolSet) LoadError() error {
	return l.mgr.loadErr
}

func CronAgent(agentname string, cfg *config.Config, systemprompt string, SkillFolderPath string, configFolderPath string) (*CronAgentToolSet, error) {
	m, err := NewManager(agentname, cfg, systemprompt, SkillFolderPath, configFolderPath)
	if err != nil {
		return nil, err
	}
	return &CronAgentToolSet{
		name: cronAgentToolSetName,
		mgr:  m,
	}, nil
}
