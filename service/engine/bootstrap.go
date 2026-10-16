package engine

// bootstrap 环境检查器：只做检查与供给，返回错误和产物；不驻留、不碰 UI。
// 致命呈现（parkWithFatal）与通知上屏（setNotice）是 Engine 的职责，
// 在 Init 里完成。运行时回调（agent factory / summarySink / 状态栏）由调用方
// 以窄接口或方法值注入，检查器不认识 *Engine。

import (
	"HyperBot/service/engine/config"
	m "HyperBot/service/engine/memory"
	s "HyperBot/service/engine/session"
	functionTools "HyperBot/service/engine/tools/functions"
	"HyperBot/service/engine/tools/toolsets/cronagent"
	"HyperBot/service/engine/tools/toolsets/localexec"
	"embed"
	"fmt"
	stdlog "log"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/memory"
	"trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	mcp "trpc.group/trpc-go/trpc-mcp-go"
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
	sessionDBFileName    string = "session.db"
	outputDir            string = "output"
)

// summarySink 本文件对摘要展示端的全部需求（Engine 以 AppendSummary 实现）。
type summarySink interface {
	AppendSummary(text string)
}

// bootNotice 启动期间收集的非致命提示（absorb 时经 setNotice 上屏）。
type bootNotice struct {
	Kind string
	Text string
}

// BootEnv 环境检查与资产装配的产物，Engine 吸收后即可构造 runner。
type BootEnv struct {
	CWD                string //进程工作目录（os.Getwd()，启动命令时所在的 shell 目录）
	ExeDir             string //可执行文件所在目录，一切资产的锚点（config/log/output 跟随二进制）
	ConfigFolderPath   string
	HyperBotConfigPath string
	SkillFolderPath    string
	LogFile            *os.File
	NeedRestart        bool //首跑创建了默认配置文件：须改完配置重启

	Config          *config.Config
	Systemprompt    string
	SessionService  session.Service
	MemoryService   memory.Service
	BuiltinTools    []tool.Tool
	BuiltinToolsets []tool.ToolSet
	SkillRepo       *skill.FSRepository

	Notices []bootNotice
}

// Check 跑完整的启动检查与资产装配。任何一步失败都返回带步骤语义的
// error，由调用方决定如何呈现（parkWithFatal）。
func Check(agentName string, sink summarySink) (*BootEnv, error) {
	env := &BootEnv{}

	// ① 可执行文件所在目录：一切资产的锚点
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable directory: %w", err)
	}
	env.ExeDir = filepath.Dir(exePath)

	// 进程工作目录：与资产锚点解耦。极端情况下 Getwd 会失败（如启动目录被删），
	// 此时回退到 ExeDir，保证 CWD 永远非空。
	env.CWD, err = os.Getwd()
	if err != nil {
		env.CWD = env.ExeDir
	}

	// ② 配置文件夹：不存在则创建默认的
	env.ConfigFolderPath = filepath.Join(env.ExeDir, HyperBotConfigFolder)
	if _, err := os.Stat(env.ConfigFolderPath); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to check config directory: %w", err)
		}
		if err := os.MkdirAll(env.ConfigFolderPath, os.ModePerm); err != nil {
			return nil, fmt.Errorf("failed to create default config directory: %w", err)
		}
		env.Notices = append(env.Notices, bootNotice{NoticeSuccess, "config folder not found, created default"})
	}

	// ③ 框架日志重定向到文件：尽早静音 stderr——Check 与 TUI 并发启动，重定向越早，
	// sqlite/gse/框架初始化等后续步骤的日志污染 TUI 的窗口越小（失败则静默跳过，保持默认输出）
	env.redirectFrameworkLog()

	// ④ 配置文件：不存在则写入默认模板，本次启动到此为止（须改完配置重启）
	env.HyperBotConfigPath = filepath.Join(env.ConfigFolderPath, HyperBotConfig)
	if _, err := os.Stat(env.HyperBotConfigPath); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to check config file: %w", err)
		}
		fd, err := os.OpenFile(env.HyperBotConfigPath, os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to create default config file: %w", err)
		}
		defer fd.Close()
		cfg := strings.ReplaceAll(config.Template, "{USERID}", uuid.New().String())
		if _, err := fd.WriteString(cfg); err != nil {
			return nil, fmt.Errorf("failed to write default config file: %w", err)
		}
		env.NeedRestart = true
		return env, nil
	}

	// ⑤ skills 文件夹：不存在则创建默认的
	env.SkillFolderPath = filepath.Join(env.ConfigFolderPath, SkillsFolder)
	if _, err := os.Stat(env.SkillFolderPath); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to check skills directory: %w", err)
		}
		if err := os.MkdirAll(env.SkillFolderPath, os.ModePerm); err != nil {
			return nil, fmt.Errorf("failed to create default skills directory: %w", err)
		}
		env.Notices = append(env.Notices, bootNotice{NoticeSuccess, "skills folder not found, created default"})
	}

	// ⑥ 加载配置文件
	cfg, err := config.LoadConfig(env.HyperBotConfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config file: %w", err)
	}
	env.Config = cfg

	// ⑦ 系统提示词（模板占位符替换）
	env.buildSystemPrompt(agentName)

	// ⑧ 会话服务：默认 SQLite（.hyperbot/session.db，与 memory.db 同目录），会话跨进程持久化；
	// 创建失败按原因降级内存服务（降级不阻断启动，原因经 Notices 上屏）
	env.SessionService = env.buildSessionService(sink)

	// ⑨ sqlite 记忆服务
	memoryService, err := m.NewSQLiteMemoryService(filepath.Join(env.ConfigFolderPath, memoryDBFileName))
	if err != nil {
		return nil, fmt.Errorf("failed to initialize sqlite memory service: %w", err)
	}
	env.MemoryService = memoryService

	// ⑩ 内置工具与工具集
	tools, sets, err := builtinAssets(env, agentName)
	if err != nil {
		return nil, err
	}
	env.BuiltinTools = tools
	env.BuiltinToolsets = sets

	// ⑪ 技能仓库（加载失败与旧版一致：忽略错误，交给调用方判空）
	env.SkillRepo, _ = skill.NewFSRepository(env.SkillFolderPath)

	return env, nil
}

// buildSessionService 构建会话服务：默认 SQLite 持久化（<CWD>/.hyperbot/session.db）。
// session 按工作目录归属——分布式设计：不同 CWD 启动的实例各持独立会话空间，同 CWD 的
// 多实例共享同一 db（WAL + busy_timeout + txlock=immediate 支撑多进程并发读写）；
// CWD 取不到时回退 ExeDir，会话落回二进制目录，保证永远有非空锚点。
// 创建失败按原因降级，降级不致命——会话只是从"跨进程持久化"退化为"进程内存"，不阻断启动：
//   - 失败原因是目录不存在（常态：新工作目录首启，CWD/.hyperbot 尚未建立）：创建目录后重试一次；
//     目录创建失败或重试仍失败 → 降级内存会话服务
//   - 其它原因失败 → 直接降级内存会话服务
//
// "是否路径不存在"用 os.Stat 判定而非解析驱动错误文案：mattn/go-sqlite3 的 CantOpen
// 错误不保证能 errors.Is 链到 fs.ErrNotExist，Stat 目录是稳定的判定依据。
func (env *BootEnv) buildSessionService(sink summarySink) session.Service {
	dbPath := filepath.Join(env.CWD, HyperBotConfigFolder, sessionDBFileName)

	svc, err := s.NewSQLiteSessionService(dbPath, env.Config.Model, sink)
	if err == nil {
		return svc
	}

	if _, statErr := os.Stat(filepath.Dir(dbPath)); os.IsNotExist(statErr) {
		if mkErr := os.MkdirAll(filepath.Dir(dbPath), os.ModePerm); mkErr != nil {
			env.Notices = append(env.Notices, bootNotice{NoticeWarning, fmt.Sprintf(
				"sqlite session db directory create failed (%v), fallback to in-memory session service", mkErr)})
			return s.NewMemorySessionService(env.Config.Model, sink)
		}
		svc, err = s.NewSQLiteSessionService(dbPath, env.Config.Model, sink)
		if err == nil {
			return svc
		}
	}

	env.Notices = append(env.Notices, bootNotice{NoticeWarning, fmt.Sprintf(
		"sqlite session service failed (%v), fallback to in-memory session service", err)})
	return s.NewMemorySessionService(env.Config.Model, sink)
}

// builtinAssets 内置 function 工具与工具集（localexec + cron agent）。
func builtinAssets(env *BootEnv, agentName string) ([]tool.Tool, []tool.ToolSet, error) {
	var tools []tool.Tool
	var sets []tool.ToolSet

	sets = append(sets, localexec.LocalExec())

	// 用独立的 agent 名，避免遥测里 cron 的自主运行和主对话混在同一个 (app, agent) 对下
	cronToolset, err := cronagent.CronAgent(
		agentName+"_cron",
		env.Config,
		env.Systemprompt,
		env.SkillFolderPath,
		env.ConfigFolderPath,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize cron agent: %w", err)
	}
	// 存档加载失败是非致命的：坏文件已被挪到 .fix<时间戳>，空集合可以正常启动
	if loadErr := cronToolset.LoadError(); loadErr != nil {
		stdlog.Printf("failed to load cron agent store: %v", loadErr)
		env.Notices = append(env.Notices, bootNotice{NoticeWarning, "cron agent config broken, moved to .fix"})
	}
	sets = append(sets, cronToolset)

	fileopsTools := functionTools.GetFileOperationsTools()
	fileSystemTools := functionTools.GetFileSystemTools()
	dateTools := functionTools.GetDateTools()
	todoTools := functionTools.GetTodoTools() // 框架内置 todo_write：任务清单，状态存 session，跨轮持久化
	tools = append(tools, fileopsTools...)
	tools = append(tools, fileSystemTools...)
	tools = append(tools, dateTools...)
	tools = append(tools, todoTools...)

	return tools, sets, nil
}

// redirectFrameworkLog 将框架/mcp/标准库的日志输出重定向到 hyperbot.log。
func (env *BootEnv) redirectFrameworkLog() {
	logPath := filepath.Join(env.ConfigFolderPath, HyperBotLogFile)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	env.LogFile = logFile
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
	//定向trpc-agent-go的日志输出到文件
	log.Default = fileLogger
	log.ContextDefault = fileLogger

	//定向trpc-mcp-go的日志输出到文件
	mcp.SetDefaultLogger(fileLogger)

	//重定向标准库 log 到文件（避免 gse 等第三方库的日志污染终端）
	stdlog.SetOutput(logFile)
}

// buildSystemPrompt 读取系统提示词模板并替换其中的占位符。
func (env *BootEnv) buildSystemPrompt(agentName string) {
	systemprompt_b, _ := fs.ReadFile("prompt/systemprompt.md")
	prompt := string(systemprompt_b)
	//Agent名称
	prompt = strings.ReplaceAll(prompt, "{{NAME}}", agentName)

	//当前日期（已由 BeforeModel 状态栏 TIMENOW 提供，每次调用刷新）
	//prompt = strings.ReplaceAll(prompt, "{{DATE}}", time.Now().Format("2006-01-02 15:04:05 (Mon)"))

	//当前时区
	zone, _ := time.Now().Zone()
	prompt = strings.ReplaceAll(prompt, "{{TIMEZONE}}", fmt.Sprintf("%s (%s)", time.Now().Location().String(), zone))

	//操作系统
	prompt = strings.ReplaceAll(prompt, "{{OSTYPE}}", runtime.GOOS)

	//CPU架构
	prompt = strings.ReplaceAll(prompt, "{{AARCH}}", runtime.GOARCH)

	//主目录
	homeDir, _ := os.UserHomeDir()
	prompt = strings.ReplaceAll(prompt, "{{HOME}}", homeDir)

	//临时目录
	prompt = strings.ReplaceAll(prompt, "{{TMPDIR}}", os.TempDir())

	//当前用户
	u, _ := user.Current()
	prompt = strings.ReplaceAll(prompt, "{{CURRENTUSER}}", u.Username)

	//主机名
	hostName, _ := os.Hostname()
	prompt = strings.ReplaceAll(prompt, "{{HOSTNAME}}", hostName)

	//运行目录（已由 BeforeModel 状态栏 CWD 提供）
	//prompt = strings.ReplaceAll(prompt, "{{CWD}}", env.CWD)

	//配置目录
	prompt = strings.ReplaceAll(prompt, "{{CONFIGPATH}}", env.ConfigFolderPath)

	//配置文件
	prompt = strings.ReplaceAll(prompt, "{{HyperBotConfig}}", HyperBotConfig)
	prompt = strings.ReplaceAll(prompt, "{{SkillsFolder}}", SkillsFolder)
	prompt = strings.ReplaceAll(prompt, "{{HyperBotLogFile}}", HyperBotLogFile)
	//输出目录（锚定 ExeDir，与 localexec/tooloutput 的 spill 目录保持一致）
	outputPath := filepath.Join(env.ExeDir, outputDir)
	prompt = strings.ReplaceAll(prompt, "{{OUTPUTDIR}}", outputPath)

	//todo_write 工具使用说明（框架 tool/todo.DefaultToolPrompt，随框架版本走，不在提示词里硬编码）
	prompt = strings.ReplaceAll(prompt, "{{TODO_PROMPT}}", functionTools.GetTodoToolPrompt())

	env.Systemprompt = prompt
}
