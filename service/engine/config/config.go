package config

import (
	"fmt"
	"gopkg.in/yaml.v2"
	"os"
)

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
type User struct {
	UserID string `yaml:"userid"`
}

type Config struct {
	HttpMcp  []HttpMCP  `yaml:"http_mcp"`
	StdinMcp []StdinMCP `yaml:"stdin_mcp"`
	Model    Model      `yaml:"model"`
	User     User       `yaml:"user"`
}

func LoadConfig(path string) (*Config, error) {
	YamlConfig := Config{}
	yamlFile, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}
	err = yaml.Unmarshal(yamlFile, &YamlConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
	}
	if YamlConfig.Model.MaxTokens == 0 {
		YamlConfig.Model.MaxTokens = 12800
	}
	return &YamlConfig, nil
}

// ReasoningEffortPtr 未配置（空串）返回 nil：框架对 nil 不下发 reasoning_effort 字段。
func (m *Model) ReasoningEffortPtr() *string {
	if m.ReasoningEffort == "" {
		return nil
	}
	return &m.ReasoningEffort
}
