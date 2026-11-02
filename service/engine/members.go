package engine

// members.go 主 agent 与 Worker 的装配路径。两者是同一份实现的两个实例而非两套
// 代码：共用模型参数、内置工具、执行工具集、技能库与上下文压缩策略——差别只有
// 提示词、专属工具（记忆/todo/taskrun 调度面只给主 agent）与状态栏开关。
// 给主 agent 加工具或改压缩参数，Worker 自动同步。

import (
	"errors"
	"fmt"

	"HyperBot/service/engine/agent"
	functionTools "HyperBot/service/engine/tools/functions"

	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/tiktoken"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// workerAgentName 是 taskrun 派发的执行 agent 名（runner 具名注册，见 newRunner）。
const workerAgentName = "Worker"

// agentRole 是一次 agent 装配里"有差异"的部分。
type agentRole struct {
	name        string
	prompt      string
	description string

	// extraTools 只给主 agent：记忆、todo、taskrun 调度面。
	// Worker 一个都不挂——它调不动别人，也不维护跨轮状态。
	extraTools []tool.Tool

	// 状态栏回调：主 agent 全开（todo/记忆/在途任务），Worker 只留基础状态栏。
	statusOpt llmagent.Option
}

// buildAgent 装配主 agent 与 Worker 的唯一入口。
func (e *Engine) buildAgent(role agentRole) (*llmagent.LLMAgent, error) {
	// ConfigBaseAgent 对未知 APIType 是静默不设模型（agent 建得出来、跑起来才失败），
	// 这里先挡住，给出可读的配置错误。
	apiType := (*(*e).Config_p).Model.APIType
	if apiType != "openai" && apiType != "anthropic" {
		return nil, errors.New("unsupported API type, check the Model.APIType field in your config")
	}

	tools := make([]tool.Tool, 0, len((*e).builtinTools)+len(role.extraTools))
	tools = append(tools, (*e).builtinTools...)
	tools = append(tools, role.extraTools...)

	// 执行工具集与 MCP 工具集是共享实例：同一份作业表、同一批 MCP 子进程。
	toolsets := make([]tool.ToolSet, 0, 1+len((*e).mcpToolsets))
	toolsets = append(toolsets, (*e).builtinToolsets...)
	toolsets = append(toolsets, (*e).mcpToolsets...)

	// tiktoken 计数器：compaction 阈值判定与 max_tokens 钳制共用；失败为 nil，各自 fail-open
	counter, _ := tiktoken.New((*(*e).Config_p).Model.Model)

	opts := []llmagent.Option{
		llmagent.WithGenerationConfig(model.GenerationConfig{
			MaxTokens:       &(*(*e).Config_p).Model.MaxTokens,
			Stream:          (*(*e).Config_p).Model.Stream,
			ReasoningEffort: (*(*e).Config_p).Model.ReasoningEffortPtr(), // 思考强度，来自配置 reasoning_effort 字段，留空不下发
		}), llmagent.WithTools(tools),
		llmagent.WithGlobalInstruction(role.prompt),
		llmagent.WithToolSets(toolsets),
		llmagent.WithRefreshToolSetsOnRun(true),
		llmagent.WithSkillsLoadedContentInToolResults(true),
		llmagent.WithAddSessionSummary(true),                                           //启用上下文压缩注入
		llmagent.WithSessionSummaryInjectionMode(llmagent.SessionSummaryInjectionUser), //摘要注入到user message，不与system prompt中的SOP规则竞争优先级
		llmagent.WithSyncSummaryIntraRun(true),                                         //在同一次对话中同步更新摘要
		llmagent.WithEnableContextCompaction(true),                                     // 启用 tool result 压缩（Pass 1+2）
		llmagent.WithContextCompactionToolResultMaxTokens(8192),                        // Pass 1 预算：历史 tool result >8K 才换 placeholder（默认1024过激：实测真实结果 p50=191/p99=3.7K，8192 覆盖99.7%；与 Pass 2 对齐成一条规则：≤8K 原文，>8K 首尾截断）
		llmagent.WithContextCompactionOversizedToolResultMaxTokens(8192),               // Pass 2: 超大 tool result 首尾保留截断
		llmagent.WithEnableOnDemandSession(true),                                       // 按需加载被压缩的原始数据（session_load）
		llmagent.WithContextCompactionTokenCounter(counter),                            //裁剪阈值判定用精确计数：默认 rune/4 对中文低估 3~4 倍，会漏裁
		llmagent.WithEnableParallelTools(true),                                         //启用并行工具调用，提升工具调用效率
	}
	if (*e).SkillRepo != nil {
		// SkillRepo 为 nil 时必须跳过（loadSkills 忽略了 NewFSRepository 的错误）：
		// WithSkills(nil) 仍会挂载 skill 工具，其 Declaration 在 nil repo 上直接 panic。
		opts = append(opts,
			llmagent.WithSkills((*e).SkillRepo),
			//仅注入知识，不注入执行工具的能力，统一通过localexec执行
			llmagent.WithSkillToolProfile(llmagent.SkillToolProfileKnowledgeOnly),
		)
	}
	if role.description != "" {
		opts = append(opts, llmagent.WithDescription(role.description))
	}
	opts = append(opts, role.statusOpt)
	return agent.ConfigBaseAgent(
		role.name,
		(*(*e).Config_p).Model,
		opts,
	), nil
}

// initMainAgent 装配主 agent：与用户对话、自己执行、按需派 Worker。
func (e *Engine) initMainAgent() (*llmagent.LLMAgent, error) {
	extra := make([]tool.Tool, 0, 16)
	if (*e).SqliteMemoryService != nil {
		extra = append(extra, (*e).SqliteMemoryService.Tools()...) //记忆工具加入全局工具列表，Agent 才能调用
	}
	extra = append(extra, functionTools.GetTodoTools()...) // 框架内置 todo_write：计划只由主 agent 持有
	if (*e).taskTools != nil {
		extra = append(extra, (*e).taskTools.All()...) // start/list/get/wait/cancel/read_task_run_transcript
	}

	// 状态栏 + 动态max_tokens钳制（公式见 agent/maxtokens.go）。
	// display 与 flight 都是 e：Engine 同时实现 SetTodoText（uistate.go）与
	// AppendFlightStatus（flight.go），flight 非 nil 才注入在途任务摘要。
	counter, _ := tiktoken.New((*(*e).Config_p).Model.Model)
	clamp := &agent.TokenClampConfig{
		Counter:       counter,
		ContextWindow: (*(*e).Config_p).Model.ContextWindow,
		CfgMaxTokens:  (*(*e).Config_p).Model.MaxTokens,
	}
	return e.buildAgent(agentRole{
		name:        (*e).Agentname,
		prompt:      (*e).Systemprompt,
		description: fmt.Sprintf("%s — interactive command-line assistant: talks to the user, executes directly, and dispatches self-contained background work to Workers via task runs.", (*e).Agentname),
		extraTools:  extra,
		statusOpt:   agent.SetBeforeModelStatusCallback(e, e, clamp),
	})
}

// initWorker 装配后台任务执行者：同一套工具、同一份技能库、同一个模型，
// 但没有记忆、没有 todo、没有调度权——它只把手里那一件事做完并交回报告。
func (e *Engine) initWorker() (*llmagent.LLMAgent, error) {
	// Worker 只注入基础状态栏（时间/工作目录/内存）+ max_tokens 钳制：
	// 不读主对话的 todo、不查记忆、不关心在途的兄弟任务。
	counter, _ := tiktoken.New((*(*e).Config_p).Model.Model)
	clamp := &agent.TokenClampConfig{
		Counter:       counter,
		ContextWindow: (*(*e).Config_p).Model.ContextWindow,
		CfgMaxTokens:  (*(*e).Config_p).Model.MaxTokens,
	}
	return e.buildAgent(agentRole{
		name:        workerAgentName,
		prompt:      (*e).WorkerPrompt,
		description: "Worker — stateless background execution agent. Give it one self-contained task; it executes with the shared tools and returns a final report. No memory, no delegation, no follow-up questions.",
		statusOpt:   agent.SetWorkerStatusCallback(clamp),
	})
}
