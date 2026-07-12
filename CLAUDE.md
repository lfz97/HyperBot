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
    ├── global/TUI.go        (TUI widget setup: pages, agent page, HelpTable, PageCreate)

    ┌── global/
    │   ├── AgentEngine.go   (Agentrunner struct, global state vars, embedFS, AgentEngineRun)
    │   ├── TUI.go           (TUI widget creation: agentPage, InitHelpTable, RefreshHelpTable)
    │   ├── tuihandler.go    (TUI operation wrappers: PrintToTui, ShowError, ToggleHelpPage)
    │   └── prompt/          (system prompt embedFS)
    │
    ├── memory/
    │   └── sqlite.go       (SQLite memory service factory)
    │
    ├── bootstrap/
    │   ├── Initializer.go  (init sequence: logs → config → skills → agent creation, called from goroutine)
    │   └── Bootstrap.go    (session lifecycle, main dialog loop)
    │
    ├── handler/
    │   ├── runIteratively.go (user input loop: /exit, /new, /flush, ESC, text)
    │   ├── runOnce.go        (single agent execution, event stream consumption)
    │   ├── model.go          (TurnCode, TurnResult types)
    │   ├── message.go        (streaming/non-streaming render; non-stream uses glamour + TranslateANSI for markdown)
    │   └── toolMsg.go        (addToolCallMsg/addToolResultMsg helpers)
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

**UI Layout**: AgentPage 全屏布局：StatusBar(1行) + AgentMessage(弹性，无边框) + InputRow(1行)。InputRow 内 InputArea 占弹性空间，右侧 15 列显示 `Ctrl+K 帮助` 灰色提示。HelpTable（`tview.Table`，左右两栏：指令名 + 描述）通过 `app_p.SetRoot()` 整体替换根组件来全屏展示，Esc/Ctrl+K 关闭后恢复原 pages。

**Startup Flow**: `main()` calls three functions in order:
- `PageCreate()` — synchronously creates `tview.Application`, `pages`, adds only `AgentPage`, calls `InitHelpTable()`, sets root
- `AgentEngineRun(initFn, startFn)` — spawns a goroutine that runs `bootstrap.Init()` then `bootstrap.AgentStart()` (thin wrapper in `global/AgentEngine.go`)
- `TuiRun()` — runs `app_p.Run()` on the main goroutine; `app_p.Stop()` (triggered by exit/error paths) causes clean return without deadlock

`PageCreate()` must run BEFORE `AgentEngineRun()` to ensure `app_p` is initialized when the goroutine calls `QueueUpdateDraw`.

ConfigCheck page and its widgets (`bannerBar`, `Log`, `banner`) were removed — init progress messages now write to `AgentMessage` directly on the agent page.

**Agent Creation Flow**: `bootstrap.Init()` runs in a goroutine spawned by `AgentEngineRun()`. It redirects framework logs to `hyperbot.log`, loads/creates `hyperbot.yaml` and `skills/` folder, replaces `{{DATE}}`, `{{OSTYPE}}`, `{{HOME}}`, `{{CWD}}` and other placeholders in system prompt, calls `loadSkills()` (populates `helpItems` for HelpTable), then creates the appropriate LLMAgent based on `APIType`. All init progress/error messages go to `AgentMessage` (no separate ConfigCheck page).

**Adding Function Tools**: create function → wrap with `function.NewFunctionTool()` → register in `Get*Tools()` (e.g. `GetFileSystemTools`) → assembled in `bootstrap/Initializer.go`. Missing registration means the tool silently won't appear.

**Dialog Loop**: `handler.AgentRunIteratively()` manages user input:
- `/exit` → terminate
- `/new` → reset session
- `/flush` → refresh tools without losing session history
- `ESC` → cancel current agent response via `context.WithCancel`
- text → invoke `AgentRunOnce()`

**Streaming Events**: `handler.AgentRunOnce()` calls `runner.Run()` and consumes an event stream. Messages render with:
- Reasoning content: yellow dim text (suppressible via `show_reasoning: false`)
- Tool calls/results: compact single-line via `TToolCompact` — green `●` dot + orange tool name + dim gray `args → result_summary`. Short results (≤60 chars, single-line) inline; long results show stats (`3 lines, 12.5KB`).

**LocalExec ToolSet**: Built-in 5-tool system for command lifecycle:
| Tool | Purpose |
|------|---------|
| `submit_command` | Submit command, get command ID |
| `get_status` | Query status; `wait_seconds` blocks until done or timeout (每秒轮询，完成即返回) |
| `get_output` | Get stdout/stderr with window limits |
| `intervene_command` | Write to stdin or send signals |
| `kill_command` | Force terminate |

**MCP Integration**: Configured via `hyperbot.yaml` with support for `sse` and `streamable_http` transport types. Also supports stdin-based MCP via `stdin_mcp` config.

**Session Memory**: `InMemorySessionService` is stored in `global.SessionService_p`. When refreshing tools via `/flush`, the session service must be preserved to maintain conversation history. `LoadConfig()` and `NewRunner()` can be called independently to reload tools without resetting memory. `NewRunner()` calls `loadSkills()` to re-scan the skills directory and refresh `helpItems` for the HelpTable — if adding a new reload step to `NewRunner()`, keep it before `initAgent()` so the agent picks up fresh state.

**Session Summarization**: `session/summarizer.go` — token + time thresholds via `WithChecksAny`, plus `WithSkipRecent`, `WithToolResultFormatter`, `WithSyncSummaryIntraRun`, and `WithSessionSummaryInjectionMode(SessionSummaryInjectionUser)`. Requires `session.NewMemorySessionService` with summarizer AND `llmagent.WithAddSessionSummary(true)`. Key gotchas: `contextwindow` in hyperbot.yaml MUST match the actual API provider limit; `WithToolResultFormatter` truncates tool results before token estimation, affecting both summary input and threshold counting. See Context Management section for full details.

**Deployed Config**: User config lives in `<cwd>/.hyperbot/hyperbot.yaml` (the working directory where the binary runs). On the author's machine this is `C:\Users\<user>\OneDrive - ...\应用\hyperbot\.hyperbot\`, but it varies by platform. The repo's `.hyperbot/` is for development only.

## Configuration

Auto-generated to `hyperbot.yaml` on first run. Supports:
- User ID (auto-generated UUID)
- Model config (model name, base URL, API key, API type: `openai` or `anthropic`)
- MCP services (SSE or streamable_http)
- Stdin MCP processes

## Dependencies

- **glamour** v1.0.0: Markdown → ANSI renderer (non-stream mode uses `glamour.Render` + `tview.TranslateANSI`)
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

- Go 1.26.4+ required
- No test files exist in this repository
- Skills are loaded from `skills/` directory in Knowledge-Only mode (commands go through LocalExec)
- Framework logs are redirected to `hyperbot.log` to avoid TUI interference
- **HelpTable dynamic items** — Default slash commands in `helpItems` initialized via `defaultHelpItems()`. Skills appended via `loadSkills()` → `ResetHelpItems()` + `AddHelpItems()`. `RefreshHelpTable()` rebuilds Table cells and is called in `ToggleHelpPage()` on every open.
- **embedFS case sensitivity** - `//go:embed` + `ReadFile` paths are case-sensitive on Linux. File named `systemprompt.md` but code reading `systemPrompt.md` silently returns empty string (error ignored with `_`). Always match exact file name case between `go:embed` glob patterns and `ReadFile` calls.
- **Skills identity contamination** - The deployed `skills/` folder (`~/.hyperbot/skills/`) contains OpenClaw skill files (self-improving-agent, find-skills, etc.) that reference "OpenClaw", "Claude Code", "clawdhub". When loaded via `llmagent.WithSkills()`, these contaminate the agent's identity. Use only HyperBot-specific skills or keep the folder empty.
- **bracketed paste** - tview TextArea 对大块粘贴支持不好，需在 TUI.go 中启用 `EnableBracketedPaste()` 让终端分片发送，框架才能正常处理。参考 commit 70bb8f3。
- **Go RE2 regex in tool descriptions** - 在给 agent 暴露正则的 tool 中，jsonschema description 必须写明 RE2 限制：`(?s)` 让 `.` 匹配换行、`(?m)` 让 `^`/`$` 匹配行边界、不支持 lookahead/lookbehind/backreference。参考 `functionTools/File.go` 中 SearchInFile 的 description。
- **MV/CP tools use otiai10/copy** - `functionTools/FileSystem.go` 的 CP 和 MV 跨设备移动使用 `github.com/otiai10/copy`（不区分文件/目录）。MV 同设备优先 `os.Rename`（快速+原子），跨设备走 `os.RemoveAll(dst)` → `copy.Copy` → `os.RemoveAll(src)`（先删目标避免合并语义）。
- **EditFile replace_all semantics** - `replace_all=false`（默认）是安全检查：多处匹配时报错拒绝替换，而非只替换第一处。`replace_all=true` 才执行全量替换。修改时不要移除 `len(Indexes) > 1` 的检查。
- **strings.Index loop pattern** - 在 `Now[offset:]` 上循环搜索时，`offset += idx` 定位到匹配起点后，必须再 `offset += len(matched)` 跳过已匹配内容，否则同一位置重复匹配导致死循环。
- **Tool call 后 agent 停止输出** - 如果 `hyperbot.log` 无 error，且对 agent 说"继续"能恢复对话，说明是模型侧在 tool result 后概率性预测了 stop token，不是框架 bug。不需要迁就式修改。
- **`models.Openai()` / `models.Anthropic()` are the canonical model constructors** — `session/summarizer.go` and agent creation use these two functions. They handle DeepSeek variant detection, reasoning backfill, and API auth. When creating a new model instance from config, call these instead of manually assembling openai/anthropic options.
- **ANSI → tview tag conversion required** — tview's `SetDynamicColors(true)` only supports tview's own color tag format (`[red]text[-]`, `[::b]bold[::-]`). It does NOT support standard ANSI escape sequences. Any ANSI-based rendering (glamour, lipgloss, etc.) must go through `tview.TranslateANSI()` to convert ANSI codes to tview tags before writing to a TextView. Without this conversion, ANSI codes appear as visible garbage text like `[38;5;252m`.
- **Tool response content must be skipped in content rendering** — `NewToolMessage` stores tool result in `Content` field alongside `Role="tool"`. Both stream and non-stream content paths check `Role != "tool"` to prevent tool JSON from leaking through the main content renderer. Without this, tool results appear as raw JSON in body text while also being formatted via `TToolResult`.
- **Multi-tool results handled in `runOnce.go`** — Framework merges parallel tool results into a single `tool.response` event with N Choices. `AgentRunOnce` detects `ObjectTypeToolResponse` and iterates ALL Choices (not just `Choices[0]`), ensuring every result is rendered.
- **Glamour markdown rendering** — Non-stream body text is rendered via `glamour` (dark theme). `newGlamourRenderer()` in `message.go` dynamically creates a renderer per call: `WithWordWrap(w)` uses `AgentMessage.GetInnerRect()` width so content fills the terminal and re-wraps on resize; `document.margin = 0` removes the dark theme's 2-char left margin. The render call applies `strings.TrimRight` to strip trailing whitespace/newlines from glamour output to prevent alignment artifacts before tool calls. **Must append `[-:-:-]` after `TranslateANSI(out)`** — glamour's ANSI output may not end with a full reset sequence, leaving unclosed tview tags that leak into the next line (tool calls appear brighter/miscolored).
- **`show_reasoning` config** — `config.Model.ShowReasoning` (`yaml:"show_reasoning"`) controls whether reasoning/thinking content is displayed. Default `false`. Affects both stream (chunk-level skip) and non-stream (whole-block skip) paths. Set `show_reasoning: true` in `hyperbot.yaml` to enable.
- **`message.go` refactored** — `printMessage` split into `renderStreamEvent`, `renderNonStreamEvent`, `renderToolCall`, `renderToolResult`. Tool call/result rendering uses shared `addToolCallMsg`/`addToolResultMsg` helpers in `toolMsg.go`. Compact single-line format via `pretty.TToolCompact` — no trailing `\n` (double-newline with next tool's leading `\n` causes alignment shift).
- **Glamour default WordWrap is 80 columns** — without `WithWordWrap`, glamour wraps all markdown at 80 columns regardless of terminal width. Always pass the current view width when creating a renderer. See `newGlamourRenderer()` in `handler/message.go` for the pattern.

## Auto-Extraction Memory (SQLite)

Persistent long-term memory using SQLite, with background LLM-based extraction after each turn. Agent can also manually use memory tools as a supplement.

### Architecture
- `memory/sqlite.go` — factory: creates `memorysqlite.Service` with `extractor.NewExtractor(model)` + `WithExtractor(ext)`. Exposes `memory_search`, `memory_load`, `memory_add`, `memory_update` via `WithAutoMemoryExposedTools`. `memory_delete` and `memory_clear` are not exposed to agent (extractor handles delete internally).
- `global/AgentEngine.go` — `SqliteMemoryService *memorysqlite.Service` global
- `bootstrap/Initializer.go` — `initSqliteMemoryService()` creates extractor model from config (via `models.Openai()` / `models.Anthropic()`), passes to `NewSQLiteMemoryService(config.Model, dbPath)`. Called after `LoadConfig()`, before `NewRunner()`.
- `bootstrap/Initializer.go` — `initAgent()` appends `SqliteMemoryService.Tools()` to agent tools and sets `WithPreloadMemory(10)` + `runner.WithMemoryService()`
- The framework runner auto-calls `EnqueueAutoMemoryJob()` after each turn — no manual trigger needed
- `global/prompt/systemprompt.md` — brief `# Memory` section: explains auto-extraction runs in background, lists available manual tools

### Key behaviors
- **Auto-extraction**: extractor runs after each turn via `EnqueueAutoMemoryJob`. Uses the same model as the main agent. Determines what to store/update/delete through a dedicated LLM call with its own system prompt (`extractor/defaultPrompt`).
- **Agent supplement**: agent has `memory_search`, `memory_load`, `memory_add`, `memory_update` exposed. Can manually store or correct when it notices something the extractor missed.
- **Preload**: `WithPreloadMemory(10)` adaptively loads all memories (≤10) or searches top-10 by user query. Injected into system prompt.
- **Search**: keyword-based (BM25 + CJK gse segmentation). No embedder needed.
- **Reconcile**: extractor's `reconcileOps` checks new Add ops against existing memories for near-duplicates (BM25 score ≥0.90 or Jaccard ≥0.70 → skip; ≥0.60 or ≥0.40 → rewrite as update).

### Known tolerable issues
- Extractor's BM25 dedup may miss semantically-similar but lexically-different duplicates → occasional near-duplicate memories. Impact is negligible since retrieval is also BM25-based (fuzzy).
- Agent and extractor can both write (dual-writer). Extractor runs async after turn; agent writes inline. Minor race potential but practically harmless — fuzzy retrieval masks any duplicates.
- `UpdateMemory` changes memory ID (content-hash based) → extractor referencing old ID falls back to Add → may create duplicate. Again, fuzzy retrieval makes this invisible in practice.

### Gotchas
- `initSqliteMemoryService()` MUST be called before `initAgent()` — agent creation reads `SqliteMemoryService.Tools()`, nil service → panic → black screen
- `NewSQLiteMemoryService` imports `config`, `models`, `extractor`, and `model` — needs full config to create extractor model
- Default memory limit: 100000 (`memory/sqlite.go:WithMemoryLimit`)
- Extractor model is the same as main model (same API endpoint/credentials)

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
### 2. Context Compaction (`bootstrap/Initializer.go` — `initAgent()`)
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
