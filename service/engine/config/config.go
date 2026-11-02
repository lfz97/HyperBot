package config

import (
	"fmt"
	"gopkg.in/yaml.v2"
	"os"
)

type User struct {
	UserID string `yaml:"userid"`
}

type Config struct {
	HttpMcp       []HttpMCP     `yaml:"http_mcp"`
	StdinMcp      []StdinMCP    `yaml:"stdin_mcp"`
	Model         Model         `yaml:"model"`
	User          User          `yaml:"user"`
	Observability Observability `yaml:"observability"`
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
