package toolsets

import (
	"time"
	"trpc.group/trpc-go/trpc-agent-go/tool/mcp"
)

func ChromeMCP(mcptype string, command string, args []string) *mcp.ToolSet {

	mcpToolSet := mcp.NewMCPToolSet(
		mcp.ConnectionConfig{
			Transport: mcptype, // 注意：使用完整名称
			Command:   command,
			Args:      args,
			Timeout:   10 * time.Second,
		},
		mcp.WithSessionReconnect(3),
	)
	return mcpToolSet
}
