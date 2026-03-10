# trpcagent

基于 [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go) 框架开发的 AI Agent，支持本地命令执行、MCP 工具扩展和多模型接入。

## 功能特性

- 🤖 **多模型支持** - 支持 OpenAI、Anthropic 等主流 LLM
- 🛠️ **本地命令执行** - 支持在终端执行本地命令
- 🔌 **MCP 协议支持** - 集成博查 MCP、Chrome MCP、Shell MCP
- 💭 **思考链** - 支持推理模型（Reasoning）
- 📝 **日志记录** - 自动记录任务执行日志

## 快速开始

### 1. 配置

首次运行会在可执行文件同级目录创建 `config.yaml` 配置文件：

```yaml
# 模型配置
model:
  model: "deepseek-reasoner"
  baseurl: "https://api.deepseek.com"
  apikey: "your-api-key"
  apitype: "openai"

# 博查 MCP 配置
bochamcp:
  enabled: false
  apikey: ""
  mcptype: "streamable_http"
  mcpendpoint: "https://mcp.bochaai.com/mcp"

# Chrome MCP 配置
chromemcp:
  enabled: false
  mcptype: "stdio"
  command: "npx"
  args:
    - "-y"
    - "chrome-devtools-mcp@latest"
    - "--slim"
    - "--headless"
```

### 2. 运行

```bash
# Linux / macOS
./trpcagent

# Windows
.\trpcagent.exe
```

## 目录结构

```
trpcagent/
├── main.go              # 程序入口
├── bootstrap/           # 启动初始化
│   ├── Bootstrap.go
│   └── Initializer.go
├── handler/             # 核心运行逻辑
│   └── run.go
├── agent/               # Agent 实现
│   ├── OpenaiAgent.go
│   └── AnthropicAgent.go
├── config/              # 配置管理
│   ├── Config.go
│   ├── builtin.go
│   └── templete.go
├── toolsets/            # 工具集
│   ├── localexec/       # 本地命令执行
│   ├── bochaMCP.go
│   ├── chromeMCP.go
│   └── shellMCP.go
├── models/              # 模型封装
├── myutils/             # 工具函数
├── utils/               # 辅助工具
└── release/             # 编译产物
    ├── windows x64/
    ├── linux x64/
    ├── macos/
    └── arm64/
```

## 编译

```bash
# 本地编译
go build -trimpath -ldflags="-s -w"

# 交叉编译
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o release/linux\ x64/trpcagent
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o release/macos/trpcagent
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "release/windows x64/trpcagent.exe"
```

## 技术栈

- Go 1.25+
- [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go)
- YAML 配置

## License

MIT
