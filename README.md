# HyperBot

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
# Windows x64
release/windows-x64/HyperBot.exe

# Linux x64
release/linux-x64/HyperBot

# Linux ARM64 (如树莓派)
release/linux-arm64/HyperBot

# macOS x64 (Intel)
release/macos-x64/HyperBot

# macOS ARM64 (Apple Silicon)
release/macos-arm64/HyperBot
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
.\HyperBot.exe

# Linux / macOS
./HyperBot
```

> 💡 **提示**: 首次运行会自动创建 `config.yaml` 配置文件，修改配置后重新运行即可！

## 为什么选择 HyperBot？

| 对比项 | HyperBot | 其他方案 |
|--------|-----------|----------|
| 安装方式 | 下载即用 | 需要 `pip install`、`npm install` 等 |
| 依赖环境 | ✅ 无需 | ❌ 需要 Python/Node.js 运行时 |
| 产物大小 | ~24MB 单文件 | 通常几十MB + 大量依赖文件夹 |
| 跨平台 | 一份编译产物多平台运行 | 可能需要分别配置不同环境 |
| 清理卸载 | 删除文件即可 | 需要逐一卸载依赖包 |

> 🏆 **真正的"绿色软件"**：不污染你的系统环境，拷贝即走！

## 🔧 从源码构建

如果你需要自定义构建或为其他平台编译，可以使用以下方法：

### 环境要求
- Go 1.20+ (推荐 1.25+)
- Git

### 构建方法

#### 方法1：使用批处理脚本（Windows）
```bash
# 一键构建所有平台
build-all.bat
```

#### 方法2：使用Makefile
```bash
# 构建所有平台
make

# 仅构建Windows版本
make build-windows

# 仅构建Linux版本
make build-linux

# 仅构建macOS版本
make build-macos

# 清理构建文件
make clean
```

#### 方法3：手动构建
```bash
# Linux x64
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o release/linux-x64/HyperBot

# Linux ARM64
GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o release/linux-arm64/HyperBot

# macOS x64
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o release/macos-x64/HyperBot

# macOS ARM64
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o release/macos-arm64/HyperBot

# Windows x64
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o release/windows-x64/HyperBot.exe
```

### 构建选项说明
- `-trimpath`: 移除文件系统中的路径信息，使构建更可重现
- `-ldflags="-s -w"`: 移除调试符号和DWARF信息，减小文件大小
- `-o`: 指定输出路径

## 📁 项目结构

```
HyperBot/
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

## 🛠️ 技术栈

- **Go 1.25+** - 静态编译，无运行时依赖
- [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go) 
- YAML 配置 - 简洁易读

## 📄 License

MIT
