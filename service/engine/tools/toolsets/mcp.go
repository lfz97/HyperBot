package toolsets

import (
	"HyperBot/service/engine/config"
	"time"
	"trpc.group/trpc-go/trpc-agent-go/tool/mcp"
)

// defaultMCPTimeout 单次 MCP 工具调用超时的默认值。旧值 10s 对任何做实际工作的
// server 都太苛刻（长分析型调用必然 context deadline exceeded），提到 2 分钟；
// 个别 server 用 yaml 的 timeout 字段覆盖（单位/秒，<=0 落到这里）。
const defaultMCPTimeout = 2 * time.Minute

// mcpTimeout 把 yaml 里的秒数换算成 Duration，<=0 视为未配置。
func mcpTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		return defaultMCPTimeout
	}
	return time.Duration(seconds) * time.Second
}

func HttpMCP(config config.HttpMCP) *mcp.ToolSet {

	mcpToolSet := mcp.NewMCPToolSet(
		mcp.ConnectionConfig{
			Transport:   string(config.Type),
			ServerURL:   config.Endpoint,
			Timeout:     mcpTimeout(config.Timeout),
			Headers:     config.Headers,
			Description: config.Description,
		},
		mcp.WithName(config.Name), //决定工具前缀 {Name}_{toolName}，多server必须唯一
		mcp.WithSessionReconnect(3),
	)
	return mcpToolSet
}

func StdinMCP(config config.StdinMCP) *mcp.ToolSet {

	mcpToolSet := mcp.NewMCPToolSet(
		mcp.ConnectionConfig{
			Transport:   "stdio",
			Command:     config.Command,
			Args:        config.Args,
			Timeout:     mcpTimeout(config.Timeout),
			Description: config.Description,
		},
		mcp.WithName(config.Name), //决定工具前缀 {Name}_{toolName}，多server必须唯一
		mcp.WithSessionReconnect(3),
	)
	return mcpToolSet
}
