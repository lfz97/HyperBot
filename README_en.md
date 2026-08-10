# HyperBot

A terminal-native AI Agent assistant: multi-model (OpenAI / Anthropic), MCP tool integration, local command execution and long-term memory. Single-binary deployment, ready out of the box.

## Key Features

- **Multi-model**: OpenAI-compatible APIs (GPT, DeepSeek, etc.) and Anthropic Claude. Switch models with one config line; DeepSeek variants are auto-detected
- **MCP integration**: SSE / Streamable HTTP / stdin transports, any number of toolsets configured via a YAML list
- **Local command execution**: built-in command lifecycle tools (submit → status → output → intervene → kill). PTY-based, so interactive tools like `ssh` and `sudo` work. The agent can autonomously complete the "write code → compile → run → debug" loop
- **Context management**: automatic session summarization + tool result compaction + on-demand retrieval — long conversations never overflow the model's context window
- **Long-term memory**: key information is auto-extracted to SQLite after each turn and retrieved into context in later conversations
- **Skill system**: drop a `SKILL.md` into `.hyperbot/skills/` to inject domain knowledge
- **Real-time streaming**: reasoning and tool calls visible as they happen

## Quick Start

**Requirements**: Linux / macOS (Windows users: use [WSL](https://learn.microsoft.com/en-us/windows/wsl/) — native Windows lacks PTY support), Go 1.26+ (build only), any OpenAI-compatible or Anthropic API key.

```bash
git clone https://github.com/lfz97/HyperBot.git
cd HyperBot
go build -o HyperBot ./cmd
./HyperBot
```

On first run, `.hyperbot/hyperbot.yaml` is generated automatically. Fill in your API key and restart.

**Controls**:

| Action | What It Does |
|--------|-------------|
| Enter | Send message (Shift+Enter for newline) |
| ESC | Interrupt current response |
| Ctrl+K | Help page |
| `/new` | Start a new conversation |
| `/exit` | Quit |

## Deployment

Single-binary deployment: one executable + one `.hyperbot/` config directory is a complete instance. No Docker, no database, no runtime dependencies. The only requirement is a readable/writable directory.

```bash
# Run from any directory; config directory is auto-generated on first run
./HyperBot
```

Migration is just copying two things — all data (config, memory, skills) is preserved:

```bash
scp HyperBot user@host:/opt/hyperbot/
scp -r .hyperbot user@host:/opt/hyperbot/
```

## Configuration

`.hyperbot/hyperbot.yaml` is auto-generated on first run. Core fields:

```yaml
model:
  model: "deepseek-reasoner"      # Model name
  baseurl: "https://api.deepseek.com"
  apikey: "your-api-key"
  apitype: "openai"               # openai or anthropic
  contextwindow: 64000            # Context window; must be ≤ the model's actual limit
  anthropicAuthHeaderTransfer: false  # true=Authorization Bearer, false=X-Api-Key
  stream: true                    # Streaming output
```

MCP services are configured via the `mcp` (HTTP) and `stdin_mcp` (subprocess) lists. Other options are documented as comments in the generated config file.

## License

MIT
