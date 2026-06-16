# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build Commands

```bash
# Run directly
go run .

# Build for current platform (Linux)
./build.sh          # → release/HyperBot

# Build for current platform (Windows PowerShell)
.\build.ps1         # → release/HyperBot.exe

# Build manually
go build -ldflags "-s -w" -buildvcs=false .
```

**CGO required** — `memory/sqlite` depends on `mattn/go-sqlite3`. Cross-compilation needs C cross-compilers per target; for now, build natively on each platform.

```bash
# Tidy dependencies after adding/removing imports
go mod tidy
```

## Architecture Overview

HyperBot is a TUI AI agent chatbot built on [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go), supporting OpenAI-compatible APIs and Anthropic Claude models with MCP tool protocol integration.

### Layer Structure

```
main.go → tview.Application
    └── tui/tui.go (ConfigPage → AgentPage)

    ┌── global/
    │   ├── backendCore.go  (Agentrunner struct, config, session, memory, tools, embedFS)
    │   ├── tui.go          (TUI widget references: App, views, helpers)
    │   ├── tuihandler.go   (TUI operation wrappers: PrintToTui, ShowError, etc.)
    │   └── prompt/         (system prompt embedFS)
    │
    ├── memory/
    │   └── sqlite.go       (SQLite memory service factory)
    │
    ├── bootstrap/
    │   ├── Initializer.go  (async init: logs → config → agent creation)
    │   └── Bootstrap.go    (session lifecycle, main dialog loop)
    │
    ├── handler/
    │   ├── runIteratively.go (user input loop: /exit, /new, /flush, ESC, text)
    │   ├── runOnce.go        (single agent execution, event stream consumption)
    │   ├── model.go          (TurnCode, TurnResult types)
    │   └── message.go        (streaming/non-streaming render, reasoning content)
    │
    ├── agent/
    │   ├── baseAgent.go      (LLMAgent config assembly: skills, tools, system prompt)
    │   ├── OpenaiAgent.go    (OpenAI-compatible factory)
    │   └── AnthropicAgent.go (Anthropic factory)
    │
    ├── models/
    │   ├── openai.go    (OpenAI adapter, DeepSeek variant auto-detection)
    │   └── anthropic.go (Anthropic adapter)
    │
    ├── session/
    │   ├── memSessionService.go (InMemorySessionService with summarizer)
    │   ├── summarizer.go        (token/time thresholds, custom prompts, PostSummaryHook)
    │   └── prompt/              (summary system.md + user.md templates)
    │
    ├── config/
    │   ├── baseConfig.go       (model, API, context window config)
    │   ├── mcpConfig.go        (SSE/streamable_http MCP config)
    │   ├── stdinMcpConfig.go   (stdin MCP config)
    │   ├── configTemplate.go   (//go:embed config.yaml template)
    │
    ├── toolsets/
    │   ├── localexec/ (built-in command execution, always enabled)
    │   ├── mcp.go       (SSE/streamable HTTP MCP)
    │   └── stdinMCP.go  (stdin-based MCP processes)
    │
    ├── functionTools/
    │   ├── File.go       (WriteFile, ReadFile, EditFile, SearchInFile, DeleteFile, FileInfo, Diff)
    │   ├── Datetool.go   (DateNow)
    │   └── FileSystem.go (PWD, CD, LS, Mkdir, CP, MV, Glob)
    │
    └── utils/
        └── pretty/      (terminal color helpers)
```

### Key Design Patterns

**Agent Creation Flow**: `bootstrap.Init()` runs async in a goroutine. It redirects framework logs to `hyperbot.log`, loads/creates `config.yaml`, replaces `{{DATE}}`, `{{OSTYPE}}`, `{{HOME}}`, `{{CWD}}` and other placeholders in system prompt, then creates the appropriate LLMAgent based on `APIType`.

**Adding Function Tools**: create function → wrap with `function.NewFunctionTool()` → register in `Get*Tools()` (e.g. `GetFileSystemTools`) → assembled in `bootstrap/Initializer.go`. Missing registration means the tool silently won't appear.

**Dialog Loop**: `handler.AgentRunIteratively()` manages user input:
- `/exit` → terminate
- `/new` → reset session
- `/flush` → refresh tools without losing session history
- `ESC` → cancel current agent response via `context.WithCancel`
- text → invoke `AgentRunOnce()`

**Streaming Events**: `handler.AgentRunOnce()` calls `runner.Run()` and consumes an event stream. Messages render with:
- Reasoning content: yellow dim text between `»` and `«`
- Tool calls: magenta `⚙` icon + tool name + args
- Tool results: gray indented output

**LocalExec ToolSet**: Built-in 6-tool system for command lifecycle:
| Tool | Purpose |
|------|---------|
| `submit_command` | Submit command, get command ID |
| `start_command` | Start submitted command by ID |
| `get_status` | Query status; `wait_seconds` blocks until done or timeout (每秒轮询，完成即返回) |
| `get_output` | Get stdout/stderr with window limits |
| `intervene_command` | Write to stdin or send signals |
| `kill_command` | Force terminate |

**MCP Integration**: Configured via `config.yaml` with support for `sse` and `streamable_http` transport types. Also supports stdin-based MCP via `stdin_mcp` config.

**Session Memory**: `InMemorySessionService` is stored in `global.SessionService`. When refreshing tools via `/flush`, the session service must be preserved to maintain conversation history. `LoadConfig()` and `NewRunner()` can be called independently to reload tools without resetting memory.

**Session Summarization**: `session/summarizer.go` — token + time thresholds via `WithChecksAny`, plus `WithSkipRecent`, `WithToolResultFormatter`, `WithSyncSummaryIntraRun`, and `WithSessionSummaryInjectionMode(SessionSummaryInjectionUser)`. Requires `session.NewMemorySessionService` with summarizer AND `llmagent.WithAddSessionSummary(true)`. Key gotchas: `contextwindow` in config.yaml MUST match the actual API provider limit; `WithToolResultFormatter` truncates tool results before token estimation, affecting both summary input and threshold counting. See Context Management section for full details.

**Deployed Config**: User config lives in `<cwd>/.hyperbot/hyperbot.yaml` (the working directory where the binary runs). On the author's machine this is `C:\Users\<user>\OneDrive - ...\应用\hyperbot\.hyperbot\`, but it varies by platform. The repo's `.hyperbot/` is for development only.

## Configuration

Auto-generated to `hyperbot.yaml` on first run. Supports:
- User ID (auto-generated UUID)
- Model config (model name, base URL, API key, API type: `openai` or `anthropic`)
- MCP services (SSE or streamable_http)
- Stdin MCP processes

## Dependencies

- **trpc-agent-go** v1.10.0: Agent framework core
- **trpc-agent-go/model/anthropic** v1.10.0: Anthropic model adapter
- **trpc-agent-go/model/tiktoken** v1.10.0: Tiktoken-based token counter (replaces SimpleTokenCounter default)
- **trpc-mcp-go** v0.0.16: MCP tool protocol support
- **tview** v0.42.0: TUI framework
- **tcell/v2** v2.13.9: Terminal event handling
- **openai-go** v1.12.0: OpenAI API SDK
- **anthropic-sdk-go** v1.37.0: Anthropic API SDK
- **zap** v1.28.0: Logging
- **otiai10/copy** v1.14.1: Cross-device file/directory copy for CP and MV tools

## Notes

- Go 1.26.1+ required
- No test files exist in this repository
- Skills are loaded from `skills/` directory in Knowledge-Only mode (commands go through LocalExec)
- Framework logs are redirected to `hyperbot.log` to avoid TUI interference
- **embedFS case sensitivity** - `//go:embed` + `ReadFile` paths are case-sensitive on Linux. File named `systemprompt.md` but code reading `systemPrompt.md` silently returns empty string (error ignored with `_`). Always match exact file name case between `go:embed` glob patterns and `ReadFile` calls.
- **Skills identity contamination** - The deployed `skills/` folder (`~/.hyperbot/skills/`) contains OpenClaw skill files (self-improving-agent, find-skills, etc.) that reference "OpenClaw", "Claude Code", "clawdhub". When loaded via `llmagent.WithSkills()`, these contaminate the agent's identity. Use only HyperBot-specific skills or keep the folder empty.
- **bracketed paste** - tview TextArea 对大块粘贴支持不好，需在 tui.go 中启用 `EnableBracketedPaste()` 让终端分片发送，框架才能正常处理。参考 commit 70bb8f3。
- **Go RE2 regex in tool descriptions** - 在给 agent 暴露正则的 tool 中，jsonschema description 必须写明 RE2 限制：`(?s)` 让 `.` 匹配换行、`(?m)` 让 `^`/`$` 匹配行边界、不支持 lookahead/lookbehind/backreference。参考 `functionTools/File.go` 中 SearchInFile 的 description。
- **MV/CP tools use otiai10/copy** - `functionTools/FileSystem.go` 的 CP 和 MV 跨设备移动使用 `github.com/otiai10/copy`（不区分文件/目录）。MV 同设备优先 `os.Rename`（快速+原子），跨设备走 `os.RemoveAll(dst)` → `copy.Copy` → `os.RemoveAll(src)`（先删目标避免合并语义）。
- **EditFile replace_all semantics** - `replace_all=false`（默认）是安全检查：多处匹配时报错拒绝替换，而非只替换第一处。`replace_all=true` 才执行全量替换。修改时不要移除 `len(Indexes) > 1` 的检查。
- **strings.Index loop pattern** - 在 `Now[offset:]` 上循环搜索时，`offset += idx` 定位到匹配起点后，必须再 `offset += len(matched)` 跳过已匹配内容，否则同一位置重复匹配导致死循环。
- **Tool call 后 agent 停止输出** - 如果 `hyperbot.log` 无 error，且对 agent 说"继续"能恢复对话，说明是模型侧在 tool result 后概率性预测了 stop token，不是框架 bug。不需要迁就式修改。
- **`models.Openai()` / `models.Anthropic()` are the canonical model constructors** — `session/summarizer.go` and agent creation use these two functions. They handle DeepSeek variant detection, reasoning backfill, and API auth. When creating a new model instance from config, call these instead of manually assembling openai/anthropic options.

## Agent-Driven Memory (SQLite)

Persistent long-term memory using SQLite, managed entirely by the agent via tool calls — no background auto-extraction.

### Architecture
- `memory/sqlite.go` — factory: creates `memorysqlite.Service` in manual/agentic mode (no extractor). Exposes 5 tools via `WithToolEnabled(memory.DeleteToolName)` on top of `DefaultEnabledTools` (search, load, add, update). `memory_clear` is intentionally not exposed.
- `global/backendCore.go` — `SqliteMemoryService *memorysqlite.Service` global
- `bootstrap/Initializer.go` — `initSqliteMemoryService()` called in `Init()`, writes to `<configDir>/memory.db`. No longer requires `config.Model` parameter (extractor removed).
- `bootstrap/Bootstrap.go` — `initAgent()` appends `SqliteMemoryService.Tools()` to agent tools and sets `WithPreloadMemory(10)` + `runner.WithMemoryService()`
- `global/prompt/systemprompt.md` — `# Memory` section defines agent memory behavior: search-before-store, proactive storage, outdated correction, atomic/specific writing standards

### Key behaviors
- **Agent-driven**: all memory creation, update, and deletion happens through explicit agent tool calls. No background extractor, no `EnqueueAutoMemoryJob`.
- **Preload**: sync during content processor — `WithPreloadMemory(10)` adaptively loads all memories (≤10) or searches top-10 by user query. Injected into system prompt via `injectSystemContextMessage`.
- **Search**: keyword-based (BM25 + CJK gse segmentation). No embedder needed. Both preload and agent `memory_search` use keyword overlap — pure BM25, no semantic understanding. Upgrading to embedder + vector backend (e.g. `sqlitevec`, `pgvector`) is the path for true semantic recall.
- **Exposed tools**: `memory_search`, `memory_load`, `memory_add`, `memory_update`, `memory_delete`. `memory_clear` is NOT exposed (risk of wiping all memories).

### Why no auto-extraction
Auto-extraction was removed because of dual-writer conflicts between agent and background extractor:
- Extractor's BM25 search is topic-matched (finds "related"), not contradiction-matched (finds "outdated") → fails to update superseded memories
- When agent updates a memory (changing its content-hash ID), extractor references the old ID → "not found" → fallback to `AddMemory` → creates duplicate
- Extractor `UpdateMemory` passes through `reconcileOps` unchecked (only Add ops are reconciled) → extractor can overwrite agent's updates unconditionally
- No timestamp/version protection on `UpdateMemory` → last-write-wins without any guard

Agent-driven mode avoids all of these by having a single writer who understands full conversation context and can detect contradictions semantically.

### Gotchas
- `initSqliteMemoryService()` MUST be called before `initAgent()` — agent creation reads `SqliteMemoryService.Tools()`, nil service → panic → black screen
- `stdlog.SetOutput(file)` in `redirectFrameworkLog()` redirects gse dictionary-loading chatter away from TUI
- Default memory limit: 100000 (`memory/sqlite.go:WithMemoryLimit`)
- `memory/sqlite.go` no longer imports `config`, `models`, `extractor`, or `model` — extractor model creation removed

## Context Management

HyperBot uses three complementary mechanisms to prevent context overflow:

### 1. Session Summarization (`session/summarizer.go` + `bootstrap/Initializer.go`)
- `WithAddSessionSummary(true)` on the LLM agent enables async summary injection
- Summarizer triggers at `CheckTokenThreshold(0.6 * contextwindow)` OR `CheckTimeThreshold(10min)` via `WithChecksAny`
- `WithSkipRecent` preserves the last complete interaction cycle (from last user message to tail) from being summarized — keeps current turn intact in prompt
- `WithToolResultFormatter` truncates tool results to 1000 runes (head 500 + tail 500) before entering summary model input — reduces noise, improves summary quality. Only affects summary input; original events remain intact in session for `session_search`/`session_load`
- `WithSyncSummaryIntraRun(true)` enables synchronous summary refresh between LLM loop iterations in the same run — ensures compressed state is visible to next LLM call in long ReAct chains
- `WithSessionSummaryInjectionMode(SessionSummaryInjectionUser)` injects summary into user message instead of system message — keeps system prompt clean (SOP rules only), summary participates in normal window management
- Token counting uses `model/tiktoken` (BPE), configured via `summary.SetTokenCounter(counter)`
- Summary model is the same as main model; for DeepSeek reasoning models, the token counter falls back to `cl100k_base` (within ~4-7% of DeepSeek's actual count per empirical testing)
- If summaries fail silently (check `hyperbot.log` for "summary worker failed"), session continues uncompressed → context grows unbounded → API errors
- Post-summary hook strips `<think>...</think>` tags from summary text
- `WithToolResultFormatter` affects summary input AND threshold token counting — the formatter truncates content before token estimation, so the effective threshold is based on truncated content, not original. This is intentional: summary model sees cleaner input and produces better state recovery
- `extractTokenThresholdMessage` includes `ReasoningContent` in calculation (previously dropped silently, causing delayed summarization for DeepSeek reasoning models)
- `WithSessionSummaryInjectionMode` does not exist in the current trpc-agent-go version; summary is injected as a system message
### 2. Context Compaction (`agent/baseAgent.go`)
- `WithEnableContextCompaction(true)` enables deterministic tool result compression before each LLM call
- **Pass 1**: Historical tool results > 1024 tokens → replaced with placeholder (`event_id`/`tool_call_id` preserved for `session_load` recovery)
  - Protects current invocation + `KeepRecentRequests` (default 1) most recent completed invocations
- **Pass 2**: Any tool result > 8192 tokens → head+tail truncation with `[...N chars truncated...]` marker
  - Applies to ALL invocations including current; gated on `OversizedToolResultMaxTokens > 0`
- Triggers at 70% context window (`ContextCompactionThresholdRatio`, default 0.7)
- If still over threshold after compaction → sync `CreateSessionSummary` runs as fallback → request rebuilt

### 3. On-Demand Session (`agent/baseAgent.go`)
- `WithEnableOnDemandSession(true)` gives agent `session_load`/`session_search` tools
- Compacted/truncated tool results can be retrieved by `event_id` with `content_offset`/`content_limit` for sliced loading

### Troubleshooting Context Overflow
- **Symptom**: API error "requested X tokens exceeds maximum Y" (X >> Y)
- **Check**: `hyperbot.log` for "summary worker failed" — if present, summaries are failing
- **Verify**: `contextwindow` in config MUST be ≤ actual model limit (not larger, or threshold triggers too late)
- **Note**: tiktoken `cl100k_base` vs DeepSeek API token count differs ~4-7% (empirically verified) — not enough to explain large discrepancies
- **Root cause pattern**: first summary attempt fails → delta grows unbounded → all subsequent attempts also fail (cascade failure)
- **Fix**: enable Context Compaction + lower `CheckTokenThresholdPercent` if needed

## Documentation Style

- README.md and README_en.md should be feature-focused and user-facing — highlight what the project does, not internal architecture or code organization
- Keep both language versions in sync when updating either one
