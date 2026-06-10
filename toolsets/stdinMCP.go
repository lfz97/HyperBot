package toolsets

import (
	"HyperBot/config"
	"time"
	"trpc.group/trpc-go/trpc-agent-go/tool/mcp"
)

func StdinMCP(config config.StdinMCP) *mcp.ToolSet {

	mcpToolSet := mcp.NewMCPToolSet(
		mcp.ConnectionConfig{
			Transport:   "stdio",
			Command:     config.Command,
			Args:        config.Args,
			Timeout:     10 * time.Second,
			Description: config.Description,
		},
		mcp.WithSessionReconnect(3),
	)
	return mcpToolSet
}
