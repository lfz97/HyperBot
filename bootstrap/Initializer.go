package bootstrap

import (
	"HyperBot/agent"
	"HyperBot/config"
	"HyperBot/functionTools"
	"HyperBot/global"
	"HyperBot/memory"
	"HyperBot/session"
	"HyperBot/toolsets"
	"HyperBot/toolsets/localexec"
	"HyperBot/utils/pretty"
	"fmt"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v2"
	stdlog "log"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-mcp-go"
)

// 定义配置文件夹中的各种配置文件名称
const (
	HyperBotConfigFolder string = ".hyperbot"
	HyperBotConfig       string = "hyperbot.yaml"
	SkillsFolder         string = "skills"
	HyperBotLogFile      string = "hyperbot.log"
	memoryDBFileName     string = "memory.db"
	outputDir            string = "output"
)

func Init(an string) {
	global.Agentname = an

	//获取Agent可执行文件所在的目录路径
	getcwd()

	//检查配置文件夹
	checkConfigFolder()

	//检查配置文件是否存在，不存在则创建一个默认的配置文件
	checkConfig()

	//检查skills文件夹是否存在
	checkSkillsFolder()

	//加载技能文件夹中的技能
	loadSkills()
	// 将框架日志重定向到文件，避免输出到终端干扰 TUI显示
	redirectFrameworkLog()

	//设置系统提示词
	configSystemPrompt()

	//加载配置文件
	LoadConfig()

	//初始化内存会话服务
	initInMemorySessionService()

	//初始化sqlite记忆服务
	initSqliteMemoryService()

	//加载function工具
	loadFunctionTools()
	//初始化AgentRunner
	NewRunner()

}

// 配置系统提示词，替换其中的占位符
func configSystemPrompt() {
	systemprompt_b, _ := global.PromptFiles.ReadFile("prompt/systemprompt.md")
	global.Systemprompt = string(systemprompt_b)
	//Agent名称
	global.Systemprompt = strings.ReplaceAll(global.Systemprompt, "{{NAME}}", global.Agentname)

	//当前日期
	global.Systemprompt = strings.ReplaceAll(global.Systemprompt, "{{DATE}}", time.Now().Format("2006-01-02 15:04:05 (Mon)"))

	//当前时区
	zone, _ := time.Now().Zone()
	global.Systemprompt = strings.ReplaceAll(global.Systemprompt, "{{TIMEZONE}}", fmt.Sprintf("%s (%s)", time.Now().Location().String(), zone))

	//操作系统
	global.Systemprompt = strings.ReplaceAll(global.Systemprompt, "{{OSTYPE}}", runtime.GOOS)

	//CPU架构
	global.Systemprompt = strings.ReplaceAll(global.Systemprompt, "{{AARCH}}", runtime.GOARCH)

	//主目录
	homeDir, _ := os.UserHomeDir()
	global.Systemprompt = strings.ReplaceAll(global.Systemprompt, "{{HOME}}", homeDir)

	//临时目录
	global.Systemprompt = strings.ReplaceAll(global.Systemprompt, "{{TMPDIR}}", os.TempDir())

	//当前用户
	u, _ := user.Current()
	global.Systemprompt = strings.ReplaceAll(global.Systemprompt, "{{CURRENTUSER}}", u.Username)

	//主机名
	hostName, _ := os.Hostname()
	global.Systemprompt = strings.ReplaceAll(global.Systemprompt, "{{HOSTNAME}}", hostName)

	//运行目录
	global.Systemprompt = strings.ReplaceAll(global.Systemprompt, "{{CWD}}", global.CWD)

	//配置目录
	global.Systemprompt = strings.ReplaceAll(global.Systemprompt, "{{CONFIGPATH}}", global.ConfigFolderPath)

	//配置文件
	global.Systemprompt = strings.ReplaceAll(global.Systemprompt, "{{HyperBotConfig}}", HyperBotConfig)
	global.Systemprompt = strings.ReplaceAll(global.Systemprompt, "{{SkillsFolder}}", SkillsFolder)
	global.Systemprompt = strings.ReplaceAll(global.Systemprompt, "{{HyperBotLogFile}}", HyperBotLogFile)
	//输出目录
	outputPath := filepath.Join(global.CWD, outputDir)
	global.Systemprompt = strings.ReplaceAll(global.Systemprompt, "{{OUTPUTDIR}}", outputPath)
}

// 获取当前可执行文件所在的目录完整路径
func getcwd() {

	exePath, err := os.Executable() // 获取当前可执行文件的路径
	if err != nil {
		global.ShowErrorAndExit(global.Log, pretty.TErrorF("获取可执行文件目录错误: %v,按任意键退出", err))
	}
	global.CWD = filepath.Dir(exePath) // 获取当前可执行文件的目录路径（不包含程序名）

}

// 检查配置文件夹是否存在
func checkConfigFolder() {
	global.ConfigFolderPath = filepath.Join(global.CWD, HyperBotConfigFolder)
	_, err := os.Stat(global.ConfigFolderPath)
	if err != nil {
		if os.IsNotExist(err) {
			//config 文件夹不存在，创建一个默认的 config 文件夹
			err := os.MkdirAll(global.ConfigFolderPath, os.ModePerm)
			if err != nil {
				global.ShowErrorAndExit(global.Log, pretty.TErrorF("创建默认config文件夹错误：%v", err))
			}
			global.ShowSuccess(global.Log, "检查到config文件夹不存在，已创建默认config文件夹")
		} else {
			global.ShowErrorAndExit(global.Log, pretty.TErrorF("检查config文件夹错误：%v", err))
		}
	} else {
		global.ShowSuccess(global.Log, "检查配置文件夹通过")
	}

}

// 检查配置文件是否存在，不存在则创建一个默认的配置文件
func checkConfig() {
	global.HyperBotConfigPath = filepath.Join(global.ConfigFolderPath, HyperBotConfig)
	// TODO: 读取并解析 configPath 中的 YAML 配置
	_, err := os.Stat(global.HyperBotConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件不存在，创建一个默认的 config.yaml
			fd, err := os.OpenFile(global.HyperBotConfigPath, os.O_RDWR|os.O_CREATE, 0644)
			if err != nil {
				global.ShowErrorAndExit(global.Log, pretty.TErrorF("创建默认配置文件错误：%v", err))
			}
			defer fd.Close()
			//生成一个随机的用户ID，替换掉配置文件中的占位符
			cfg := strings.ReplaceAll(config.Template, "{USERID}", uuid.New().String())
			_, err = fd.WriteString(cfg)
			if err != nil {
				global.ShowErrorAndExit(global.Log, pretty.TErrorF("写入默认配置文件错误：%v,按任意键退出", err))
			}
			global.ShowSuccessAndExit(global.Log, "检查到配置文件不存在，已创建默认配置文件。请根据实际情况修改配置文件后重新启动程序！")
		} else {
			global.ShowErrorAndExit(global.Log, pretty.TErrorF("检查配置文件错误：%v", err))
		}
	} else {
		global.ShowSuccess(global.Log, "检查配置文件通过!")
	}

}

func checkSkillsFolder() {
	global.SkillFolderPath = filepath.Join(global.ConfigFolderPath, SkillsFolder)
	_, err := os.Stat(global.SkillFolderPath)
	if err != nil {
		if os.IsNotExist(err) {
			//skills 文件夹不存在，创建一个默认的 skills 文件夹
			err := os.MkdirAll(global.SkillFolderPath, os.ModePerm)
			if err != nil {
				global.ShowErrorAndExit(global.Log, pretty.TErrorF("创建默认skills文件夹错误：%v", err))
			}
			global.ShowSuccess(global.Log, "检查到skills文件夹不存在，已创建默认skills文件夹")
		} else {
			global.ShowErrorAndExit(global.Log, pretty.TErrorF("检查skills文件夹错误：%v", err))
		}
	} else {
		global.ShowSuccess(global.Log, "检查skills文件夹通过")

	}

}

func loadSkills() {
	global.SkillRepo, _ = skill.NewFSRepository(global.SkillFolderPath)
}

func loadConfig() (*config.Config, error) {
	YamlConfig := config.Config{}
	yamlFile, err := os.ReadFile(global.HyperBotConfigPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件错误：%v", err)
	}
	err = yaml.Unmarshal(yamlFile, &YamlConfig)
	if err != nil {
		return nil, fmt.Errorf("解析配置文件错误：%v", err)
	}
	return &YamlConfig, nil
}

func parseToolsetsFromConfig() {
	global.Toolsets = []tool.ToolSet{} //先清空工具集

	if len((*global.Config_p).HttpMcp) != 0 {
		//读取配置文件中的 MCP 配置，创建 MCP ToolSet 并添加到 Toolsets 中
		for _, mcpConfig := range (*global.Config_p).HttpMcp {
			//只有配置了 Enabled 字段为 true 的 MCP 配置才会被创建 ToolSet 并添加到 Toolsets 中
			if mcpConfig.Enabled == true {
				mcpToolSet := toolsets.HttpMCP(mcpConfig)
				global.Toolsets = append(global.Toolsets, mcpToolSet)
			}

		}
	}
	if len((*global.Config_p).StdinMcp) != 0 {
		//读取配置文件中的 StdinMCP 配置，创建 StdinMCP ToolSet 并添加到 Toolsets 中
		for _, stdinMcpConfig := range (*global.Config_p).StdinMcp {
			if stdinMcpConfig.Enabled == true {
				stdinMcpToolSet := toolsets.StdinMCP(stdinMcpConfig)
				global.Toolsets = append(global.Toolsets, stdinMcpToolSet)
			}
		}
	}

	global.Toolsets = append(global.Toolsets, localexec.LocalExec()) //localexec 必须启用

}

func initInMemorySessionService() {
	global.SessionService_p = session.NewMemorySessionService((*global.Config_p).Model)
}

func initSqliteMemoryService() {
	service, err := memory.NewSQLiteMemoryService(filepath.Join(global.ConfigFolderPath, memoryDBFileName))
	if err != nil {
		global.ShowErrorAndExit(global.Log, pretty.TErrorF("初始化sqlite记忆服务错误: %v", err))
	}
	global.SqliteMemoryService = service
}

func initAgent() runner.Runner {
	var Runner runner.Runner
	var Agent_p *llmagent.LLMAgent
	global.Tools = append(global.Tools, global.SqliteMemoryService.Tools()...) //将SqliteMemoryService的工具添加到全局工具列表中，使得Agent能够调用记忆相关的工具
	opts := []llmagent.Option{
		llmagent.WithGenerationConfig(model.GenerationConfig{
			Stream: (*global.Config_p).Model.Stream,
		}),
		llmagent.WithTools(global.Tools),
		llmagent.WithGlobalInstruction(global.Systemprompt), //系统提示词
		llmagent.WithToolSets(global.Toolsets),
		llmagent.WithRefreshToolSetsOnRun(true),
		llmagent.WithSkillsLoadedContentInToolResults(true),
		//仅注入知识，不注入执行工具的能力，统一通过localexec执行
		llmagent.WithSkills(global.SkillRepo),
		llmagent.WithSkillToolProfile(
			llmagent.SkillToolProfileKnowledgeOnly,
		),
		llmagent.WithAddSessionSummary(true),                                           //启用上下文压缩注入
		llmagent.WithSessionSummaryInjectionMode(llmagent.SessionSummaryInjectionUser), //摘要注入到user message，不与system prompt中的SOP规则竞争优先级
		llmagent.WithSyncSummaryIntraRun(true),                                         //在同一次对话中同步更新摘要
		llmagent.WithEnableContextCompaction(true),                                     // 启用 tool result 压缩（Pass 1+2）
		llmagent.WithContextCompactionOversizedToolResultMaxTokens(8192),               // Pass 2: 超大 tool result 首尾保留截断
		llmagent.WithEnableOnDemandSession(true),                                       // 按需加载被压缩的原始数据（session_load）
		llmagent.WithPreloadMemory(10),                                                 // 预加载最近的10条记忆到上下文中，提升模型对近期事件的记忆能力
		llmagent.WithEnableParallelTools(true),                                         //启用并行工具调用，提升工具调用效率
	}
	if (*global.Config_p).Model.APIType == "openai" {
		Agent_p = agent.OpenaiAgent(
			global.Agentname,
			(*global.Config_p).Model.Model,
			(*global.Config_p).Model.BaseURL,
			(*global.Config_p).Model.APIKey,
			opts,
		)
	} else if (*global.Config_p).Model.APIType == "anthropic" {
		Agent_p = agent.AnthropicAgent(
			global.Agentname,
			(*global.Config_p).Model.Model,
			(*global.Config_p).Model.BaseURL,
			(*global.Config_p).Model.APIKey,
			opts,
		)

	} else {
		pretty.ErrorWithExit("不支持的API类型，请检查配置文件中的 Model.APIType 字段")
	}

	Runner = runner.NewRunner(
		global.Agentname,
		Agent_p,
		runner.WithSessionService(global.SessionService_p),   // 使用内存会话服务，其中包含自动摘要功能
		runner.WithMemoryService(global.SqliteMemoryService), // 使用sqlite记忆服务
	)
	return Runner
}

func LoadConfig() {
	//加载配置文件
	global.Config_p = nil
	config_p, err := loadConfig()
	if err != nil {
		global.ShowErrorAndExit(global.Log, pretty.TErrorF("加载配置文件错误: %v,按任意键退出", err))
	}
	global.Config_p = config_p
}

func loadFunctionTools() {
	global.Tools = []tool.Tool{} //清空全局工具列表，重新加载工具，确保工具的最新状态被加载
	fileopstools := functionTools.GetFileOperationsTools()
	fileSystemTools := functionTools.GetFileSystemTools()
	dateTools := functionTools.GetDateTools()
	global.Tools = append(global.Tools, fileopstools...)
	global.Tools = append(global.Tools, fileSystemTools...)
	global.Tools = append(global.Tools, dateTools...)
}
func NewRunner() {
	LoadConfig()              //加载配置文件
	parseToolsetsFromConfig() //从配置文件加载工具集
	loadFunctionTools()       //加载function工具
	loadSkills()              //加载技能文件夹中的技能
	runner := initAgent()
	global.AgentRunner_p = &global.Agentrunner{
		Runner: runner,
		Stream: (*global.Config_p).Model.Stream,
	}

	global.PrintToTui(global.Log, pretty.TReady(global.Agentname), false)

}

// redirectFrameworkLog 将框架的日志输出从 stdout 重定向到可执行文件同目录下的 hyperbot.log 文件-created by copilot
func redirectFrameworkLog() {
	logPath := filepath.Join(global.ConfigFolderPath, HyperBotLogFile)
	var err error
	global.FrameworkLogFile_p, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
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
		zapcore.AddSync(global.FrameworkLogFile_p),
		zapcore.DebugLevel,
	)
	fileLogger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1)).Sugar()
	//定向trpc-agent-go的日志输出到文件
	log.Default = fileLogger
	log.ContextDefault = fileLogger

	//定向trpc-mcp-go的日志输出到文件
	mcp.SetDefaultLogger(fileLogger)

	//重定向标准库 log 到文件（避免 gse 等第三方库的日志污染终端）
	if global.FrameworkLogFile_p != nil {
		stdlog.SetOutput(global.FrameworkLogFile_p)
	}
}
