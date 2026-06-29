# HyperBot 🤖

A terminal-native AI Agent chatbot — multi-model (OpenAI / Anthropic), unified MCP tool integration, full local command lifecycle management, ready to go out of the box.

## System Requirements

- **Operating System**: Linux / macOS (recommended). Windows users should use [WSL](https://learn.microsoft.com/en-us/windows/wsl/) (WSL 1 or WSL 2). Native Windows lacks PTY support — command execution falls back to plain pipes, and AI agents have significantly higher failure rates when operating PowerShell.
- **Go 1.26+** (only needed for source builds; pre-built binaries also available)
- An OpenAI-compatible or Anthropic API key

## What It Does

### 🖥️ AI Assistant in Your Terminal

A two-page TUI built on [tview](https://github.com/rivo/tview): config page → chat page. Mouse and bracketed paste enabled — paste long text without freezing. Everything happens in the terminal. No browser needed.

### 🤖 Multi-Model Freedom

Works with OpenAI-compatible APIs (GPT-4o, DeepSeek, etc.) and Anthropic Claude models. Switch models with one config line. DeepSeek models get automatic variant adaptation.

### 🔄 Real-Time Streaming

Both streaming and non-streaming modes. In streaming mode, reasoning content is highlighted in dim yellow, and tool calls appear in real time — you always see what the agent is thinking and doing.

### 🔧 Unified MCP Integration

Add any number of MCP toolsets via YAML. Three transport modes:

| Transport | Use Case |
|-----------|----------|
| SSE | HTTP Server-Sent Events, ideal for remote MCP services |
| Streamable HTTP | HTTP streaming for modern MCP servers |
| Stdin MCP | Launch a local subprocess via `command + args`, e.g. `npx -y mcp-exec` |

### 💻 Full Command Lifecycle

The built-in `localexec` toolset manages the complete command lifecycle through PTY (pseudo-terminal), supporting interactive tools like `ssh`, `sudo`, and `msfconsole` without breaking the TUI layout:

```
Submit → Start → Poll Status → Read Output → Intervene (stdin / signals) → Kill
```

The agent can autonomously complete the "write code → compile → run → debug" loop.

> **Note**: PTY is fully available on Linux/macOS. Native Windows does not support PTY — command execution degrades to plain pipes, and interactive tools will not work correctly. **Windows users should use WSL.**

### 📚 Knowledge-Only Skill System

`SKILL.md` files under `.hyperbot/skills/` are auto-loaded as knowledge context. Skills inject guidance only — no per-skill execution tools. All commands go through `localexec`, avoiding tool duplication.

Create a custom skill with just two files:

```
.hyperbot/skills/my-skill/
├── SKILL.md           # Skill definition (required)
└── references/        # Reference docs (optional)
    └── guide.md
```

### 🧠 Smart Session Summarization

Long conversations are automatically compressed so you never hit context limits:

- **Accurate token counting**: Uses [tiktoken](https://github.com/tiktoken-go/tokenizer) for model-accurate counts, far better than character-based estimates
- **Multi-condition triggers** (any one fires): events ≥ 20 / new tokens ≥ 150K / idle > 10 min
- **Structured output**: Enforces a 9-section template for consistent compression quality
- **Live echo**: Generated summaries appear in green in the chat area
- **Async execution**: Background compression, never blocks the conversation

### 💬 Session Management

- **Multi-turn memory**: Session-level context preserved across turns
- **`/new`**: Start fresh anytime
- **`/flush`**: Hot-reload MCP config without losing conversation history
- **`ESC`**: Cancel the agent's current response instantly
- **Self-recovery**: On errors, the agent constructs a recovery prompt and tries again — transient 5xx errors won't kill your session

### 🧠 Long-Term Memory (Auto Memory)

Key information is automatically extracted after each conversation and persisted. The next conversation intelligently retrieves and injects relevant memories into context:

- **Auto Extraction**: After each turn, a background LLM asynchronously analyzes the conversation and extracts facts and events
- **Adaptive Preload**: When memory is small, all entries are injected. When large, semantic search finds the top-N most relevant ones
- **SQLite Persistence**: Single-file `memory.db`, zero maintenance, survives restarts
- **Hybrid Mode**: The agent has `memory_search` / `memory_load` tools to actively search any historical memory
- **Deduplication**: Duplicate information is auto-merged as updates, never creating redundant entries

### 🔨 Build

```bash
# Linux / macOS
./build.sh

# Windows PowerShell (not recommended — use WSL instead)
.\build.ps1
```

Output goes to the `release/` directory.

## Quick Start

### Requirements

- **Operating System**: Linux / macOS. Windows users should run inside WSL.
- **Go 1.26+** (for building)
- An OpenAI-compatible or Anthropic API key

### Install & Run

```bash
git clone https://github.com/lfz97/HyperBot.git
cd HyperBot

# Run directly
go run .

# Or build first
go build .
./HyperBot
```

The first run auto-generates the config file and skills directory. Edit your API key and restart.

### Windows Users

HyperBot depends on PTY (pseudo-terminal), which is not available on native Windows. Clone and run inside WSL:

```powershell
# Install WSL from PowerShell (skip if already installed)
wsl --install

# Enter WSL, then follow the Linux instructions above
wsl
git clone https://github.com/lfz97/HyperBot.git
cd HyperBot
go run .
```

### Interactive Commands

| Action | What It Does |
|--------|-------------|
| Type text + `Ctrl+Enter` | Send message (Enter for newline) |
| `/new` | Start a new conversation, reset context |
| `/flush` | Hot-reload config, keep session history |
| `/exit` | Quit |
| `ESC` | Interrupt current agent response |

## Configuration

Auto-generated on first run as `.hyperbot/hyperbot.yaml`:

```yaml
user:
  userid: "<auto-generated-uuid>"

model:
  model: "deepseek-reasoner"      # Model name
  baseurl: "https://api.deepseek.com"
  apikey: "your-api-key"
  apitype: "openai"               # openai or anthropic
  contextwindow: 64000            # Context window size
  stream: true                    # Enable streaming output

# HTTP MCP services (unlimited entries)
mcp:
  - name: "my-mcp"
    enabled: true
    type: "sse"                   # sse or streamable_http
    endpoint: "http://127.0.0.1:8080/mcp"
    headers: {}
    # headers: {"Authorization": "Bearer xxx"}

# Stdin MCP (launch local MCP server as subprocess)
stdin_mcp:
  - name: "local-tool"
    enabled: true
    command: "npx"
    args: ["-y", "mcp-exec"]
```

### Supported Models

| API Type | Examples |
|----------|----------|
| `openai` | `deepseek-reasoner`, `deepseek-chat`, `gpt-4o`, `gpt-4o-mini` |
| `anthropic` | `claude-sonnet-4-6`, `claude-opus-4-6` |

## Runtime Directory Layout

```
<Next to executable>/
├── .hyperbot/
│   ├── hyperbot.yaml        # Main config
│   ├── memory.db            # Long-term memory database
│   ├── skills/              # Skill repository
│   ├── hyperbot.log         # Background logs
└── output/                  # Task output directory
```

## Deployment & Portability

HyperBot is **single-binary** — one executable + one config directory is a complete runtime instance. No Docker, no database server, no runtime dependencies.

### Deploy

```bash
# Build or download the binary, then run from any directory
./HyperBot
# The .hyperbot/ config directory is auto-generated on first run
```

The only requirement: the directory containing the binary must be **readable and writable**.

### Migrate

Copy the binary and `.hyperbot/` directory to another machine. **All data is preserved**:

```bash
# Migrate to a new machine
scp HyperBot user@new-host:/opt/hyperbot/
scp -r .hyperbot user@new-host:/opt/hyperbot/
```

`.hyperbot/` directory contents:

| File/Dir | Contents |
|----------|----------|
| `hyperbot.yaml` | API key, model, MCP tool config |
| `memory.db` | Long-term memory (extracted conversation insights) |
| `skills/` | Custom skill definitions |
| `hyperbot.log` | Runtime logs |

> **Note**: `hyperbot.yaml` contains your API key. Ensure the target environment is secure before migrating.


## License

MIT License
