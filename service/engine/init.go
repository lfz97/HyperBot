package engine

import (
	"HyperBot/service/engine/config"
	"HyperBot/service/engine/tools/toolsets"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"HyperBot/service/engine/observability"
	ag "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/agent/taskrun/inprocess"
	"trpc.group/trpc-go/trpc-agent-go/memory"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	taskruntask "trpc.group/trpc-go/trpc-agent-go/tool/taskrun"
)

type Engine struct {
	Config_p            *config.Config                //yaml配置
	Agentname           string                        //Agent名称
	CWD                 string                        //当前工作目录（os.Getwd()）
	ExeDir              string                        //可执行文件所在目录，资产锚点
	ConfigFolderPath    string                        //配置文件夹路径
	HyperBotConfigPath  string                        //配置文件路径
	SkillFolderPath     string                        //技能文件夹路径
	SkillRepo           *skill.FSRepository           //技能仓库
	AgentRunner_p       *Agentrunner                  //Runner，全局唯一
	SessionService_p    session.Service               //会话服务，包含自动摘要功能
	FrameworkLogFile_p  *os.File                      // 保存日志文件句柄，防止被 GC 回收
	SqliteMemoryService memory.Service                // sqlite记忆服务
	ob                  observability.ObserverService //可观测服务
	Systemprompt        string                        //agent的系统提示词
	WorkerPrompt        string                        //Worker（后台任务 agent）的系统提示词
	mcpToolsets         []tool.ToolSet                //agent挂载的工具集，每轮自动更新
	builtinTools        []tool.Tool                   //内置function清单，启动时确定，不自动刷新
	builtinToolsets     []tool.ToolSet                //内置工具集，启动时确定，不自动刷新

	// taskrun 后台任务面：主 agent 经 taskTools 派 Worker，每个 Worker 跑在
	// 独立 child session 里。worker 是原子发布的当代实例——工厂只做原子读，
	// 构造永远发生在 refresh（主 goroutine）上，并发 spawn 不会重复 reload。
	taskService *inprocess.Service
	taskTools   *taskruntask.Tools
	worker      atomic.Pointer[llmagent.LLMAgent]

	// retry：重试控制器。构造期不注入——每轮 turn 由 SubmitInput 重铸
	// （跨轮零残留），状态流转见 retry_controller.go
	retry retryController

	// 启动状态（BootXxx 常量，经 FirstStartupJSON 暴露给 TUI）：
	// booting → （首次启动）config → ready，或 booting → ready。
	// firstStartupFields 是 config 态下配置页的预填值快照。
	// 两个字的写入只经 setBootState（同一临界区成对写），别处裸写即数据竞争。
	bootState          string
	firstStartupFields map[string]string

	// ── 对上层 UI 暴露的可观察状态（pull 契约，方法见 uistate.go）──
	mu        sync.Mutex // 串行化引擎各 goroutine 的写入与 UI goroutine 的读取
	records   []string
	version   uint64
	runState  RunState
	todoText  string
	notice    notice
	startup   []string
	startupOK bool
	skills    []SkillItem

	// ── turn 生命周期记账（e.mu 守护，成对读写在 uistate.go 的 helper）──
	// curRequestID 是当前 turn 的 requestID（请求粒度）：由 SubmitInput 铸造、
	// 错误重试沿用同一个。Interrupt 据此调 runner.Cancel 精确终止在途 run。
	// 取消意图与重试观察态在 retry（retry_controller.go），不在此处。
	curRequestID string
}

type Agentrunner struct {
	// ManagedRunner 而非基础 Runner：engine 的中断链路（Interrupt → Cancel）
	// 需要 requestID 级控制原语。runner 由 newRunner 自构造（具体类型必然
	// 实现全部能力接口），无需逗号断言与降级路径（对比 openclaw gateway 的
	// 外部注入场景）。
	Runner    runner.ManagedRunner
	Stream    bool
	SessionId string
}

// newRunner 构造 runner（双具名 agent）与 taskrun 后台任务面。返回 error 由
// 调用方按语境处理：Init 直接上抛（boot 同步链路），ApplyFirstStartupConfig
// setFatal 后返回（TUI 渲染终态——不能驻留 TUI 事件循环 goroutine）。
func (e *Engine) newRunner() error {
	Runner := runner.NewRunnerWithAgentFactory(
		(*e).Agentname,
		(*e).Agentname,
		func(ctx context.Context, ro ag.RunOptions) (ag.Agent, error) {
			if err := (*e).refresh(); err != nil {
				return nil, err
			}
			return e.initMainAgent()
		},
		// Worker 用独立工厂，且不 refresh：它返回 refresh 已经构造好的那一代实例。
		// 这样 worker 的构造永远发生在 refresh 的同一 goroutine 上，spawn 只做一次原子读，
		// 不会出现 N 个并发 spawn 各自重载配置/刷新 MCP。
		runner.WithAgentFactory(workerAgentName, func(ctx context.Context, ro ag.RunOptions) (ag.Agent, error) {
			worker := (*e).worker.Load()
			if worker == nil {
				return nil, errors.New("worker is not initialized")
			}
			return worker, nil
		}),
		runner.WithSessionService((*e).SessionService_p),
		runner.WithMemoryService((*e).SqliteMemoryService),
	)
	// 自构造的 runner 必然实现 ManagedRunner；逗号断言失败=框架契约回归，
	// 走 error 而非 panic（TUI 侧有装配错误的呈现路径）。
	mr, ok := Runner.(runner.ManagedRunner)
	if !ok {
		return errors.New("runner does not implement ManagedRunner (framework contract regression)")
	}
	(*e).AgentRunner_p = &Agentrunner{
		Runner: mr,
		Stream: (*(*e).Config_p).Model.Stream,
	}

	// agent 级后台任务：主 agent 用它派 Worker，每个 Worker 跑在自己的 child session 里。
	// 必须在 runner 之后构造——Service 直接持有 runner。
	service, err := inprocess.NewService(Runner)
	if err != nil {
		return fmt.Errorf("failed to initialize task run service: %w", err)
	}
	// 不能挂 turn 的 ctx：turn 结束不该杀掉还在跑的 run。
	service.Start(context.Background())
	tools := taskruntask.NewTools(
		service,
		taskruntask.WithDefaultAgentName(workerAgentName),
		taskruntask.WithSessionService((*e).SessionService_p), // 打开 read_task_run_transcript
	)
	(*e).taskService = service
	(*e).taskTools = &tools
	return nil
}

// 刷新配置文件，MCP工具集，SKILL仓库，并重建当代 Worker（原子发布）。
// 返回 error 时由调用方（agent 工厂）转发给 runner，按错误预算重试/交还用户。
func (e *Engine) refresh() error {
	e.loadConfig()
	e.refreshMCPFromConfig()
	e.loadSkills()
	worker, err := e.initWorker()
	if err != nil {
		return err
	}
	(*e).worker.Store(worker)
	return nil
}

// loadConfig 热重载配置：软失败——加载失败保留上一代配置并告警，不打断本轮。
// 理由：配置损坏通常是"改了一半/写坏"，按观测后端同款软失败处理（对比启动/
// 重装配路径的 assembleConfig 硬失败 error 链路，见 Init/ApplyFirstStartupConfig）
// ——本轮沿用上一代配置继续跑，用户看到告警；修好后下一轮 refresh 自动生效。
// 零反馈不可接受：静默沿用旧配置会让人以为改动已生效。
func (e *Engine) loadConfig() {
	c, err := config.LoadConfig((*e).HyperBotConfigPath)
	if err != nil {
		(*e).setNotice(NoticeWarning, fmt.Sprintf("config reload failed, using previous config: %v", err))
		return
	}
	(*e).Config_p = c
}

func (e *Engine) loadMCPFromConfig() []tool.ToolSet {
	latestmcps := []tool.ToolSet{}
	idx := 0
	if len((*(*e).Config_p).HttpMcp) != 0 {
		for _, mcpConfig := range (*(*e).Config_p).HttpMcp {
			if mcpConfig.Enabled == true {
				if mcpConfig.Name == "" { //未配置名称时分配默认名，避免工具前缀冲突
					mcpConfig.Name = fmt.Sprintf("mcp_%d", idx)
				}
				mcpToolSet := toolsets.HttpMCP(mcpConfig)
				latestmcps = append(latestmcps, mcpToolSet)
				idx++
			}

		}
	}
	if len((*(*e).Config_p).StdinMcp) != 0 {
		for _, stdinMcpConfig := range (*(*e).Config_p).StdinMcp {
			if stdinMcpConfig.Enabled == true {
				if stdinMcpConfig.Name == "" {
					stdinMcpConfig.Name = fmt.Sprintf("mcp_%d", idx)
				}
				stdinMcpToolSet := toolsets.StdinMCP(stdinMcpConfig)
				latestmcps = append(latestmcps, stdinMcpToolSet)
				idx++
			}
		}
	}
	return latestmcps
}

// diffMCPtoolsets 增量对齐 MCP 工具集：同名复用（stdio 子进程/HTTP 连接不重启）、
// 新增启用、消失关闭。不比较配置内容——同 name 改 endpoint/args 不生效，
// 需改名或先禁用再启用触发重启（行为契约由 mcp_test.go 锁定）。
func (e *Engine) diffMCPtoolsets(latestmcps []tool.ToolSet) {

	common := []tool.ToolSet{}
	new := []tool.ToolSet{}
	for _, latestItem := range latestmcps {
		for _, runningItem := range (*e).mcpToolsets {
			if runningItem.Name() == latestItem.Name() {
				common = append(common, runningItem)
			}
		}
	}

	//无共同清单：当前的全关闭，直接以新清单为准。
	//if/else 互斥不能省：common 为空时 else 分支的循环一次都匹配不上，Close 永不发生（泄漏）。
	if len(common) == 0 {
		for _, ts := range (*e).mcpToolsets {
			ts.Close()
		}
		(*e).mcpToolsets = latestmcps

	} else {
		//遍历完共同清单都没匹配上 → 当前清单独有，关闭
		for _, running := range (*e).mcpToolsets {
			for i := 0; i < len(common); i++ {
				if common[i].Name() == running.Name() {
					break
				}
				if i == len(common)-1 {
					running.Close()
				}
			}
		}
		//遍历完共同清单都没匹配上 → 新增，追加到 new
		for _, latest := range latestmcps {
			for i := 0; i < len(common); i++ {
				if common[i].Name() == latest.Name() {
					break
				}
				if i == len(common)-1 {
					new = append(new, latest)
				}
			}
		}
		new = append(new, common...)
		(*e).mcpToolsets = new
	}

}

func (e *Engine) refreshMCPFromConfig() {
	//loadMCPFromConfig 新造的实例全是惰性的（未连接），diff 中未被启用的
	//直接丢弃即可，无需 Close；Close 只发生在旧实例离开的分支。
	e.diffMCPtoolsets(e.loadMCPFromConfig())
}

// loadSkills 重建技能仓库并发布技能清单（refresh 每回合调用）。
func (e *Engine) loadSkills() {
	(*e).SkillRepo, _ = skill.NewFSRepository((*e).SkillFolderPath)
	e.publishSkillItems()
}

// publishSkillItems 把技能仓库摘要发布到可观察状态。
func (e *Engine) publishSkillItems() {
	if (*e).SkillRepo == nil {
		return
	}
	summaries := (*e).SkillRepo.Summaries()
	itms := []SkillItem{}
	for _, s := range summaries {
		des := strings.ReplaceAll(s.Description, "\n", " ")
		itms = append(itms, SkillItem{
			Cmd:  "/" + s.Name,
			Desc: des,
		})
	}
	(*e).setSkillItems(itms)
}
