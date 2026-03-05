package toolsets

import (
	"time"
	"trpc.group/trpc-go/trpc-agent-go/tool/mcp"
	"trpcagent/config"
)

func ShellMCP() *mcp.ToolSet {

	mcpToolSet := mcp.NewMCPToolSet(
		mcp.ConnectionConfig{
			Transport: "streamable_http", // 注意：使用完整名称
			ServerURL: config.ShellMCPEndpoint,
			Timeout:   10 * time.Second,
		},
		mcp.WithSessionReconnect(3),
	)
	return mcpToolSet
}
