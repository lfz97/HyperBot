package config

type MCPtype string

const (
	SSE             MCPtype = "sse"
	Streamable_HTTP MCPtype = "streamable_http"
)

type HttpMCP struct {
	Enabled     bool
	Type        MCPtype
	Endpoint    string
	Headers     map[string]string
	Description string
}

type StdinMCP struct {
	Enabled     bool
	Command     string
	Args        []string
	Description string
}
