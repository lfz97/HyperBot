# HyperBot 🤖

A TUI (Terminal User Interface) AI Agent chatbot built on the [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go) framework. Supports OpenAI-compatible APIs and Anthropic (Claude) models, plugs into any MCP toolset (SSE / Streamable HTTP / stdin) through a unified config, and ships with full local-command lifecycle management, a Knowledge-Only skill system, and automatic session summarization.

## Features

- 🖥️ **TUI Interface**: A two-page terminal UI based on [tview](https://github.com/rivo/tview) (config page → chat page). Mouse and bracketed paste are both enabled — pasting long text no longer freezes the input.
- 🤖 **Multi-Model Support**: Works with the OpenAI API (including OpenAI-compatible providers like DeepSeek, with automatic variant adaptation for DeepSeek) and the Anthropic Claude family.
- 🔄 **Streaming / Non-Streaming Output**: Real-time streaming of reasoning content (highlighted in dim yellow) and tool calls, or full non-streaming responses.
- 🔧 **Unified MCP Integration**: Configure any number of MCP toolsets via a single list:
  - HTTP transports: `sse` and `streamable_http`
  - Local transport: `stdin_mcp` (start a child process via `command + args`, e.g. `npx -y mcp-exec`)
- 💻 **Local Command Lifecycle**: The built-in `localexec` toolset exposes 6 tools that close the full loop: submit → start → poll status → fetch output → intervene (stdin / signal) → kill.
- 📚 **Knowledge-Only Skill System**: A filesystem-based skill repository that auto-loads `SKILL.md` files under `.hyperbot/skills/`. Skills only inject knowledge context; all command execution is funneled through `localexec`.
- 🧠 **Automatic Session Summarization**: Uses [tiktoken](https://github.com/tiktoken-go/tokenizer) for accurate token counting; multiple OR-combined trigger conditions automatically compress history; the generated summary is echoed back to the chat area in green in real time.
- 💬 **Multi-Turn Chat & Session Commands**: Session-level context memory with `/new` (reset session), `/flush` (hot-reload tool config), `/exit` (quit), and `ESC` (interrupt the current response).
- 📝 **Operation Record**: For complex tasks, the agent appends a concise Markdown entry to `.hyperbot/OperationRecord.md` so future sessions can pick up where it left off.
- 🔄 **Cross-Compilation**: One-shot builds for Linux (x64/ARM64), macOS (x64/ARM64), and Windows (x64).

## Architecture

```
User Input
  │
  ▼
┌─────────────────────────────────────────────────────────┐
│  main.go → tui.TuiInit (tview.Application)              │
│    └── tui/tui.go                                       │
│         ├── ConfigPage  (startup banner + init logs)    │
│         └── AgentPage   (chat: Sidebar + messages + input) │
├─────────────────────────────────────────────────────────┤
│  bootstrap/                                             │
│    ├── Initializer.go  (log redirect → load config → fill SystemPrompt → create Agent/Session) │
│    └── Bootstrap.go    (main loop: New / Continue / Flush / Int / Exit state machine) │
├─────────────────────────────────────────────────────────┤
│  handler/                                               │
│    ├── runIteratively.go  (user loop + ESC cancel + /flush command parsing) │
│    ├── runOnce.go         (single turn: Runner.Run + event-stream consumption) │
│    └── message.go         (streaming / non-streaming render, reasoning blocks, tool calls / results) │
├─────────────────────────────────────────────────────────┤
│  agent/                                                 │
│    ├── baseAgent.go       (LLMAgent option assembly: skills / toolsets / SystemPrompt / summary injection) │
│    ├── OpenaiAgent.go     (OpenAI-compatible factory)   │
│    └── AnthropicAgent.go  (Anthropic factory)           │
├─────────────────────────────────────────────────────────┤
│  session/                                               │
│    ├── memSessionService.go (in-memory Session service + async summary) │
│    ├── summarizer.go        (Summarizer + triggers + PostSummaryHook echo) │
│    ├── toolformatter.go     (compact tool-call / result formatting in summary input) │
│    └── prompt/{system,user}.md (summarizer system / user prompts) │
├─────────────────────────────────────────────────────────┤
│  models/                                                │
│    ├── openai.go     (OpenAI adapter, DeepSeek auto-detect) │
│    └── anthropic.go  (Anthropic adapter)                │
├─────────────────────────────────────────────────────────┤
│  toolsets/                                              │
│    ├── localexec/    (built-in local command toolset, always enabled) │
│    ├── mcp.go        (HTTP MCP: sse / streamable_http)  │
│    └── stdinMCP.go   (Stdin MCP: command + args child process) │
└─────────────────────────────────────────────────────────┘
```

## Project Structure

```
HyperBot/
├── main.go                       # Entry: calls tui.TuiInit() to start the tview app
├── Makefile                      # Cross-compile script (5 targets)
├── build.ps1                     # Windows PowerShell build script
│
├── agent/                        # Agent creation & configuration
│   ├── baseAgent.go              #   Core: LLMAgent option assembly
│   ├── OpenaiAgent.go            #   OpenAI-compatible Agent factory
│   └── AnthropicAgent.go         #   Anthropic Agent factory
│
├── bootstrap/                    # Startup
│   ├── Initializer.go            #   Log redirect → config → skill dir → SystemPrompt placeholders → Agent / Session
│   ├── Bootstrap.go              #   Chat main loop: Session management, TurnResult state machine
│   └── prompt/
│       └── systemprompt.md       #   Global System Prompt (Traffic Light protocol, Execution Engine workflow)
│
├── config/                       # Config definitions
│   ├── baseConfig.go             #   Config / Model / User structs
│   ├── mcpConfig.go              #   MCP (HTTP) config: sse / streamable_http
│   ├── stdinMcpConfig.go         #   StdinMCP (subprocess) config
│   └── yaml_template.go          #   Default YAML written on first run
│
├── handler/                      # Conversation layer
│   ├── model.go                  #   TurnResult / TurnCode (New, Continue, Flush, Int, Error, Exit) + AgentRunner
│   ├── runIteratively.go         #   User loop: input capture, Ctrl+Enter submit, command dispatch, ESC cancel
│   ├── runOnce.go                #   Single turn: Runner.Run() → event consumption → TerminalError handling
│   └── message.go                #   Message rendering: streaming / non-streaming, reasoning blocks, tool calls / results
│
├── models/                       # Model adapters
│   ├── openai.go                 #   OpenAI model init (with DeepSeek variant auto-detection)
│   └── anthropic.go              #   Anthropic model init
│
├── session/                      # Sessions & summarization
│   ├── memSessionService.go      #   InMemory SessionService + async summary queue
│   ├── summarizer.go             #   Summarizer factory: tiktoken counting + multi-triggers + PostSummaryHook
│   ├── toolformatter.go          #   Compact tool-call / result format for summary input (with truncation)
│   └── prompt/
│       ├── system.md             #   Summarizer system prompt (enforces <analysis> + <summary>)
│       └── user.md               #   Summarizer user prompt (9-section structured template)
│
├── toolsets/                     # Toolsets
│   ├── localexec/                #   Built-in local command execution (always enabled)
│   │   ├── definition.go         #     ToolSet interface impl
│   │   ├── tools.go              #     6 tools: submit / start / status / output / intervene / kill
│   │   ├── manager.go            #     Process manager: submit, start, signal, stdin, output capture
│   │   ├── model.go              #     Job / Manager / status constants
│   │   └── cache.go              #     Global Manager singleton
│   ├── mcp.go                    #   HTTP MCP (sse / streamable_http)
│   └── stdinMCP.go               #   Stdin MCP (subprocess)
│
├── functionTools/                # Example function tool
│   └── BookRecommender.go
│
├── tui/                          # TUI
│   ├── tui.go                    #   Two-page layout + banner + mouse / bracketed paste
│   ├── global_object/            #   Global *tview component singletons
│   └── tip/                      #   Status-bar scroll tips / sidebar prompt copy
│
├── utils/                        # Utilities
│   └── pretty/                   #   Pretty terminal output (ANSI / tview tag dual-mode)
│
└── release/                      # Cross-compile artifacts
    ├── linux-arm64/  linux-x64/  macos-arm64/  macos-x64/  windows-x64/
```

> All runtime data lives under `.hyperbot/` and `output/` next to the executable, so the source tree stays clean.

## Core Flow

### Startup

1. `main.go` calls `tui.TuiInit()`: creates `tview.Application`, loads `ConfigPage` (banner + log area), enables mouse and bracketed paste.
2. A background goroutine runs `bootstrap.Init("HyperBot")`:
   - Resolves the executable's directory as `CWD`
   - Ensures the config directory `.hyperbot/` exists
   - Ensures `.hyperbot/hyperbot.yaml` exists (first run writes from `yaml_template.go` and replaces `{USERID}` with a random UUID)
   - Ensures the skills directory `.hyperbot/skills/` exists
   - Redirects trpc-agent-go framework logs to `.hyperbot/hyperbot.log` so they don't pollute the TUI
   - Loads and parses `hyperbot.yaml`
   - Fills SystemPrompt placeholders with runtime info (date / timezone / OS / arch / user / hostname / `CWD` / `OUTPUTDIR` etc.)
   - Creates the in-memory SessionService (with async Summarizer) and the Agent + Runner
3. Once initialized, `app.QueueUpdateDraw()` switches to `AgentPage`.

### Chat Main Loop (`bootstrap.AgentStart`)

Driven by the `handler.TurnResult` state machine:

| Code | Meaning | Behavior |
|------|---------|----------|
| `New` | New conversation | Reset sessionID / requestID, wait for user input |
| `Continue` | Turn completed normally | Keep sessionID, wait for next input |
| `Int` | User pressed ESC | No extra prompt, wait for next input |
| `Error` | Turn errored out | Build a recovery prompt automatically and let the agent self-heal |
| `Flush` | User typed `/flush` | Reload config → close old Runner → create a new Runner with the latest tool config; sessionID is preserved |
| `Exit` | User typed `/exit` | Close Runner, exit the tview app |

### Single Turn (`handler.AgentRunOnce`)

Calls `runner.Run(ctx, userID, sessionID, msg)` and consumes the returned event channel:

- **Reasoning content**: opens with `»` → dim yellow text → closes with `«`
- **Body content**: rendered as-is
- **Tool calls**: magenta `⚙` + tool name + arguments
- **Tool results**: gray, indented
- Only `IsTerminalError()` aborts the conversation; non-terminal errors `continue` so transient 5xxs don't kill the chat
- Listens to `ctx.Done()` (i.e. user ESC) throughout, so output can be cancelled at any moment

### Local Command Execution (LocalExec)

The agent uses 6 tools to manage commands end to end:

| Tool | Purpose |
|------|---------|
| `submit_command` | Submit a command (`process` + `args`); returns a command ID |
| `start_command` | Start a previously-submitted command by ID |
| `get_status` | Query status (pending / running / done / failed / killed); returns all if no ID is given |
| `get_output` | Read stdout / stderr; supports a byte `window` and a `stream` selector |
| `intervene_command` | Write to stdin or send `SIGINT` / `SIGTERM` / `SIGKILL` (Windows: stdin + force-kill only) |
| `kill_command` | Force-terminate the command |

> Workflow: `submit → start → poll get_status / get_output → intervene if needed → kill if needed`.

### Automatic Session Summarization

- **Triggers (any one fires, OR-combined)**:
  - `EventThreshold = 20`: number of new events since last summary
  - `CheckTokenThreshold = 150_000`: number of new tokens since last summary
  - `CheckTimeThreshold = 10 * time.Minute`: idle time in the current session
- **Token counting**: replaces the default counter with `trpc-agent-go/model/tiktoken` so counts are accurate per the active model.
- **Summary contract**: enforces a two-block output
  - `<analysis>...</analysis>`: the model's own scratchpad on what to keep / drop (≤150 words)
  - `<summary>...</summary>`: a 9-section structured template (Primary Request / Key Concepts / Files & Code / Errors & Fixes / Problem Solving / User Messages / Pending Tasks / Current Work / Optional Next Step)
- **Compact summary input**: `WithToolCallFormatter` / `WithToolResultFormatter` feed tool calls and results to the summarizer in `[Tool: name, Args: ...]` form, with truncation for oversized payloads (args > 100 chars / result > 300 chars).
- **Live TUI echo**: `PostSummaryHook` writes the freshly generated summary into the chat area in green, so users can always see the current compressed view.
- **Async**: `AsyncSummaryNum=2`, `SummaryQueueSize=100`, `SummaryJobTimeout=60s` — summaries run in the background without blocking the active turn.
- **Next-turn injection**: `llmagent.WithAddSessionSummary(true)` makes the framework prepend the latest summary to the next turn's context, avoiding re-reading the full history.

## Configuration

On first launch the following layout is created next to the executable:

```
<CWD>/
├── .hyperbot/
│   ├── hyperbot.yaml        # Main config
│   ├── skills/              # Skill repository (Knowledge-Only)
│   ├── hyperbot.log         # Framework logs
│   └── OperationRecord.md   # (Appended by the agent during tasks)
└── output/                  # Default artifact output directory
```

Default `hyperbot.yaml`:

```yaml
# User config
user:
  userid: "<auto-generated-uuid>"   # Auto-generated on first run

# Model config
model:
  model: "deepseek-reasoner"
  baseurl: "https://api.deepseek.com"
  apikey: "your-api-key"
  apitype: "openai"          # openai or anthropic
  contextwindow: 64000       # Context window size; affects auto-summary triggering
  stream: true               # Streaming output (live reasoning + tool calls)

# MCP services (HTTP): any number of entries
# type: "sse" or "streamable_http"
mcp:
  - name: "mcpexec"
    enabled: true
    type: "sse"
    endpoint: "http://127.0.0.1:8080/mcp"
    headers: {}              # Optional, e.g. {"Authorization": "Bearer xxx"}
  # - name: "another-mcp"
  #   enabled: true
  #   type: "streamable_http"
  #   endpoint: "http://127.0.0.1:8081/mcp"
  #   headers: {}

# Stdin MCP (subprocess): launch a local MCP server with command + args
stdin_mcp:
  - name: "stdin-tool"
    enabled: true
    command: "npx"
    args: ["-y", "mcp-exec"]
  # - name: "another-stdin-mcp"
  #   enabled: true
  #   command: "node"
  #   args: ["path/to/another/mcp"]
```

> ⚠️ Since 1.3.x, the legacy `bochamcp` / `mcpexec` / `chromemcp` blocks have been merged into a unified `mcp:` list + `stdin_mcp:` list. Old configs need to be migrated.

### Supported Models

| APIType | Examples | Notes |
|---------|----------|-------|
| `openai` | `deepseek-reasoner`, `deepseek-chat`, `gpt-4o`, `gpt-4o-mini` | OpenAI-compatible API; DeepSeek models auto-enable variant adaptation |
| `anthropic` | `claude-3-5-sonnet-20241022`, `claude-3-opus` | Native Anthropic API |

## Quick Start

### Requirements

- **Go 1.26+**
- An API key (OpenAI-compatible or Anthropic)

### 1. Clone & Run

```bash
git clone https://github.com/lfz97/HyperBot.git
cd HyperBot

# Run directly
go run .

# Or build then run
go build .
./HyperBot         # Linux/macOS
./HyperBot.exe     # Windows
```

### 2. First Run

The first launch creates `.hyperbot/hyperbot.yaml` and `.hyperbot/skills/` next to the executable. Edit the config and re-run.

### 3. Interactive Commands

| Input | Action |
|-------|--------|
| Text + `Ctrl+Enter` | Send the message (Enter inserts a newline) |
| `/new` | Start a new conversation (reset Session) |
| `/flush` | Hot-reload config: keep the current Session, only rebuild toolsets (handy after editing `mcp` / `stdin_mcp`) |
| `/exit` | Quit |
| `ESC` | Interrupt the current agent response |

> Long text can be pasted directly: bracketed paste is enabled, so the TUI no longer freezes by inserting characters one at a time.

### 4. Cross-Compilation

```bash
make all          # Build all 5 targets

make linux-x64
make linux-arm64
make macos-x64
make macos-arm64
make windows-x64

make clean        # Clean release/
```

## Build Artifacts

| Platform | Path |
|----------|------|
| Linux x64 | `release/linux-x64/HyperBot` |
| Linux ARM64 | `release/linux-arm64/HyperBot` |
| macOS x64 | `release/macos-x64/HyperBot` |
| macOS ARM64 | `release/macos-arm64/HyperBot` |
| Windows x64 | `release/windows-x64/HyperBot.exe` |

## Skill System

HyperBot uses a **Knowledge-Only** skill profile: skills only inject knowledge context into the agent — there are no per-skill execution tools. All commands run through `localexec`, avoiding duplicated tooling.

Skills live under `.hyperbot/skills/` and are auto-discovered by the framework's `skill.NewFSRepository()`.

### Creating Custom Skills

```
.hyperbot/skills/
└── my-skill/
    ├── SKILL.md           # Skill definition (required)
    └── references/        # Reference documents (optional)
        └── guide.md
```

## SystemPrompt & Execution Protocol

HyperBot bundles a **Traffic Light + Execution Engine** protocol (see `bootstrap/prompt/systemprompt.md`) that classifies tasks as:

- **🟢 Simple Interaction**: a single tool call, a single chained command, or a pure Q&A → execute immediately, skip planning and logs.
- **🔴 Complex Task**: tasks with a data-dependency chain, multi-file refactors, or system-level setup → must present a plan and obtain explicit user approval before running the Execution Engine workflow (which includes progress tracking and writing key steps back to `OperationRecord.md`).
- **🛑 Anti-Overengineering Red Lines**: must not artificially split simple tasks, must not add unsolicited prerequisite checks; obedience over optimization.

At startup the framework fills these placeholders with runtime info: `{{NAME}}` `{{DATE}}` `{{TIMEZONE}}` `{{OSTYPE}}` `{{AARCH}}` `{{HOME}}` `{{TMPDIR}}` `{{CURRENTUSER}}` `{{HOSTNAME}}` `{{CWD}}` `{{CONFIGPATH}}` `{{HyperBotConfig}}` `{{SkillsFolder}}` `{{HyperBotLogFile}}` `{{OperationRecord}}` `{{OUTPUTDIR}}`.

## Logs & Artifacts

| Path | Purpose |
|------|---------|
| `.hyperbot/hyperbot.log` | trpc-agent-go framework logs (redirected from stdout to keep the TUI clean) |
| `.hyperbot/OperationRecord.md` | Markdown operation record appended by the agent during complex tasks |
| `output/` | Default output directory for final task artifacts |

## Dependencies

| Dependency | Purpose |
|------------|---------|
| [trpc-agent-go](https://trpc.group/trpc-go/trpc-agent-go) v1.8.1 | Core agent framework (Runner, LLMAgent, Tool, Skill, Session) |
| [trpc-agent-go/model/anthropic](https://trpc.group/trpc-go/trpc-agent-go) v1.8.0 | Anthropic model adapter |
| [trpc-agent-go/model/tiktoken](https://trpc.group/trpc-go/trpc-agent-go) v1.8.0 | Accurate tiktoken counting (drives auto-summary triggers) |
| [trpc-mcp-go](https://trpc.group/trpc-go/trpc-mcp-go) v0.0.10 | MCP tool protocol support |
| [tview](https://github.com/rivo/tview) v0.42.0 | TUI framework |
| [tcell/v2](https://github.com/gdamore/tcell) v2.13.9 | Terminal event handling (incl. bracketed paste) |
| [openai-go](https://github.com/openai/openai-go) v1.12.0 | OpenAI API SDK |
| [anthropic-sdk-go](https://github.com/anthropics/anthropic-sdk-go) v1.19.0 | Anthropic API SDK |
| [google/uuid](https://github.com/google/uuid) v1.6.0 | Session / Request ID generation |
| [yaml.v2](https://gopkg.in/yaml.v2) v2.4.0 | YAML config parsing |
| [zap](https://go.uber.org/zap) v1.27.1 | Logging |

## License

MIT License
