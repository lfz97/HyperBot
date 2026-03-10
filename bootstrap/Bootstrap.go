package bootstrap

import (
	"context"
	"github.com/google/uuid"
	"os/exec"
	"trpcagent/handler"
)

func Boot(ctx context.Context, AgentName string) {

	for {
		sessionID := uuid.New().String()
		userID := uuid.New().String()
		requestID := uuid.New().String()
		runner, ToolExitCommands := Init(AgentName)
		EndReason_p, _ := handler.AgentRunIteratively(ctx, runner, sessionID, userID, requestID)
		if EndReason_p.Code == 0 {
			//用户主动结束对话，退出程序
			exitToolsWhileEnding(ToolExitCommands)
			break
		} else if EndReason_p.Code == 1 {
			exitToolsWhileEnding(ToolExitCommands)
			continue
		}
	}

}

// 结束时回收工具进程，执行退出命令
func exitToolsWhileEnding(ToolExitCommands []string) {
	for _, cmd := range ToolExitCommands {
		if cmd != "" {
			c := exec.Command(cmd)
			c.CombinedOutput()
		}
	}
}
