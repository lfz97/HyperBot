package toolsets

import (
	"fmt"
	"time"
	"trpc.group/trpc-go/trpc-agent-go/tool/mcp"
)

func BochaMCP(mcptype string, url string, apikey string) *mcp.ToolSet {

	mcpToolSet := mcp.NewMCPToolSet(
		mcp.ConnectionConfig{
			Transport: mcptype, // 注意：使用完整名称
			ServerURL: url,
			Timeout:   10 * time.Second,
			Headers: map[string]string{
				"Authorization": fmt.Sprintf("Bearer %s", apikey),
			},
		},
		mcp.WithSessionReconnect(3),
	)
	return mcpToolSet
}
