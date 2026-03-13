package bootstrap

import (
	"HyperBot/agent"
	"HyperBot/config"
	"HyperBot/handler"
	"HyperBot/toolsets"
	"HyperBot/toolsets/localexec"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v2"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

func Init(AgentName string) handler.AgentRunner {
	ExeDirPath, err := getExeDirPath()
	if err != nil {
		fmt.Println("获取可执行文件目录错误：", err)
		fmt.Println("按回车键退出...")
		fmt.Scanln()
		os.Exit(0)
	}
	configSystemPrompt(ExeDirPath)
	exist, err := checkConfig(ExeDirPath)
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
	exist, err = checkSkillsFolder(ExeDirPath)
	if err != nil {
		fmt.Println("检查skills文件夹错误：", err)
		fmt.Println("按回车键退出...")
		fmt.Scanln()
		os.Exit(0)
	}
	if exist == false && err == nil {
		fmt.Println("检查到skills文件夹不存在，已创建默认skills文件夹")
	}
	config_p, err := loadConfig()
	if err != nil {
		fmt.Println("加载配置文件错误：", err)
		fmt.Println("按回车键退出...")
	}
	Tools, Toolsets, Model := parseConfig(*config_p)
	runner := initAgent(Tools, Toolsets, Model, AgentName)
	ar := handler.AgentRunner{
		Runner: runner,
		Stream: Model.Stream,
	}
	return ar
}

// 配置系统提示词，替换其中的占位符
func configSystemPrompt(ExeDirPath string) {
	os_type := runtime.GOOS
	config.SystemPrompt = strings.ReplaceAll(config.SystemPrompt, "{{OSTYPE}}", os_type)
	DiaryPath := filepath.Join(ExeDirPath, "Diary")
	config.SystemPrompt = strings.ReplaceAll(config.SystemPrompt, "{{DIARYPATH}}", DiaryPath)
}

// 获取当前可执行文件所在的目录完整路径
func getExeDirPath() (string, error) {

	exePath, err := os.Executable() // 获取当前可执行文件的路径
	if err != nil {
		return "", fmt.Errorf("获取可执行文件路径错误：%v", err)
	}
	ExeDirPath := filepath.Dir(exePath) // 获取当前可执行文件的目录路径（不包含程序名）
	return ExeDirPath, nil
}

// 检查配置文件是否存在，不存在则创建一个默认的配置文件
func checkConfig(ExeDirPath string) (bool, error) {
	configName := "config.yaml"
	configPath := filepath.Join(ExeDirPath, configName)
	// TODO: 读取并解析 configPath 中的 YAML 配置
	_, err := os.Stat(configPath)
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
func checkSkillsFolder(ExeDirPath string) (bool, error) {
	SkillFolderPath := filepath.Join(ExeDirPath, "skills")
	_, err := os.Stat(SkillFolderPath)
	if err != nil {
		if os.IsNotExist(err) {
			//skills 文件夹不存在，创建一个默认的 skills 文件夹
			err := os.MkdirAll(SkillFolderPath, os.ModePerm)
			if err != nil {
				return false, fmt.Errorf("创建默认配置文件错误：%v", err)
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
				Stream: Model.Stream,
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
				Stream: Model.Stream,
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
