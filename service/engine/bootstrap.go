package engine

// bootstrap 环境检查器：只做检查与供给，返回错误和产物；不驻留、不碰 UI。
// 致命呈现与通知上屏是 Engine（Init）的职责；运行时回调由调用方注入，检查器不认识 *Engine。

import (
	"HyperBot/service/engine/config"
	"fmt"
	stdlog "log"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"trpc.group/trpc-go/trpc-agent-go/log"
	mcp "trpc.group/trpc-go/trpc-mcp-go"
)

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

// bootNotice 启动期间收集的非致命提示（absorb 时经 setNotice 上屏）。
type bootNotice struct {
	Kind string
	Text string
}

// BootEnv 环境检查的产物：只含路径、日志句柄与非致命提示；
// 运行时对象（Config/服务/工具/技能仓库）由 Engine 在 absorb 后装配（见 assemble.go）。
type BootEnv struct {
	CWD                string //进程工作目录
	ExeDir             string //可执行文件目录，资产锚点（config/log/output 跟随二进制）
	ConfigFolderPath   string
	HyperBotConfigPath string
	SkillFolderPath    string
	LogFile            *os.File
	ConfigCreated      bool //首跑创建了默认配置文件：进入首次启动配置流程（TUI 配置页收集模型配置）

	Notices []bootNotice
}

// Check 跑完整的启动检查：路径存在性、默认配置/技能目录落盘、日志重定向。
// 不实例化任何运行时对象。任何一步失败都返回带步骤语义的
// error，由调用方决定如何呈现（parkWithFatal）。
func Check() (*BootEnv, error) {
	env := &BootEnv{}

	// ① 可执行文件所在目录：一切资产的锚点
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable directory: %w", err)
	}
	env.ExeDir = filepath.Dir(exePath)

	// Getwd 失败（如启动目录被删）时回退 ExeDir，保证 CWD 非空。
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

	// ③ 框架日志重定向到文件：Check 与 TUI 并发启动，越早静音 stderr，日志污染 TUI 的窗口越小
	env.redirectFrameworkLog()

	// ④ 配置文件：不存在则写入默认模板，本次启动进入首次启动配置流程
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
		env.ConfigCreated = true
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

	return env, nil
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
