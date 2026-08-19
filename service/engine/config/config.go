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
	AnthropicAuthHeaderTransfer bool   `yaml:"anthropicAuthHeaderTransfer"` //如果true，那么通过Authorization: Bearer认证，如果为false，通过X-Api-Key认证
	Stream                      bool   `yaml:"stream"`                      //true or false
	ContextWindow               int    `yaml:"contextwindow"`               // 上下文窗口大小
	MaxTokens                   int    `yaml:"maxtokens"`                   // 每次请求的最大生成 token 数，默认 12800
	ShowReasoning               bool   `yaml:"show_reasoning"`              // 是否显示推理/思考内容
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
		return nil, fmt.Errorf("读取配置文件错误：%v", err)
	}
	err = yaml.Unmarshal(yamlFile, &YamlConfig)
	if err != nil {
		return nil, fmt.Errorf("解析配置文件错误：%v", err)
	}
	if YamlConfig.Model.MaxTokens == 0 { // maxtokens 未配置时使用默认值 12800
		YamlConfig.Model.MaxTokens = 12800
	}
	return &YamlConfig, nil
}
