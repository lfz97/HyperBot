package localexec

import (
	"context"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

type LocalExecToolSet struct {
}

func (l *LocalExecToolSet) Tools(context.Context) []tool.Tool {
	tools := GetTools()
	return tools
}

func (l *LocalExecToolSet) Close() error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.jobs = map[string]*Job{} // 清空所有任务
	return nil
}

func (l *LocalExecToolSet) Name() string {
	return "LocalExec"
}

func LocalExec() *LocalExecToolSet {
	return &LocalExecToolSet{}
}
