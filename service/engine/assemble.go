package engine

// assemble 引擎资产装配：Config、系统提示词、会话/记忆服务、内置工具、技能仓库。
// 与 bootstrap 的检查分工：Check 只做路径存在性检查与默认文件落盘；
// 这里在 absorb（路径已拷入引擎字段）之后由引擎创建真正的运行时对象。

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

	"trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

//go:embed prompt/*
var fs embed.FS

// assembleConfig 加载配置文件到引擎（⑥）。
func (e *Engine) assembleConfig() error {
	cfg, err := config.LoadConfig(e.HyperBotConfigPath)
	if err != nil {
		return fmt.Errorf("failed to load config file: %w", err)
	}
	e.Config_p = cfg
	return nil
}

// buildSessionService 构建会话服务：默认 SQLite 持久化（<CWD>/.hyperbot/session.db）。
// 会话按工作目录归属：同 CWD 的多实例共享同一 db（WAL 等参数支撑并发读写），不同 CWD 各自独立。
// 创建失败降级为内存服务（不阻断启动）：目录不存在则建目录重试一次，其余原因直接降级。
// "路径不存在"用 os.Stat 判定而非解析驱动错误：go-sqlite3 的 CantOpen 不保证 errors.Is 到 fs.ErrNotExist。
func (e *Engine) buildSessionService() session.Service {
	dbPath := filepath.Join(e.CWD, HyperBotConfigFolder, sessionDBFileName)

	svc, err := s.NewSQLiteSessionService(dbPath, e.Config_p.Model, e)
	if err == nil {
		return svc
	}

	if _, statErr := os.Stat(filepath.Dir(dbPath)); os.IsNotExist(statErr) {
		if mkErr := os.MkdirAll(filepath.Dir(dbPath), os.ModePerm); mkErr != nil {
			e.setNotice(NoticeWarning, fmt.Sprintf(
				"sqlite session db directory create failed (%v), fallback to in-memory session service", mkErr))
			return s.NewMemorySessionService(e.Config_p.Model, e)
		}
		svc, err = s.NewSQLiteSessionService(dbPath, e.Config_p.Model, e)
		if err == nil {
			return svc
		}
	}

	e.setNotice(NoticeWarning, fmt.Sprintf(
		"sqlite session service failed (%v), fallback to in-memory session service", err))
	return s.NewMemorySessionService(e.Config_p.Model, e)
}

// buildMemoryService 构建 sqlite 记忆服务（⑨）。
func (e *Engine) buildMemoryService() error {
	memoryService, err := m.NewSQLiteMemoryService(filepath.Join(e.ConfigFolderPath, memoryDBFileName))
	if err != nil {
		return fmt.Errorf("failed to initialize sqlite memory service: %w", err)
	}
	e.SqliteMemoryService = memoryService
	return nil
}

// buildBuiltinAssets 内置 function 工具与工具集（localexec + cron agent）（⑩）。
func (e *Engine) buildBuiltinAssets() error {
	var tools []tool.Tool
	var sets []tool.ToolSet

	sets = append(sets, localexec.LocalExec())

	// 用独立的 agent 名，避免遥测里 cron 的自主运行和主对话混在同一个 (app, agent) 对下
	cronToolset, err := cronagent.CronAgent(
		e.Agentname+"_cron",
		e.Config_p,
		e.Systemprompt,
		e.SkillFolderPath,
		e.ConfigFolderPath,
	)
	if err != nil {
		return fmt.Errorf("failed to initialize cron agent: %w", err)
	}
	// 存档加载失败是非致命的：坏文件已被挪到 .fix<时间戳>，空集合可以正常启动
	if loadErr := cronToolset.LoadError(); loadErr != nil {
		stdlog.Printf("failed to load cron agent store: %v", loadErr)
		e.setNotice(NoticeWarning, "cron agent config broken, moved to .fix")
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

	e.builtinTools = tools
	e.builtinToolsets = sets
	return nil
}

// buildSystemPrompt 读取系统提示词模板并替换其中的占位符（⑦）。
func (e *Engine) buildSystemPrompt() {
	systemprompt_b, _ := fs.ReadFile("prompt/systemprompt.md")
	prompt := string(systemprompt_b)
	prompt = strings.ReplaceAll(prompt, "{{NAME}}", e.Agentname)
	// {{DATE}}/{{CWD}} 不注入：已由 BeforeModel 状态栏每次调用刷新提供
	zone, _ := time.Now().Zone()
	prompt = strings.ReplaceAll(prompt, "{{TIMEZONE}}", fmt.Sprintf("%s (%s)", time.Now().Location().String(), zone))

	prompt = strings.ReplaceAll(prompt, "{{OSTYPE}}", runtime.GOOS)

	prompt = strings.ReplaceAll(prompt, "{{AARCH}}", runtime.GOARCH)

	homeDir, _ := os.UserHomeDir()
	prompt = strings.ReplaceAll(prompt, "{{HOME}}", homeDir)

	prompt = strings.ReplaceAll(prompt, "{{TMPDIR}}", os.TempDir())

	u, _ := user.Current()
	prompt = strings.ReplaceAll(prompt, "{{CURRENTUSER}}", u.Username)

	hostName, _ := os.Hostname()
	prompt = strings.ReplaceAll(prompt, "{{HOSTNAME}}", hostName)

	prompt = strings.ReplaceAll(prompt, "{{CONFIGPATH}}", e.ConfigFolderPath)

	prompt = strings.ReplaceAll(prompt, "{{HyperBotConfig}}", HyperBotConfig)
	prompt = strings.ReplaceAll(prompt, "{{SkillsFolder}}", SkillsFolder)
	prompt = strings.ReplaceAll(prompt, "{{HyperBotLogFile}}", HyperBotLogFile)
	//输出目录（锚定 ExeDir，与 localexec/tooloutput 的 spill 目录保持一致）
	outputPath := filepath.Join(e.ExeDir, outputDir)
	prompt = strings.ReplaceAll(prompt, "{{OUTPUTDIR}}", outputPath)

	//todo_write 工具使用说明（框架 tool/todo.DefaultToolPrompt，随框架版本走，不在提示词里硬编码）
	prompt = strings.ReplaceAll(prompt, "{{TODO_PROMPT}}", functionTools.GetTodoToolPrompt())

	e.Systemprompt = prompt
}
