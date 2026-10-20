package engine

import (
	"HyperBot/service/engine/agent"
	"HyperBot/service/engine/config"
	"HyperBot/service/engine/tools/toolsets"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	ag "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/tiktoken"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

type Engine struct {
	Config_p            *config.Config      //yaml配置
	Agentname           string              //Agent名称
	CWD                 string              //当前工作目录（os.Getwd()）
	ExeDir              string              //可执行文件所在目录，资产锚点
	ConfigFolderPath    string              //配置文件夹路径
	HyperBotConfigPath  string              //配置文件路径
	SkillFolderPath     string              //技能文件夹路径
	SkillRepo           *skill.FSRepository //技能仓库
	AgentRunner_p       *Agentrunner        //Runner，全局唯一
	SessionService_p    session.Service     //会话服务，包含自动摘要功能
	FrameworkLogFile_p  *os.File            // 保存日志文件句柄，防止被 GC 回收
	SqliteMemoryService memory.Service      // sqlite记忆服务
	Systemprompt        string              //agent的系统提示词
	mcpToolsets         []tool.ToolSet      //agent挂载的工具集，每轮自动更新
	builtinTools        []tool.Tool         //内置function清单，启动时确定，不自动刷新
	builtinToolsets     []tool.ToolSet      //内置工具集，启动时确定，不自动刷新

	// errBudget：连续错误自动重试预算，状态流转见 errorBudget.go
	errBudget errorBudget

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
	inputCh   chan string

	interruptCh chan struct{}
}

type Agentrunner struct {
	Runner    runner.Runner
	Stream    bool
	SessionId string
}

func (e *Engine) newRunner() {
	var Runner runner.Runner
	Runner = runner.NewRunnerWithAgentFactory(
		(*e).Agentname,
		(*e).Agentname,
		func(ctx context.Context, ro ag.RunOptions) (ag.Agent, error) {
			(*e).refresh()
			var Agent_p *llmagent.LLMAgent
			toolsets := []tool.ToolSet{}
			tools := []tool.Tool{}
			toolsets = append(toolsets, (*e).builtinToolsets...)
			toolsets = append(toolsets, (*e).mcpToolsets...)
			tools = append(tools, (*e).builtinTools...)
			tools = append(tools, (*e).SqliteMemoryService.Tools()...) //记忆工具加入全局工具列表，Agent 才能调用
			counter, _ := tiktoken.New((*(*e).Config_p).Model.Model)   //tiktoken 计数器：compaction 阈值判定与 max_tokens 钳制共用；失败为 nil，各自 fail-open
			opts := []llmagent.Option{
				llmagent.WithGenerationConfig(model.GenerationConfig{
					MaxTokens:       &(*(*e).Config_p).Model.MaxTokens,
					Stream:          (*(*e).Config_p).Model.Stream,
					ReasoningEffort: (*(*e).Config_p).Model.ReasoningEffortPtr(), // 思考强度，来自配置 reasoning_effort 字段，留空不下发
				}), llmagent.WithTools(tools),
				llmagent.WithGlobalInstruction((*e).Systemprompt),
				llmagent.WithToolSets(toolsets),
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
				llmagent.WithContextCompactionToolResultMaxTokens(8192),                        // Pass 1 预算：历史 tool result >8K 才换 placeholder（默认1024过激：实测真实结果 p50=191/p99=3.7K，8192 覆盖99.7%；与 Pass 2 对齐成一条规则：≤8K 原文，>8K 首尾截断）
				llmagent.WithContextCompactionOversizedToolResultMaxTokens(8192),               // Pass 2: 超大 tool result 首尾保留截断
				llmagent.WithEnableOnDemandSession(true),                                       // 按需加载被压缩的原始数据（session_load）
				llmagent.WithContextCompactionTokenCounter(counter),                            //裁剪阈值判定用精确计数：默认 rune/4 对中文低估 3~4 倍，会漏裁
				llmagent.WithEnableParallelTools(true),                                         //启用并行工具调用，提升工具调用效率
				agent.SetBeforeModelStatusCallback(e, &agent.TokenClampConfig{
					Counter:       counter,
					ContextWindow: (*(*e).Config_p).Model.ContextWindow,
					CfgMaxTokens:  (*(*e).Config_p).Model.MaxTokens,
				}), //追加beforeModel状态栏 + 动态max_tokens钳制（公式见 agent/maxtokens.go）
			}
			// ConfigBaseAgent 对未知 APIType 是静默不设模型（agent 建得出来、跑起来才失败），
			// 这里先挡住，给出可读的配置错误。
			apiType := (*(*e).Config_p).Model.APIType
			if apiType != "openai" && apiType != "anthropic" {
				return nil, errors.New("unsupported API type, check the Model.APIType field in your config")
			}
			Agent_p = agent.ConfigBaseAgent(
				(*e).Agentname,
				(*(*e).Config_p).Model,
				opts,
			)
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

// 刷新配置文件，MCP工具集，SKILL仓库
func (e *Engine) refresh() {
	e.loadConfig()
	e.refreshMCPFromConfig()
	e.loadSkills()
}

func (e *Engine) loadConfig() {
	c, err := config.LoadConfig((*e).HyperBotConfigPath)
	if err != nil {
		(*e).parkWithFatal(FatalError, fmt.Sprintf("Failed to load config: %v (press any key to exit)", err), true)
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
