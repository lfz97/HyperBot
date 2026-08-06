package engine

import (
	"charm.land/glamour/v2"
	"context"
	"embed"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"HyperBot/service/engine/agent"
	"HyperBot/service/engine/config"
	m "HyperBot/service/engine/memory"
	s "HyperBot/service/engine/session"
	"HyperBot/service/engine/tools/functions"
	"HyperBot/service/engine/tools/toolsets"
	"HyperBot/service/engine/tools/toolsets/localexec"
	"HyperBot/utils/pretty"
	stdlog "log"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	ag "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-mcp-go"
)

//go:embed prompt/*
var fs embed.FS

// 定义配置文件夹中的各种配置文件名称
const (
	HyperBotConfigFolder string = ".hyperbot"
	HyperBotConfig       string = "hyperbot.yaml"
	SkillsFolder         string = "skills"
	HyperBotLogFile      string = "hyperbot.log"
	memoryDBFileName     string = "memory.db"
	outputDir            string = "output"
)

type Engine struct {
	Config_p            *config.Config      //yaml配置
	Agentname           string              //Agent名称
	CWD                 string              //当前工作目录
	ConfigFolderPath    string              //配置文件夹路径
	HyperBotConfigPath  string              //配置文件路径
	SkillFolderPath     string              //技能文件夹路径
	SkillRepo           *skill.FSRepository //技能仓库
	AgentRunner_p       *Agentrunner        //Runner，全局唯一
	SessionService_p    session.Service     //会话服务，包含自动摘要功能
	FrameworkLogFile_p  *os.File            // 保存日志文件句柄，防止被 GC 回收
	SqliteMemoryService memory.Service      // sqlite记忆服务
	Systemprompt        string              //agent的系统提示词
	Toolsets            []tool.ToolSet      //agent挂载的工具集
	Tools               []tool.Tool         //agent挂载的工具

	tui tuiService
}
type Agentrunner struct {
	Runner    runner.Runner
	Stream    bool
	SessionId string
	RequestId string
}

type tuiService interface {
	AddHelpItems(items []map[string]string)
	ClearAppFuncTrigger()
	PrintToMsgView(content string, clear bool)
	ReadInputAreaPromptWithEnter() string
	ResetHelpItems()
	SetAppFuncTriggerWithEsc(f func())
	ShowErrorInMsgViewAndExit(errmsg string)
	ShowMsgAndExitNoTrigger(msg string)
	ShowSuccessInMsgView(sussessmsg string)
	ShowSuccessInMsgViewAndExit(sussessmsg string)
	StatusBarScrollingTip(ctx context.Context, tip string, TColor string)
	StatusBarUserTip(s string)
	NewGlamourRenderer() *glamour.TermRenderer
}

func (e *Engine) preCheckLoad() {

	//获取Agent可执行文件所在的目录路径
	e.getcwd()

	//检查配置文件夹
	e.checkConfigFolder()

	//检查配置文件是否存在，不存在则创建一个默认的配置文件
	e.checkConfig()

	//检查skills文件夹是否存在
	e.checkSkillsFolder()

	// 将框架日志重定向到文件，避免输出到终端干扰 TUI显示
	e.redirectFrameworkLog()

	//设置系统提示词
	e.configSystemPrompt()

	//加载配置文件
	e.loadConfig()

	//初始化内存会话服务
	e.initInMemorySessionService()

	//初始化sqlite记忆服务
	e.initSqliteMemoryService()

	//加载skill
	e.loadSkills()

	//从配置文件加载工具集
	e.parseToolsetsFromConfig()

	//加载function工具
	e.loadFunctionTools()

}

func (e *Engine) newRunner() {
	var Runner runner.Runner
	Runner = runner.NewRunnerWithAgentFactory(
		(*e).Agentname,
		(*e).Agentname,
		func(ctx context.Context, ro ag.RunOptions) (ag.Agent, error) {
			(*e).reload()
			var Agent_p *llmagent.LLMAgent
			(*e).Tools = append((*e).Tools, (*e).SqliteMemoryService.Tools()...) //将SqliteMemoryService的工具添加到全局工具列表中，使得Agent能够调用记忆相关的工具
			opts := []llmagent.Option{
				llmagent.WithGenerationConfig(model.GenerationConfig{
					Stream: (*(*e).Config_p).Model.Stream,
				}),
				llmagent.WithTools((*e).Tools),
				llmagent.WithGlobalInstruction((*e).Systemprompt), //系统提示词
				llmagent.WithToolSets((*e).Toolsets),
				llmagent.WithRefreshToolSetsOnRun(true),
				llmagent.WithSkillsLoadedContentInToolResults(true),
				//仅注入知识，不注入执行工具的能力，统一通过localexec执行
				llmagent.WithSkills((*e).SkillRepo),
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
			if (*(*e).Config_p).Model.APIType == "openai" {
				Agent_p = agent.OpenaiAgent(
					(*e).Agentname,
					(*(*e).Config_p).Model,
					opts,
				)
			} else if (*(*e).Config_p).Model.APIType == "anthropic" {
				Agent_p = agent.AnthropicAgent(
					(*e).Agentname,
					(*(*e).Config_p).Model,
					opts,
				)

			} else {
				return nil, errors.New("不支持的API类型，请检查配置文件中的 Model.APIType 字段")
			}
			return Agent_p, nil
		},
		runner.WithSessionService((*e).SessionService_p),
		runner.WithMemoryService((*e).SqliteMemoryService),
	)
	(*e).AgentRunner_p = &Agentrunner{
		Runner: Runner,
		Stream: (*(*e).Config_p).Model.Stream,
	}
}

func (e *Engine) reload() {
	e.loadConfig()
	e.parseToolsetsFromConfig()
	e.loadFunctionTools()
	e.loadSkills()
}
func (e *Engine) loadFunctionTools() {
	(*e).Tools = []tool.Tool{} //清空全局工具列表，重新加载工具，确保工具的最新状态被加载
	fileopstools := functionTools.GetFileOperationsTools()
	fileSystemTools := functionTools.GetFileSystemTools()
	dateTools := functionTools.GetDateTools()
	(*e).Tools = append((*e).Tools, fileopstools...)
	(*e).Tools = append((*e).Tools, fileSystemTools...)
	(*e).Tools = append((*e).Tools, dateTools...)
}

func (e *Engine) parseToolsetsFromConfig() {
	(*e).Toolsets = []tool.ToolSet{} //先清空工具集

	if len((*(*e).Config_p).HttpMcp) != 0 {
		//读取配置文件中的 MCP 配置，创建 MCP ToolSet 并添加到 Toolsets 中
		for _, mcpConfig := range (*(*e).Config_p).HttpMcp {
			//只有配置了 Enabled 字段为 true 的 MCP 配置才会被创建 ToolSet 并添加到 Toolsets 中
			if mcpConfig.Enabled == true {
				mcpToolSet := toolsets.HttpMCP(mcpConfig)
				(*e).Toolsets = append((*e).Toolsets, mcpToolSet)
			}

		}
	}
	if len((*(*e).Config_p).StdinMcp) != 0 {
		//读取配置文件中的 StdinMCP 配置，创建 StdinMCP ToolSet 并添加到 Toolsets 中
		for _, stdinMcpConfig := range (*(*e).Config_p).StdinMcp {
			if stdinMcpConfig.Enabled == true {
				stdinMcpToolSet := toolsets.StdinMCP(stdinMcpConfig)
				(*e).Toolsets = append((*e).Toolsets, stdinMcpToolSet)
			}
		}
	}

	(*e).Toolsets = append((*e).Toolsets, localexec.LocalExec()) //localexec 必须启用

}
func (e *Engine) loadSkills() {
	(*e).SkillRepo, _ = skill.NewFSRepository((*e).SkillFolderPath)
	summaries := (*e).SkillRepo.Summaries()
	(*e).tui.ResetHelpItems()
	itms := []map[string]string{}
	for _, s := range summaries {

		des_rune := []rune(s.Description)
		if len(des_rune) >= 50 {
			des_rune = des_rune[:50]
		}
		des := string(des_rune) + "......"

		i := map[string]string{
			"/" + s.Name: des,
		}
		itms = append(itms, i)
	}
	(*e).tui.AddHelpItems(itms)
}
func (e *Engine) initSqliteMemoryService() {
	service, err := m.NewSQLiteMemoryService(((*e).Config_p).Model, filepath.Join((*e).ConfigFolderPath, memoryDBFileName))
	if err != nil {
		(*e).tui.ShowErrorInMsgViewAndExit(pretty.TErrorF("初始化sqlite记忆服务错误: %v", err))
	}
	(*e).SqliteMemoryService = service
}
func (e *Engine) loadConfig() {
	//加载配置文件
	c, err := config.LoadConfig((*e).HyperBotConfigPath)
	if err != nil {
		(*e).tui.ShowErrorInMsgViewAndExit(pretty.TErrorF("加载配置文件错误: %v,按任意键退出", err))
	}
	(*e).Config_p = c
}
func (e *Engine) initInMemorySessionService() {
	(*e).SessionService_p = s.NewMemorySessionService((*e).Config_p.Model, (*e).tui)
}

// 配置系统提示词，替换其中的占位符
func (e *Engine) configSystemPrompt() {
	systemprompt_b, _ := fs.ReadFile("prompt/systemprompt.md")
	(*e).Systemprompt = string(systemprompt_b)
	//Agent名称
	(*e).Systemprompt = strings.ReplaceAll((*e).Systemprompt, "{{NAME}}", (*e).Agentname)

	//当前日期
	(*e).Systemprompt = strings.ReplaceAll((*e).Systemprompt, "{{DATE}}", time.Now().Format("2006-01-02 15:04:05 (Mon)"))

	//当前时区
	zone, _ := time.Now().Zone()
	(*e).Systemprompt = strings.ReplaceAll((*e).Systemprompt, "{{TIMEZONE}}", fmt.Sprintf("%s (%s)", time.Now().Location().String(), zone))

	//操作系统
	(*e).Systemprompt = strings.ReplaceAll((*e).Systemprompt, "{{OSTYPE}}", runtime.GOOS)

	//CPU架构
	(*e).Systemprompt = strings.ReplaceAll((*e).Systemprompt, "{{AARCH}}", runtime.GOARCH)

	//主目录
	homeDir, _ := os.UserHomeDir()
	(*e).Systemprompt = strings.ReplaceAll((*e).Systemprompt, "{{HOME}}", homeDir)

	//临时目录
	(*e).Systemprompt = strings.ReplaceAll((*e).Systemprompt, "{{TMPDIR}}", os.TempDir())

	//当前用户
	u, _ := user.Current()
	(*e).Systemprompt = strings.ReplaceAll((*e).Systemprompt, "{{CURRENTUSER}}", u.Username)

	//主机名
	hostName, _ := os.Hostname()
	(*e).Systemprompt = strings.ReplaceAll((*e).Systemprompt, "{{HOSTNAME}}", hostName)

	//运行目录
	(*e).Systemprompt = strings.ReplaceAll((*e).Systemprompt, "{{CWD}}", (*e).CWD)

	//配置目录
	(*e).Systemprompt = strings.ReplaceAll((*e).Systemprompt, "{{CONFIGPATH}}", (*e).ConfigFolderPath)

	//配置文件
	(*e).Systemprompt = strings.ReplaceAll((*e).Systemprompt, "{{HyperBotConfig}}", HyperBotConfig)
	(*e).Systemprompt = strings.ReplaceAll((*e).Systemprompt, "{{SkillsFolder}}", SkillsFolder)
	(*e).Systemprompt = strings.ReplaceAll((*e).Systemprompt, "{{HyperBotLogFile}}", HyperBotLogFile)
	//输出目录
	outputPath := filepath.Join((*e).CWD, outputDir)
	(*e).Systemprompt = strings.ReplaceAll((*e).Systemprompt, "{{OUTPUTDIR}}", outputPath)
}

// redirectFrameworkLog 将框架的日志输出从 stdout 重定向到可执行文件同目录下的 hyperbot.log 文件-created by copilot
func (e *Engine) redirectFrameworkLog() {
	logPath := filepath.Join((*e).ConfigFolderPath, HyperBotLogFile)
	var err error
	(*e).FrameworkLogFile_p, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
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
		zapcore.AddSync((*e).FrameworkLogFile_p),
		zapcore.DebugLevel,
	)
	fileLogger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1)).Sugar()
	//定向trpc-agent-go的日志输出到文件
	log.Default = fileLogger
	log.ContextDefault = fileLogger

	//定向trpc-mcp-go的日志输出到文件
	mcp.SetDefaultLogger(fileLogger)

	//重定向标准库 log 到文件（避免 gse 等第三方库的日志污染终端）
	if (*e).FrameworkLogFile_p != nil {
		stdlog.SetOutput((*e).FrameworkLogFile_p)
	}
}

func (e *Engine) checkSkillsFolder() {
	(*e).SkillFolderPath = filepath.Join((*e).ConfigFolderPath, SkillsFolder)
	_, err := os.Stat((*e).SkillFolderPath)
	if err != nil {
		if os.IsNotExist(err) {
			//skills 文件夹不存在，创建一个默认的 skills 文件夹
			err := os.MkdirAll((*e).SkillFolderPath, os.ModePerm)
			if err != nil {
				(*e).tui.ShowErrorInMsgViewAndExit(pretty.TErrorF("创建默认skills文件夹错误：%v", err))
			}
			(*e).tui.ShowSuccessInMsgView("检查到skills文件夹不存在，已创建默认skills文件夹")
		} else {
			(*e).tui.ShowErrorInMsgViewAndExit(pretty.TErrorF("检查skills文件夹错误：%v", err))
		}
	}

}
func (e *Engine) getcwd() {
	exePath, err := os.Executable() // 获取当前可执行文件的路径
	if err != nil {
		(*e).tui.ShowErrorInMsgViewAndExit(pretty.TErrorF("获取可执行文件目录错误: %v,按任意键退出", err))
	}
	(*e).CWD = filepath.Dir(exePath) // 获取当前可执行文件的目录路径（不包含程序名）
}

func (e *Engine) checkConfigFolder() {
	(*e).ConfigFolderPath = filepath.Join((*e).CWD, HyperBotConfigFolder)
	_, err := os.Stat((*e).ConfigFolderPath)
	if err != nil {
		if os.IsNotExist(err) {
			//config 文件夹不存在，创建一个默认的 config 文件夹
			err := os.MkdirAll((*e).ConfigFolderPath, os.ModePerm)
			if err != nil {
				(*e).tui.ShowErrorInMsgViewAndExit(pretty.TErrorF("创建默认config文件夹错误：%v", err))
			}
			(*e).tui.ShowSuccessInMsgView("检查到config文件夹不存在，已创建默认config文件夹")
		} else {
			(*e).tui.ShowErrorInMsgViewAndExit(pretty.TErrorF("检查config文件夹错误：%v", err))
		}
	}

}

// 检查配置文件是否存在，不存在则创建一个默认的配置文件
func (e *Engine) checkConfig() {
	(*e).HyperBotConfigPath = filepath.Join((*e).ConfigFolderPath, HyperBotConfig)
	// TODO: 读取并解析 configPath 中的 YAML 配置
	_, err := os.Stat((*e).HyperBotConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件不存在，创建一个默认的 config.yaml
			fd, err := os.OpenFile((*e).HyperBotConfigPath, os.O_RDWR|os.O_CREATE, 0644)
			if err != nil {
				(*e).tui.ShowErrorInMsgViewAndExit(pretty.TErrorF("创建默认配置文件错误：%v", err))
			}
			defer fd.Close()
			//生成一个随机的用户ID，替换掉配置文件中的占位符
			cfg := strings.ReplaceAll(config.Template, "{USERID}", uuid.New().String())
			_, err = fd.WriteString(cfg)
			if err != nil {
				(*e).tui.ShowErrorInMsgViewAndExit(pretty.TErrorF("写入默认配置文件错误：%v,按任意键退出", err))
			}
			(*e).tui.ShowSuccessInMsgViewAndExit("检查到配置文件不存在，已创建默认配置文件。请根据实际情况修改配置文件后重新启动程序！")
		} else {
			(*e).tui.ShowErrorInMsgViewAndExit(pretty.TErrorF("检查配置文件错误：%v", err))
		}
	}

}
