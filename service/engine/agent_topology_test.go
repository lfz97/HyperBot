package engine

// 主 agent 与 Worker 的同源关系：两者必须挂同一套执行工具，Worker 只少掉
// 记忆 / todo / 调度这类"状态与权限"，不能少掉干活的能力。

import (
	"strings"
	"testing"

	"HyperBot/service/engine/config"
	hyperbotsession "HyperBot/service/engine/session"
	functionTools "HyperBot/service/engine/tools/functions"
	"HyperBot/service/engine/tools/toolsets/localexec"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/taskrun"
)

func newAgentTopologyEngine(t *testing.T) *Engine {
	m := config.Model{
		APIType:       "openai",
		Model:         "test-model",
		MaxTokens:     1024,
		ContextWindow: 8192,
	}
	e := &Engine{
		Agentname:    "HyperBot",
		Config_p:     &config.Config{Model: m},
		Systemprompt: "MAIN PROMPT",
		WorkerPrompt: "WORKER PROMPT",
	}
	// SkillRepo 用空临时目录：装配路径对非 nil repo 走真实分支（WithSkills 挂载
	// 的 skill 工具在 Declaration 时会读 repo，nil 会 panic——生产路径由
	// buildAgent 的判空守卫兜底，这里不测那个分支）。
	repo, err := skill.NewFSRepository(t.TempDir())
	if err != nil {
		t.Fatalf("skill repo: %v", err)
	}
	e.SkillRepo = repo
	// 框架只在配置了 session service 时才暴露 read_task_run_transcript，
	// 与生产装配（newRunner 里 WithSessionService）保持同构。
	taskTools := taskrun.NewTools(
		nil,
		taskrun.WithDefaultAgentName(workerAgentName),
		taskrun.WithSessionService(hyperbotsession.NewMemorySessionService(m, nil)),
	)
	e.taskTools = &taskTools
	e.builtinToolsets = []tool.ToolSet{localexec.LocalExec()}
	e.builtinTools = append(
		[]tool.Tool{},
		functionTools.GetFileOperationsTools()...,
	)
	e.builtinTools = append(
		e.builtinTools,
		functionTools.GetFileSystemTools()...,
	)
	return e
}

func toolNameSet(tools []tool.Tool) map[string]bool {
	names := make(map[string]bool, len(tools))
	for _, t := range tools {
		if decl := t.Declaration(); decl != nil {
			names[decl.Name] = true
		}
	}
	return names
}

func TestMainAndWorkerShareExecutionTools(t *testing.T) {
	e := newAgentTopologyEngine(t)

	main, err := e.initMainAgent()
	if err != nil {
		t.Fatalf("initMainAgent: %v", err)
	}
	worker, err := e.initWorker()
	if err != nil {
		t.Fatalf("initWorker: %v", err)
	}

	if got := main.Info().Name; got != "HyperBot" {
		t.Fatalf("main agent name=%q, want %q", got, "HyperBot")
	}
	if got := worker.Info().Name; got != workerAgentName {
		t.Fatalf("worker name=%q, want %q", got, workerAgentName)
	}

	mainTools := toolNameSet(main.Tools())
	wkTools := toolNameSet(worker.Tools())

	// 干活的能力：两者都有
	for _, name := range []string{
		"CommandRun", "CommandStatus", "CommandOutput", "CommandIntervene", "CommandKill",
	} {
		if !mainTools[name] {
			t.Errorf("main agent missing execution tool %q", name)
		}
		if !wkTools[name] {
			t.Errorf("worker missing execution tool %q", name)
		}
	}

	// 状态与权限：只有主 agent 有
	for _, name := range []string{
		"todo_write", "start_task_run", "list_task_runs", "get_task_run",
		"wait_task_run", "cancel_task_run", "read_task_run_transcript",
	} {
		if !mainTools[name] {
			t.Errorf("main agent missing %q", name)
		}
		if wkTools[name] {
			t.Errorf("worker must not have %q", name)
		}
	}

	// Worker 多出来的工具 = 0：它是主 agent 去掉状态与调度后的同一份能力
	for name := range wkTools {
		if !mainTools[name] {
			t.Errorf("worker has tool %q that main agent does not", name)
		}
	}
}

func TestAgentPromptsResolveAllPlaceholders(t *testing.T) {
	e := newAgentTopologyEngine(t)
	e.ConfigFolderPath = ".hyperbot"
	e.ExeDir = "/tmp/hyperbot-test"

	e.buildSystemPrompt()
	e.buildWorkerPrompt()

	for name, prompt := range map[string]string{
		"systemprompt.md": e.Systemprompt,
		"workerprompt.md": e.WorkerPrompt,
	} {
		if strings.Contains(prompt, "{{") {
			t.Errorf("%s still contains an unresolved placeholder", name)
		}
		if !strings.Contains(prompt, "HyperBot") {
			t.Errorf("%s: {{NAME}} was not substituted", name)
		}
	}
}
