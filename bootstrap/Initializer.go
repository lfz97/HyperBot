package bootstrap

import (
	"fmt"
	"gopkg.in/yaml.v2"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpcagent/agent"
	"trpcagent/config"
	"trpcagent/toolsets"
	"trpcagent/toolsets/localexec"
)

func Init(AgentName string) runner.Runner {
	configSystemPrompt()
	exist, err := checkConfig()
	if err != nil {
		fmt.Println("检查配置文件错误：", err)
		fmt.Println("按回车键退出...")
		fmt.Scanln()
		os.Exit(0)
	}
	if exist == false && err == nil {
		fmt.Println("已创建默认配置文件，请前往修改后重新启动程序,按回车键退出...")
		fmt.Scanln()
		os.Exit(0)
	}
	config_p, err := loadConfig()
	if err != nil {
		fmt.Println("加载配置文件错误：", err)
		fmt.Println("按回车键退出...")
	}
	Tools, Toolsets, Model := parseConfig(*config_p)
	runner := initAgent(Tools, Toolsets, Model, AgentName)
	return runner
}

// 配置系统提示词，替换其中的占位符
func configSystemPrompt() {
	os_type := runtime.GOOS
	config.SystemPrompt = strings.ReplaceAll(config.SystemPrompt, "{{OSTYPE}}", os_type)
	if os_type == "windows" {
		diarypath := os.Getenv("USERPROFILE")
		config.SystemPrompt = strings.ReplaceAll(config.SystemPrompt, "{{DIARYPATH}}", diarypath+"\\Diary")
	} else if os_type == "linux" || os_type == "darwin" {
		diarypath := os.Getenv("HOME")
		config.SystemPrompt = strings.ReplaceAll(config.SystemPrompt, "{{DIARYPATH}}", diarypath+"/Diary")
	} else {
		config.SystemPrompt = strings.ReplaceAll(config.SystemPrompt, "{{DIARYPATH}}", "Diary")
	}
}

// 检查配置文件是否存在，不存在则创建一个默认的配置文件
func checkConfig() (bool, error) {
	configName := "config.yaml"
	// 获取当前可执行文件的目录路径（不包含程序名）
	exePath, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("获取可执行文件路径错误：%v", err)
	}
	exeDir := filepath.Dir(exePath)
	configPath := filepath.Join(exeDir, configName)
	// TODO: 读取并解析 configPath 中的 YAML 配置
	_, err = os.Stat(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件不存在，创建一个默认的 config.yaml
			fd, err := os.OpenFile(configPath, os.O_RDWR|os.O_CREATE, 0644)
			if err != nil {
				return false, fmt.Errorf("创建默认配置文件错误：%v", err)
			}
			_, err = fd.WriteString(config.Template)
			if err != nil {
				return false, fmt.Errorf("写入默认配置文件错误：%v", err)
			}
			return false, nil

		}
	}
	return true, nil
}

func loadConfig() (*config.Config, error) {
	YamlConfig := config.Config{}

	configName := "config.yaml"
	// 获取当前可执行文件的目录路径（不包含程序名）
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	configPath := filepath.Join(exeDir, configName)
	yamlFile, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件错误：%v", err)
	}
	err = yaml.Unmarshal(yamlFile, &YamlConfig)
	if err != nil {
		return nil, fmt.Errorf("解析配置文件错误：%v", err)
	}
	return &YamlConfig, nil
}

func parseConfig(RunningConfig config.Config) ([]tool.Tool, []tool.ToolSet, config.Model) {
	Tools := []tool.Tool{}
	Toolsets := []tool.ToolSet{}
	/*以下是toolsets类型的配置读取*/
	if RunningConfig.BochaMCP.Enabled == true {
		Toolsets = append(Toolsets, toolsets.BochaMCP(RunningConfig.BochaMCP.MCPtype, RunningConfig.BochaMCP.MCPEndpoint, RunningConfig.BochaMCP.APIKey))
	}
	if RunningConfig.ChromeMCP.Enabled == true {
		Toolsets = append(Toolsets, toolsets.ChromeMCP(RunningConfig.ChromeMCP.MCPtype, RunningConfig.ChromeMCP.MCPEndpoint))
	}
	if RunningConfig.MCPExec.Enabled == true {
		Toolsets = append(Toolsets, toolsets.ShellMCP(RunningConfig.MCPExec.MCPtype, RunningConfig.MCPExec.MCPEndpoint))
	}
	Toolsets = append(Toolsets, localexec.LocalExec()) //localexec 必须启用
	return Tools, Toolsets, RunningConfig.Model
}

func initAgent(Tools []tool.Tool, Toolsets []tool.ToolSet, Model config.Model, AgentName string) runner.Runner {
	var Runner runner.Runner
	if Model.APIType == "openai" {
		Agent_p := agent.OpenaiAgent(
			AgentName,
			config.SystemPrompt,
			model.GenerationConfig{
				Stream: true,
			},
			Tools,
			Toolsets,
			Model.Model,
			Model.BaseURL,
			Model.APIKey,
		)
		Runner = runner.NewRunner(AgentName, Agent_p)
	} else if Model.APIType == "anthropic" {
		Agent_p := agent.AnthropicAgent(
			AgentName,
			config.SystemPrompt,
			model.GenerationConfig{
				Stream: true,
			},
			Tools,
			Toolsets,
			Model.Model,
			Model.BaseURL,
			Model.APIKey,
		)
		Runner = runner.NewRunner(AgentName, Agent_p)
	} else {
		fmt.Println("不支持的API类型，请检查配置文件中的Model.APIType字段，按回车键退出...")
		fmt.Scanln()
		os.Exit(0)
	}

	return Runner
}
