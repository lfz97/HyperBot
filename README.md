# trpcagent

> 🏃 **一个干净、跨平台的 AI Agent 工具** | 下载即可运行，无需安装依赖！

基于 [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go) 框架开发的 AI Agent，支持本地命令执行、MCP 工具扩展和多模型接入。

## ✨ 核心亮点

| 特性 | 说明 |
|------|------|
| 📦 **单文件产物** | 编译后仅一个可执行文件，无额外依赖 |
| 🌍 **跨平台** | 支持 Windows、Linux、macOS，x64 & ARM64 |
| ⚡ **开箱即用** | 下载运行，无需安装任何运行时或库 |
| 🎯 **干净简洁** | 配置简单，零外部依赖，即下即用 |

## 功能特性

- 🤖 **多模型支持** - 支持 OpenAI、Anthropic 等主流 LLM
- 🛠️ **本地命令执行** - 支持在终端执行本地命令
- 🔌 **MCP 协议支持** - 集成博查 MCP、Chrome MCP、Shell MCP
- 💭 **思考链** - 支持推理模型（Reasoning）
- 📝 **日志记录** - 自动记录任务执行日志

## 🚀 快速开始

### 下载运行

无需安装 Go、无需配置环境！直接下载对应平台的编译产物即可运行：

```bash
# Windows (.exe)
# 下载 release/windows x64/trpcagent.exe

# Linux
# 下载 release/linux x64/trpcagent

# macOS (Intel)
# 下载 release/macos/trpcagent

# macOS (Apple Silicon)
# 下载 release/arm64/trpcagent
```

### 配置

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
# Windows
.\trpcagent.exe

# Linux / macOS
./trpcagent
```

> 💡 **提示**: 首次运行会自动创建 `config.yaml` 配置文件，修改配置后重新运行即可！

## 为什么选择 trpcagent？

| 对比项 | trpcagent | 其他方案 |
|--------|-----------|----------|
| 安装方式 | 下载即用 | 需要 `pip install`、`npm install` 等 |
| 依赖环境 | ✅ 无需 | ❌ 需要 Python/Node.js 运行时 |
| 产物大小 | ~10MB 单文件 | 通常几十MB + 大量依赖文件夹 |
| 跨平台 | 一份编译产物多平台运行 | 可能需要分别配置不同环境 |
| 清理卸载 | 删除文件即可 | 需要逐一卸载依赖包 |

> 🏆 **真正的"绿色软件"**：不污染你的系统环境，拷贝即走！

## 📁 项目结构

```
trpcagent/
├── main.go              # 程序入口
├── bootstrap/           # 启动初始化
├── handler/             # 核心运行逻辑
├── agent/               # Agent 实现 (OpenAI/Anthropic)
├── config/              # 配置管理
├── toolsets/            # 工具集 (本地命令/MCP)
├── models/              # 模型封装
├── myutils/             # 工具函数
└── utils/               # 辅助工具
```

> 📥 **使用方式**: 直接从 `release/` 目录下载对应平台的可执行文件即可！

## 编译

```bash
# 本地编译
go build -trimpath -ldflags="-s -w"

# 交叉编译
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o release/linux\ x64/trpcagent
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o release/macos/trpcagent
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "release/windows x64/trpcagent.exe"
```

## 🛠️ 技术栈

- **Go 1.25+** - 静态编译，无运行时依赖
- [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go) 
- YAML 配置 - 简洁易读

## 📄 License

MIT
