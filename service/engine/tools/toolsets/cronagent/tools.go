package cronagent

import (
	"context"
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

func getTools(m *manager) []tool.Tool {
	toolSets := []tool.Tool{}

	CreateAgentTool := function.NewFunctionTool(
		func(ctx context.Context, req struct {
			Cronexpr string `json:"Cronexpr" jsonschema:"description:Agent执行的周期cron表达式。支持5字段标准Unix cron（分 时 日 月 周，如 0 9 * * * 表示每天9点整），也支持6字段带秒（秒 分 时 日 月 周，如 */30 * * * * * 表示每30秒），以及 @every 5m 这类描述符"`
			Prompt   string `json:"Prompt" jsonschema:"description:对此agent下达的指令"`
		}) (map[string]string, error) {
			id, err := m.Create(req.Cronexpr, req.Prompt)
			if err != nil {
				return nil, fmt.Errorf("agent created error: %w", err)
			}
			return map[string]string{
				"Id": id,
			}, nil
		},
		function.WithName(createAgentToolName),
		function.WithDescription("注册并启动一个Cron Agent，它可以按照cron表达式周期执行，返回agent唯一id"),
	)

	StartAgentTool := function.NewFunctionTool(
		func(ctx context.Context, req struct {
			Id string `json:"Id" jsonschema:"description:agent唯一id"`
		}) (string, error) {
			if err := m.Start(req.Id); err != nil {
				return "", fmt.Errorf("agent id %s started error: %w", req.Id, err)
			}
			return fmt.Sprintf("agent id %s started", req.Id), nil
		},
		function.WithName(startAgentToolName),
		function.WithDescription("启动一个处于暂停状态的agent；agent已在运行时调用无副作用"),
	)

	StopAgentTool := function.NewFunctionTool(
		func(ctx context.Context, req struct {
			Id string `json:"Id" jsonschema:"description:agent唯一id"`
		}) (string, error) {
			if err := m.Stop(req.Id); err != nil {
				return "", fmt.Errorf("agent id %s stopped error: %w", req.Id, err)
			}
			return fmt.Sprintf("agent id %s stopped", req.Id), nil
		},
		function.WithName(stopAgentToolName),
		function.WithDescription("暂停一个处于运行状态的agent，并终止它当前正在执行的那一轮；agent已暂停时调用无副作用"),
	)

	RemoveAgentTool := function.NewFunctionTool(
		func(ctx context.Context, req struct {
			Id string `json:"Id" jsonschema:"description:agent唯一id"`
		}) (string, error) {
			if err := m.Remove(req.Id); err != nil {
				return "", fmt.Errorf("agent id %s removed error: %w", req.Id, err)
			}
			return fmt.Sprintf("agent id %s removed", req.Id), nil
		},
		function.WithName(removeAgentToolName),
		function.WithDescription("移除一个agent，无需先暂停"),
	)

	GetAgentStatusTool := function.NewFunctionTool(
		func(ctx context.Context, req struct {
			Id string `json:"Id" jsonschema:"description:agent唯一id"`
		}) (string, error) {
			status, err := m.Status(req.Id)
			if err != nil {
				return "", err
			}
			return status, nil
		},
		function.WithName(getAgentStatusToolName),
		function.WithDescription("获取一个cron agent的状态"),
	)
	GetAllStatusTool := function.NewFunctionTool(
		func(ctx context.Context, req struct{}) (string, error) {
			status, err := m.StatusAll()
			if err != nil {
				return "", err
			}
			return status, nil
		},
		function.WithName(getAllStatusToolName),
		function.WithDescription("获取全部cron agent的状态"),
	)
	GetAgentOutputTool := function.NewFunctionTool(
		func(ctx context.Context, req struct {
			Id     string `json:"Id" jsonschema:"description:agent唯一id"`
			Window int    `json:"Window" jsonschema:"description:可选：返回末尾 Window 个字符；默认0表示返回全部输出"`
		}) (string, error) {
			result, err := m.Output(req.Id, req.Window)
			if err != nil {
				return "", err
			}
			return result, nil
		},
		function.WithName(getAgentOutputToolName),
		function.WithDescription("获取指定agent最近一轮执行的输出内容"),
	)
	ClearAllTool := function.NewFunctionTool(
		func(ctx context.Context, req struct{}) (string, error) {
			n, err := m.ClearAll()
			if err != nil {
				return "", fmt.Errorf("clear all agents error: %w", err)
			}
			return fmt.Sprintf("已停止并移除全部 %d 个 cron agent，持久化存档已清空", n), nil
		},
		function.WithName(clearAllToolName),
		function.WithDescription("停止并移除全部 cron agent，同时清空持久化存档。此操作不可撤销：重启后这些任务不会恢复。只想停掉单个任务请用 stop 或 remove"),
	)
	toolSets = append(toolSets, CreateAgentTool, StartAgentTool, StopAgentTool, RemoveAgentTool, GetAgentStatusTool, GetAllStatusTool, GetAgentOutputTool, ClearAllTool)
	return toolSets
}
