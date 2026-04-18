package bootstrap

import (
	"HyperBot/agent"
	"HyperBot/config"
	"HyperBot/handler"
	"HyperBot/session"
	"HyperBot/toolsets"
	"HyperBot/toolsets/localexec"
	"HyperBot/tui/global_object"
	"HyperBot/utils/pretty"
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v2"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// 定义配置文件夹路径
var ConfigFolderPath string

// 定义配置文件夹中的各种配置文件名称
const (
	HyperBotConfigFolder string = ".hyperbot"
	HyperBotConfig       string = "hyperbot.yaml"
	SkillsFolder         string = "skills"
	HyperBotLogFile      string = "hyperbot.log"
	OperationRecord      string = "OperationRecord.md"
	outputDir            string = "output"
)

func Init(AgentName string) handler.AgentRunner {

	//获取Agent可执行文件所在的目录路径
	cwd, err := getcwd()
	if err != nil {
		done := make(chan struct{})
		global_object.App_p.QueueUpdateDraw(func() {
			fmt.Fprint(global_object.LogView_p, pretty.TErrorF("获取可执行文件目录错误: %v,按任意键退出", err))
			global_object.LogView_p.ScrollToEnd()
			//只要有按键就退出程序
			global_object.App_p.SetFocus(global_object.LogView_p)
			global_object.LogView_p.SetInputCapture(
				func(event *tcell.EventKey) *tcell.EventKey {
					global_object.App_p.Stop()
					//close(done)
					return nil

				})
		})
		<-done
	}

	//检查配置文件夹
	cfp, isExist, err := checkConfigFolder(cwd)
	if isExist == false && err == nil { //如果文件夹不存在并且没有错误，说明成功创建了文件夹，输出成功信息
		global_object.App_p.QueueUpdateDraw(func() {
			fmt.Fprint(global_object.LogView_p, pretty.TSuccess("检查到config文件夹不存在，已创建默认config文件夹"))
			global_object.LogView_p.ScrollToEnd()
		})
		ConfigFolderPath = cfp

	} else if isExist == false && err != nil { //如果文件夹不存在并且有错误，说明创建文件夹失败，输出错误信息并退出程序
		done := make(chan struct{})
		global_object.App_p.QueueUpdateDraw(func() {
			fmt.Fprint(global_object.LogView_p, pretty.TErrorF("检查config文件夹错误: %v,按任意键退出", err))
			global_object.LogView_p.ScrollToEnd()
			//只要有按键就退出程序
			global_object.App_p.SetFocus(global_object.LogView_p)
			global_object.LogView_p.SetInputCapture(
				func(event *tcell.EventKey) *tcell.EventKey {
					global_object.App_p.Stop()
					//close(done)
					return nil

				})
		})
		<-done
	} else if isExist == true { //如果文件夹存在，输出成功信息
		global_object.App_p.QueueUpdateDraw(func() {
			fmt.Fprint(global_object.LogView_p, pretty.TSuccess("检查到config文件夹通过"))
			global_object.LogView_p.ScrollToEnd()
		})
		ConfigFolderPath = cfp
	}

	//检查配置文件是否存在，不存在则创建一个默认的配置文件
	HyperBotConfigPath, exist, err := checkConfig()
	if err != nil {
		global_object.App_p.QueueUpdateDraw(func() {
			fmt.Fprint(global_object.LogView_p, pretty.TErrorF("检查配置文件错误: %v", err))
			global_object.LogView_p.ScrollToEnd()
		})
	} else if exist == false && err == nil { //如果文件不存在并且没有错误，说明成功创建了文件，输出成功信息并提示用户修改配置文件
		done := make(chan struct{})
		global_object.App_p.QueueUpdateDraw(func() {
			fmt.Fprint(global_object.LogView_p, pretty.TWelcome("已创建默认配置文件，请修改后重新启动程序。按任意键退出"))
			global_object.LogView_p.ScrollToEnd()
			//只要有按键就退出程序
			global_object.App_p.SetFocus(global_object.LogView_p)
			global_object.LogView_p.SetInputCapture(
				func(event *tcell.EventKey) *tcell.EventKey {
					global_object.App_p.Stop()
					//close(done)
					return nil

				})

		})
		<-done
	} else if exist == true {
		global_object.App_p.QueueUpdateDraw(func() {
			fmt.Fprint(global_object.LogView_p, pretty.TSuccess("检查配置文件通过"))
			global_object.LogView_p.ScrollToEnd()
		})
	}

	//加载配置文件
	config_p, err := loadConfig(HyperBotConfigPath)
	if err != nil {
		done := make(chan struct{})
		global_object.App_p.QueueUpdateDraw(func() {
			fmt.Fprint(global_object.LogView_p, pretty.TErrorF("加载配置文件错误: %v,按任意键退出", err))
			global_object.LogView_p.ScrollToEnd()
			//只要有按键就退出程序
			global_object.App_p.SetFocus(global_object.LogView_p)
			global_object.LogView_p.SetInputCapture(
				func(event *tcell.EventKey) *tcell.EventKey {
					global_object.App_p.Stop()
					//close(done)
					return nil

				})
		})
		<-done
	}

	//检查skills文件夹是否存在
	SkillFolderPath, exist, err := checkSkillsFolder()
	if err != nil { //检查skills文件夹错误，输出错误信息并退出程序
		done := make(chan struct{})
		global_object.App_p.QueueUpdateDraw(func() {
			fmt.Fprint(global_object.LogView_p, pretty.TErrorF("检查skills文件夹错误: %v,按任意键退出", err))
			global_object.LogView_p.ScrollToEnd()
			//只要有按键就退出程序
			global_object.App_p.SetFocus(global_object.LogView_p)
			global_object.LogView_p.SetInputCapture(
				func(event *tcell.EventKey) *tcell.EventKey {
					global_object.App_p.Stop()
					//close(done)
					return nil

				})
		})
		<-done
	} else if exist == false && err == nil { //如果skills文件夹不存在并且没有错误，说明成功创建了文件夹，输出成功信息
		global_object.App_p.QueueUpdateDraw(func() {
			fmt.Fprint(global_object.LogView_p, pretty.TSuccess("检查到skills文件夹不存在，已创建默认skills文件夹"))
			global_object.LogView_p.ScrollToEnd()
		})
	} else if exist == true { //如果文件夹存在，输出成功信息
		global_object.App_p.QueueUpdateDraw(func() {
			fmt.Fprint(global_object.LogView_p, pretty.TSuccess("检查skills文件夹通过"))
			global_object.LogView_p.ScrollToEnd()
		})
	}

	//设置系统提示词
	configSystemPrompt(AgentName, cwd)

	// 将框架日志重定向到文件，避免输出到终端干扰 TUI显示
	redirectFrameworkLog()

	//解析配置文件
	Tools, Toolsets, Model, User := parseConfig(*config_p)
	runner := initAgent(Tools, Toolsets, Model, AgentName, SkillFolderPath)
	ar := handler.AgentRunner{
		Runner: runner,
		Stream: Model.Stream,
		UserId: User.UserID,
	}
	global_object.App_p.QueueUpdateDraw(func() {
		fmt.Fprint(global_object.LogView_p, pretty.TReady(AgentName))
		global_object.LogView_p.ScrollToEnd()
	})
	return ar
}

// 配置系统提示词，替换其中的占位符
func configSystemPrompt(AgentName string, cwd string) {

	//Agent名称
	config.SystemPrompt = strings.ReplaceAll(config.SystemPrompt, "{{NAME}}", AgentName)

	//当前日期
	config.SystemPrompt = strings.ReplaceAll(config.SystemPrompt, "{{DATE}}", time.Now().Format("2006-01-02 15:04:05 (Mon)"))

	//当前时区
	zone, _ := time.Now().Zone()
	config.SystemPrompt = strings.ReplaceAll(config.SystemPrompt, "{{TIMEZONE}}", fmt.Sprintf("%s (%s)", time.Now().Location().String(), zone))

	//操作系统
	config.SystemPrompt = strings.ReplaceAll(config.SystemPrompt, "{{OSTYPE}}", runtime.GOOS)

	//CPU架构
	config.SystemPrompt = strings.ReplaceAll(config.SystemPrompt, "{{AARCH}}", runtime.GOARCH)

	//主目录
	homeDir, _ := os.UserHomeDir()
	config.SystemPrompt = strings.ReplaceAll(config.SystemPrompt, "{{HOME}}", homeDir)

	//临时目录
	config.SystemPrompt = strings.ReplaceAll(config.SystemPrompt, "{{TMPDIR}}", os.TempDir())

	//当前用户
	u, _ := user.Current()
	config.SystemPrompt = strings.ReplaceAll(config.SystemPrompt, "{{CURRENTUSER}}", u.Username)

	//主机名
	hostName, _ := os.Hostname()
	config.SystemPrompt = strings.ReplaceAll(config.SystemPrompt, "{{HOSTNAME}}", hostName)

	//运行目录
	config.SystemPrompt = strings.ReplaceAll(config.SystemPrompt, "{{CWD}}", cwd)

	//配置目录
	config.SystemPrompt = strings.ReplaceAll(config.SystemPrompt, "{{CONFIGPATH}}", ConfigFolderPath)

	//配置文件
	config.SystemPrompt = strings.ReplaceAll(config.SystemPrompt, "{{HyperBotConfig}}", HyperBotConfig)
	config.SystemPrompt = strings.ReplaceAll(config.SystemPrompt, "{{SkillsFolder}}", SkillsFolder)
	config.SystemPrompt = strings.ReplaceAll(config.SystemPrompt, "{{HyperBotLogFile}}", HyperBotLogFile)
	config.SystemPrompt = strings.ReplaceAll(config.SystemPrompt, "{{OperationRecord}}", OperationRecord)

	//输出目录
	outputDir := filepath.Join(cwd, outputDir)
	config.SystemPrompt = strings.ReplaceAll(config.SystemPrompt, "{{OUTPUTDIR}}", outputDir)
}

// 获取当前可执行文件所在的目录完整路径
func getcwd() (string, error) {

	exePath, err := os.Executable() // 获取当前可执行文件的路径
	if err != nil {
		return "", fmt.Errorf("获取可执行文件路径错误：%v", err)
	}
	cwd := filepath.Dir(exePath) // 获取当前可执行文件的目录路径（不包含程序名）
	return cwd, nil
}

// 检查配置文件夹是否存在
func checkConfigFolder(cwd string) (string, bool, error) {
	ConfigFolderPath := filepath.Join(cwd, HyperBotConfigFolder)
	_, err := os.Stat(ConfigFolderPath)
	if err != nil {
		if os.IsNotExist(err) {
			//config 文件夹不存在，创建一个默认的 config 文件夹
			err := os.MkdirAll(ConfigFolderPath, os.ModePerm)
			if err != nil {
				return "", false, fmt.Errorf("创建默认config文件夹错误：%v", err)
			}
			return ConfigFolderPath, false, nil
		} else {
			return "", false, fmt.Errorf("检查config文件夹错误：%v", err)
		}
	}
	return ConfigFolderPath, true, nil
}

// 检查配置文件是否存在，不存在则创建一个默认的配置文件
func checkConfig() (string, bool, error) {
	HyperBotConfigPath := filepath.Join(ConfigFolderPath, HyperBotConfig)
	// TODO: 读取并解析 configPath 中的 YAML 配置
	_, err := os.Stat(HyperBotConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件不存在，创建一个默认的 config.yaml
			fd, err := os.OpenFile(HyperBotConfigPath, os.O_RDWR|os.O_CREATE, 0644)
			if err != nil {
				return "", false, fmt.Errorf("创建默认配置文件错误：%v", err)
			}
			defer fd.Close()
			cfg := config.Template
			//生成一个随机的用户ID，替换掉配置文件中的占位符
			cfg = strings.ReplaceAll(cfg, "{USERID}", uuid.New().String())
			_, err = fd.WriteString(cfg)
			if err != nil {
				return "", false, fmt.Errorf("写入默认配置文件错误：%v", err)
			}
			return HyperBotConfigPath, false, nil
		} else {
			return "", false, fmt.Errorf("检查配置文件错误：%v", err)
		}
	}
	return HyperBotConfigPath, true, nil
}

func checkSkillsFolder() (string, bool, error) {
	SkillFolderPath := filepath.Join(ConfigFolderPath, "skills")
	_, err := os.Stat(SkillFolderPath)
	if err != nil {
		if os.IsNotExist(err) {
			//skills 文件夹不存在，创建一个默认的 skills 文件夹
			err := os.MkdirAll(SkillFolderPath, os.ModePerm)
			if err != nil {
				return "", false, fmt.Errorf("创建默认skills文件夹错误：%v", err)
			}
			return SkillFolderPath, false, nil
		} else {
			return "", false, fmt.Errorf("检查skills文件夹错误：%v", err)
		}
	}
	return SkillFolderPath, true, nil
}

func loadConfig(HyperBotConfigPath string) (*config.Config, error) {
	YamlConfig := config.Config{}
	yamlFile, err := os.ReadFile(HyperBotConfigPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件错误：%v", err)
	}
	err = yaml.Unmarshal(yamlFile, &YamlConfig)
	if err != nil {
		return nil, fmt.Errorf("解析配置文件错误：%v", err)
	}
	return &YamlConfig, nil
}

func parseConfig(RunningConfig config.Config) ([]tool.Tool, []tool.ToolSet, config.Model, config.User) {
	Tools := []tool.Tool{}
	Toolsets := []tool.ToolSet{}

	if len(RunningConfig.Mcp) != 0 {
		//读取配置文件中的 MCP 配置，创建 MCP ToolSet 并添加到 Toolsets 中
		for _, mcpConfig := range RunningConfig.Mcp {
			//只有配置了 Enabled 字段为 true 的 MCP 配置才会被创建 ToolSet 并添加到 Toolsets 中
			if mcpConfig.Enabled == true {
				mcpToolSet := toolsets.MCP(string(mcpConfig.Type), mcpConfig.Endpoint, mcpConfig.Headers)
				Toolsets = append(Toolsets, mcpToolSet)
			}

		}
	}
	if len(RunningConfig.StdinMcp) != 0 {
		//读取配置文件中的 StdinMCP 配置，创建 StdinMCP ToolSet 并添加到 Toolsets 中
		for _, stdinMcpConfig := range RunningConfig.StdinMcp {
			if stdinMcpConfig.Enabled == true {
				stdinMcpToolSet := toolsets.StdinMCP(stdinMcpConfig.Command, stdinMcpConfig.Args)
				Toolsets = append(Toolsets, stdinMcpToolSet)
			}
		}
	}

	Toolsets = append(Toolsets, localexec.LocalExec()) //localexec 必须启用
	return Tools, Toolsets, RunningConfig.Model, RunningConfig.User
}

func initAgent(Tools []tool.Tool, Toolsets []tool.ToolSet, Model config.Model, AgentName string, skillsPath string) runner.Runner {
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
			skillsPath,
		)
		Runner = runner.NewRunner(AgentName, Agent_p,
			runner.WithSessionService(session.NewMemorySessionService(Model)), // 使用内存会话服务，其中包含自动摘要功能
		)
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
			skillsPath,
		)
		Runner = runner.NewRunner(AgentName, Agent_p,
			runner.WithSessionService(session.NewMemorySessionService(Model)), // 使用内存会话服务，其中包含自动摘要功能
		)
	} else {
		pretty.ErrorWithExit("不支持的API类型，请检查配置文件中的 Model.APIType 字段")
	}

	return Runner
}

// redirectFrameworkLog 将框架的日志输出从 stdout 重定向到可执行文件同目录下的 hyperbot.log 文件-created by copilot
func redirectFrameworkLog() {
	logPath := filepath.Join(ConfigFolderPath, HyperBotLogFile)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	encoderCfg := zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "lvl",
		NameKey:        "name",
		CallerKey:      "caller",
		MessageKey:     "message",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     zapcore.RFC3339TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderCfg),
		zapcore.AddSync(logFile),
		zapcore.DebugLevel,
	)
	fileLogger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1)).Sugar()
	log.Default = fileLogger
	log.ContextDefault = fileLogger
}
