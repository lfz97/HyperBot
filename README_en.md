# HyperBot 🤖

An AI Agent chatbot built on the trpc-agent-go framework, supporting multiple LLM providers and MCP tool integration.

## Features

- 🤖 **Multi-Model Support**: Supports OpenAI and Anthropic (Claude) model series
- 🔧 **MCP Tool Integration**: Supports external tools like Bocha MCP, Chrome MCP
- 💻 **Local Code Execution**: Supports local code execution capability
- 📚 **Skill System**: Extensible custom skills (skills)
- 💬 **Interactive Chat**: Multi-turn conversation with context memory
- 🔄 **Cross-Compilation**: Supports Linux, macOS, Windows multi-platform

## Project Structure

```
HyperBot/
├── agent/              # Agent core implementation
│   ├── AnthropicAgent.go
│   ├── baseAgent.go
│   └── OpenaiAgent.go
├── bootstrap/          # Bootstrapping
├── config/             # Configuration files
├── functionTools/      # Function tools
├── handler/            # Message handling
├── models/             # Model adapters
├── myutils/            # Utilities
├── skills/             # Skills directory
├── toolsets/           # Tool sets
├── utils/              # Utility libraries
└── release/            # Build artifacts
```

## Configuration

Before running, create a `config.yaml` configuration file:

```yaml
# Model configuration
model:
  Model: "claude-3-5-sonnet-20241022"    # Model name
  BaseURL: "https://api.anthropic.com"    # API address
  APIKey: "your-api-key"                  # API key
  APIType: "anthropic"                    # Model type: openai or anthropic
  Stream: true                             # Enable streaming

# MCP tool configuration (optional)
bochamcp:
  Enabled: false
  APIKey: ""
  MCPtype: "bocha"
  MCPEndpoint: ""

mcpexec:
  Enabled: false
  MCPtype: "exec"
  MCPEndpoint: ""

chromemcp:
  Enabled: false
  MCPtype: "chrome"
  MCPEndpoint: ""
```

## Quick Start

### 1. Clone the project

```bash
git clone https://github.com/your-repo/HyperBot.git
cd HyperBot
```

### 2. Configure

```bash
cp config/templete.yaml config.yaml
# Edit config.yaml and add your API Key
```

### 3. Run

```bash
# Run directly
go run .

# Or use compiled binary
./release/linux-x64/HyperBot
```

### 4. Cross-Compilation

```bash
# Build all platforms
make all

# Build specific platform
make build-linux-x64
make build-windows-x64

# Clean
make clean
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

HyperBot supports extending functionality through the skills directory. Each skill contains:

- `SKILL.md`: Skill definition file
- `references/`: Reference documentation directory

### Creating Custom Skills

Create a new skill in the `skills/` directory:

```
skills/
└── my-skill/
    ├── SKILL.md
    └── references/
        └── guide.md
```

## Requirements

- Go 1.25+
- API Key (OpenAI or Anthropic)

## Dependencies

- [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go) - Core framework
- [anthropic-sdk-go](https://github.com/anthropics/anthropic-sdk-go) - Anthropic API
- [google/uuid](https://github.com/google/uuid) - UUID generation
- [yaml.v2](https://gopkg.in/yaml.v2) - YAML configuration parsing

## License

MIT License
