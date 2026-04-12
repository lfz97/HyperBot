package bootstrap

import (
	"HyperBot/agent"
	"HyperBot/config"
	"HyperBot/handler"
	"HyperBot/toolsets"
	"HyperBot/toolsets/localexec"
	"HyperBot/utils/pretty"
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/google/uuid"
	"github.com/rivo/tview"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v2"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

func Init(AgentName string, app_p *tview.Application, view_p *tview.TextView) handler.AgentRunner {
	// 将框架日志重定向到文件，避免输出到终端干扰 TUI
	redirectFrameworkLog()

	ExeDirPath, err := getExeDirPath()
	if err != nil {
		done := make(chan struct{})
		app_p.QueueUpdateDraw(func() {
			fmt.Fprint(view_p, pretty.TErrorF("获取可执行文件目录错误: %v", err))
			view_p.ScrollToEnd()
			app_p.Stop()
		})
		<-done
	}
	configSystemPrompt(ExeDirPath)
	exist, err := checkConfig(ExeDirPath)
	if err != nil {
		app_p.QueueUpdateDraw(func() {
			fmt.Fprint(view_p, pretty.TErrorF("检查配置文件错误: %v", err))
			view_p.ScrollToEnd()
		})
	}
	if exist == false && err == nil {
		done := make(chan struct{})
		app_p.QueueUpdateDraw(func() {
			fmt.Fprint(view_p, pretty.TWelcome("已创建默认配置文件，请修改后重新启动程序。按回车键退出"))
			view_p.ScrollToEnd()
			//只要有按键就退出程序
			app_p.SetFocus(view_p)
			view_p.SetInputCapture(
				func(event *tcell.EventKey) *tcell.EventKey {
					app_p.Stop()
					//close(done)
					return nil

				})

		})
		<-done
	}
	config_p, err := loadConfig(ExeDirPath)
	if err != nil {
		app_p.QueueUpdateDraw(func() {
			fmt.Fprint(view_p, pretty.TErrorF("加载配置文件错误: %v", err))
			view_p.ScrollToEnd()
		})
	}
	exist, err = checkSkillsFolder(ExeDirPath)
	if err != nil {
		app_p.QueueUpdateDraw(func() {
			fmt.Fprint(view_p, pretty.TErrorF("检查skills文件夹错误: %v", err))
			view_p.ScrollToEnd()
		})
	}
	if exist == false && err == nil {
		app_p.QueueUpdateDraw(func() {
			fmt.Fprint(view_p, pretty.TSuccess("检查到skills文件夹不存在，已创建默认skills文件夹"))
			view_p.ScrollToEnd()
		})
	}

	Tools, Toolsets, Model, User := parseConfig(*config_p)
	runner := initAgent(Tools, Toolsets, Model, AgentName, ExeDirPath)
	ar := handler.AgentRunner{
		Runner: runner,
		Stream: Model.Stream,
		UserId: User.UserID,
	}
	app_p.QueueUpdateDraw(func() {
		fmt.Fprint(view_p, pretty.TReady(AgentName))
		view_p.ScrollToEnd()
	})
	return ar
}

// 配置系统提示词，替换其中的占位符
func configSystemPrompt(ExeDirPath string) {
	date := time.Now().Local().String()
	config.SystemPrompt = strings.ReplaceAll(config.SystemPrompt, "{{DATE}}", date)
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
	configPath := filepath.Join(ExeDirPath, "config.yaml")
	// TODO: 读取并解析 configPath 中的 YAML 配置
	_, err := os.Stat(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件不存在，创建一个默认的 config.yaml
			fd, err := os.OpenFile(configPath, os.O_RDWR|os.O_CREATE, 0644)
			if err != nil {
				return false, fmt.Errorf("创建默认配置文件错误：%v", err)
			}
			cfg := config.Template
			//生成一个随机的用户ID，替换掉配置文件中的占位符
			cfg = strings.ReplaceAll(cfg, "{USERID}", uuid.New().String())
			_, err = fd.WriteString(cfg)
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
				return false, fmt.Errorf("创建默认skills文件夹错误：%v", err)
			}
			return false, nil

		}
	}
	return true, nil
}

func loadConfig(ExeDirPath string) (*config.Config, error) {
	YamlConfig := config.Config{}

	configPath := filepath.Join(ExeDirPath, "config.yaml")
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

func parseConfig(RunningConfig config.Config) ([]tool.Tool, []tool.ToolSet, config.Model, config.User) {
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
	return Tools, Toolsets, RunningConfig.Model, RunningConfig.User
}

func initAgent(Tools []tool.Tool, Toolsets []tool.ToolSet, Model config.Model, AgentName string, ExeDirPath string) runner.Runner {
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
			ExeDirPath,
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
			ExeDirPath,
		)
		Runner = runner.NewRunner(AgentName, Agent_p)
	} else {
		pretty.ErrorWithExit("不支持的API类型，请检查配置文件中的 Model.APIType 字段")
	}

	return Runner
}

// redirectFrameworkLog 将框架的日志输出从 stdout 重定向到可执行文件同目录下的 hyperbot.log 文件-created by copilot
func redirectFrameworkLog() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	logPath := filepath.Join(filepath.Dir(exePath), "hyperbot.log")
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
