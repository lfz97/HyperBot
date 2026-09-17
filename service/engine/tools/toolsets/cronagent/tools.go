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
			Cronexpr string `json:"Cronexpr" jsonschema:"description=Cron expression defining how often the agent runs. Supports 5-field standard Unix cron (minute hour day-of-month month day-of-week; 0 9 * * * means 09:00 every day); 6-field cron with seconds (second minute hour day-of-month month day-of-week; */30 * * * * * means every 30 seconds); and descriptors such as @every 5m."`
			Prompt   string `json:"Prompt" jsonschema:"description=The instruction given to this agent."`
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
		function.WithDescription("Register and start a cron agent that runs periodically according to a cron expression; returns the agent's unique ID."),
	)

	StartAgentTool := function.NewFunctionTool(
		func(ctx context.Context, req struct {
			Id string `json:"Id" jsonschema:"description=Unique agent ID."`
		}) (string, error) {
			if err := m.Start(req.Id); err != nil {
				return "", fmt.Errorf("agent id %s started error: %w", req.Id, err)
			}
			return fmt.Sprintf("agent id %s started", req.Id), nil
		},
		function.WithName(startAgentToolName),
		function.WithDescription("Start an agent that is currently paused; calling it on an already running agent has no side effects."),
	)

	StopAgentTool := function.NewFunctionTool(
		func(ctx context.Context, req struct {
			Id string `json:"Id" jsonschema:"description=Unique agent ID."`
		}) (string, error) {
			if err := m.Stop(req.Id); err != nil {
				return "", fmt.Errorf("agent id %s stopped error: %w", req.Id, err)
			}
			return fmt.Sprintf("agent id %s stopped", req.Id), nil
		},
		function.WithName(stopAgentToolName),
		function.WithDescription("Pause a running agent and abort the round it is currently executing; calling it on an already paused agent has no side effects."),
	)

	RemoveAgentTool := function.NewFunctionTool(
		func(ctx context.Context, req struct {
			Id string `json:"Id" jsonschema:"description=Unique agent ID."`
		}) (string, error) {
			if err := m.Remove(req.Id); err != nil {
				return "", fmt.Errorf("agent id %s removed error: %w", req.Id, err)
			}
			return fmt.Sprintf("agent id %s removed", req.Id), nil
		},
		function.WithName(removeAgentToolName),
		function.WithDescription("Remove an agent; it does not need to be paused first."),
	)

	GetAgentStatusTool := function.NewFunctionTool(
		func(ctx context.Context, req struct {
			Id string `json:"Id" jsonschema:"description=Unique agent ID."`
		}) (string, error) {
			status, err := m.Status(req.Id)
			if err != nil {
				return "", err
			}
			return status, nil
		},
		function.WithName(getAgentStatusToolName),
		function.WithDescription("Get the status of a single cron agent."),
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
		function.WithDescription("Get the status of all cron agents."),
	)
	GetAgentOutputTool := function.NewFunctionTool(
		func(ctx context.Context, req struct {
			Id     string `json:"Id" jsonschema:"description=Unique agent ID."`
			Window int    `json:"Window" jsonschema:"description=Optional: return only the last Window characters. Defaults to 0 which returns the entire output."`
		}) (string, error) {
			result, err := m.Output(req.Id, req.Window)
			if err != nil {
				return "", err
			}
			return result, nil
		},
		function.WithName(getAgentOutputToolName),
		function.WithDescription("Get the output of the specified agent's most recent run."),
	)
	ClearAllTool := function.NewFunctionTool(
		func(ctx context.Context, req struct{}) (string, error) {
			n, err := m.ClearAll()
			if err != nil {
				return "", fmt.Errorf("clear all agents error: %w", err)
			}
			return fmt.Sprintf("stopped and removed all %d cron agents; the persisted archive has been cleared", n), nil
		},
		function.WithName(clearAllToolName),
		function.WithDescription("Stop and remove all cron agents and clear the persisted archive. This cannot be undone: these tasks will not come back after a restart. Use stop or remove instead if you only want to stop a single task."),
	)
	toolSets = append(toolSets, CreateAgentTool, StartAgentTool, StopAgentTool, RemoveAgentTool, GetAgentStatusTool, GetAllStatusTool, GetAgentOutputTool, ClearAllTool)
	return toolSets
}
