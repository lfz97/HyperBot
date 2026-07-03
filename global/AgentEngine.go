package global

import (
	"HyperBot/config"
	"embed"
	"trpc.group/trpc-go/trpc-agent-go/runner"

	"os"
	memorysqlite "trpc.group/trpc-go/trpc-agent-go/memory/sqlite"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

type Agentrunner struct {
	Runner runner.Runner
	Stream bool
}

// 定义核心的状态变量
var (
	Config_p            *config.Config           //yaml配置
	Agentname           string                   //Agent名称
	CWD                 string                   //当前工作目录
	ConfigFolderPath    string                   //配置文件夹路径
	HyperBotConfigPath  string                   //配置文件路径
	SkillFolderPath     string                   //技能文件夹路径
	SkillRepo           *skill.FSRepository      //技能仓库
	AgentRunner_p       *Agentrunner             //Runner，全局唯一
	SessionService_p    *inmemory.SessionService //会话服务，包含自动摘要功能
	FrameworkLogFile_p  *os.File                 // 保存日志文件句柄，防止被 GC 回收
	SqliteMemoryService *memorysqlite.Service    // sqlite记忆服务
	//go:embed prompt/*
	PromptFiles embed.FS //提示词嵌入FS

	Systemprompt string         //agent的系统提示词
	Toolsets     []tool.ToolSet //agent挂载的工具集
	Tools        []tool.Tool    //agent挂载的工具

	SessionId string
	RequestId string
)

func AgentEngineRun(initFn, startFn func()) {
	go func() {
		initFn()
		startFn()
	}()
}
