package config

type BochaMCP struct {
	Enabled     bool
	APIKey      string
	MCPtype     string
	MCPEndpoint string
}

type MCPExec struct {
	Enabled     bool
	MCPtype     string
	MCPEndpoint string
}

type ChromeMCP struct {
	Enabled     bool
	MCPtype     string
	Command     string
	Args        []string
	ExitCommand string
}

type Model struct {
	Model   string
	BaseURL string
	APIKey  string
	APIType string // "openai" or "anthropic"
}

type Config struct {
	BochaMCP  BochaMCP  `yaml:"bochamcp"`
	MCPExec   MCPExec   `yaml:"mcpexec"`
	ChromeMCP ChromeMCP `yaml:"chromemcp"`
	Model     Model     `yaml:"model"`
}
