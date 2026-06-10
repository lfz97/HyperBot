package toolsets

import (
	"HyperBot/config"
	"time"
	"trpc.group/trpc-go/trpc-agent-go/tool/mcp"
)

func HttpMCP(config config.HttpMCP) *mcp.ToolSet {

	mcpToolSet := mcp.NewMCPToolSet(
		mcp.ConnectionConfig{
			Transport:   string(config.Type),
			ServerURL:   config.Endpoint,
			Timeout:     10 * time.Second,
			Headers:     config.Headers,
			Description: config.Description,
		},
		mcp.WithSessionReconnect(3),
	)
	return mcpToolSet
}
