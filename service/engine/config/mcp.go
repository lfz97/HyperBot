package config

type MCPtype string

const (
	SSE             MCPtype = "sse"
	Streamable_HTTP MCPtype = "streamable_http"
)

type HttpMCP struct {
	Enabled     bool
	Name        string // 决定工具前缀 {Name}_{toolName}，多个 MCP server 间必须唯一
	Type        MCPtype
	Endpoint    string
	Headers     map[string]string
	Description string
	// Timeout 单次工具调用的超时，单位/秒（沿用 model.httptimeout 惯例）。
	// 长分析型 server（如逆向工具）务必调大；<=0 用默认值 defaultMCPTimeout。
	Timeout int
}

type StdinMCP struct {
	Enabled     bool
	Name        string // 决定工具前缀 {Name}_{toolName}，多个 MCP server 间必须唯一
	Command     string
	Args        []string
	Description string
	// Timeout 同 HttpMCP.Timeout
	Timeout int
}
