package config

type MCPtype string

const (
	SSE             MCPtype = "sse"
	Streamable_HTTP MCPtype = "streamable_http"
)

type HttpMCP struct {
	Enabled     bool
	Name        string // ToolSet 名称，决定工具前缀 {Name}_{toolName}，多个 MCP server 时必须唯一
	Type        MCPtype
	Endpoint    string
	Headers     map[string]string
	Description string
}

type StdinMCP struct {
	Enabled     bool
	Name        string // ToolSet 名称，决定工具前缀 {Name}_{toolName}，多个 MCP server 时必须唯一
	Command     string
	Args        []string
	Description string
}
