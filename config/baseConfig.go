package config

type Model struct {
	Model   string
	BaseURL string
	APIKey  string
	APIType string // "openai" or "anthropic"
	Stream  bool   //true or false
}
type User struct {
	UserID string
}

type Config struct {
	Mcp      []MCP      `yaml:"mcp"`
	StdinMcp []StdinMCP `yaml:"stdin_mcp"`
	Model    Model      `yaml:"model"`
	User     User       `yaml:"user"`
}
