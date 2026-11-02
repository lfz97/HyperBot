package config

type Model struct {
	Model                       string `yaml:"model"`
	BaseURL                     string `yaml:"baseurl"`
	APIKey                      string `yaml:"apikey"`
	APIType                     string `yaml:"apitype"`                     // "openai" or "anthropic"
	AnthropicAuthHeaderTransfer bool   `yaml:"anthropicAuthHeaderTransfer"` // true: Authorization: Bearer 认证；false: X-Api-Key 认证
	Stream                      bool   `yaml:"stream"`
	ContextWindow               int    `yaml:"contextwindow"`
	MaxTokens                   int    `yaml:"maxtokens"`        // 默认 12800
	ShowReasoning               bool   `yaml:"show_reasoning"`   // 是否显示推理/思考内容
	ReasoningEffort             string `yaml:"reasoning_effort"` // 思考强度档位（low/medium/high/max），留空不下发该参数
	HttpTimeout                 int    `yaml:"httptimeout"`
}
