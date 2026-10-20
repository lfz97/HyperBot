package localexec

import (
	"context"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// LocalExecToolSet 的 name 为空串（见 LocalExec）：框架的 NamedToolSet 对空名
// 工具集不加前缀，工具以 mapper.go 里登记的名字原样暴露给模型。
// cronagent/MCP 工具集保留前缀，不会与本工具集裸名冲突。
type LocalExecToolSet struct {
	name string
	mgr  *Manager
}

func (l *LocalExecToolSet) Tools(context.Context) []tool.Tool {
	return getTools(l.mgr)
}

func (l *LocalExecToolSet) Close() error {
	l.mgr.mu.Lock()
	defer l.mgr.mu.Unlock()
	l.mgr.jobs = map[string]*Job{}
	return nil
}

func (l *LocalExecToolSet) Name() string {
	return l.name
}

func LocalExec() tool.ToolSet {
	return &LocalExecToolSet{
		name: "",
		mgr:  &Manager{jobs: map[string]*Job{}},
	}
}
