package toolsets

import (
	"fmt"
	"time"
	"trpc.group/trpc-go/trpc-agent-go/tool/mcp"
	"trpcagent/config"
)

func BochaMCP() *mcp.ToolSet {

	mcpToolSet := mcp.NewMCPToolSet(
		mcp.ConnectionConfig{
			Transport: "streamable_http", // 注意：使用完整名称
			ServerURL: config.BochaMCPEndpoint,
			Timeout:   10 * time.Second,
			Headers: map[string]string{
				"Authorization": fmt.Sprintf("Bearer %s", config.BochaAPIKey),
			},
		},
		mcp.WithSessionReconnect(3),
	)
	return mcpToolSet
}
