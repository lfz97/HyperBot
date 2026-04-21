# HyperBot 🤖

基于 [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go) 框架构建的 TUI (终端用户界面) AI Agent 聊天机器人。支持 OpenAI 兼容接口与 Anthropic (Claude) 模型，集成 MCP 工具协议，具备本地命令执行、技能系统扩展和多轮对话记忆等能力。

## 功能特性

- 🖥️ **TUI 交互界面**: 基于 [tview](https://github.com/rivo/tview) 的终端图形界面，包含配置页和对话页双页面切换
- 🤖 **多模型支持**: 兼容 OpenAI API（含 DeepSeek 等兼容提供商）和 Anthropic Claude 系列模型
- 🔄 **流式/非流式输出**: 支持实时流式推理过程展示（Reasoning Content 黄色高亮）和非流式完整响应
- 🔧 **MCP 工具集成**: 通过 Streamable HTTP 协议接入 Bocha 搜索、Chrome 浏览器控制、Shell 远程执行等 MCP 服务
- 💻 **本地命令执行**: 内置 LocalExec 工具集，提供完整的命令生命周期管理（提交 → 启动 → 监控 → 干预 → 终止）
- 📚 **技能系统**: 基于文件系统的技能仓库，自动加载 `skills/` 目录下的知识技能（Knowledge-Only 模式）
- 💬 **多轮对话**: Session 级上下文记忆，支持 `/new` 重置对话、`/exit` 退出、`ESC` 中断当前响应
- 📝 **日记系统**: Agent 自动在 `Diary/` 目录记录复杂任务的执行日志
- 🔄 **交叉编译**: 支持 Linux (x64/ARM64)、macOS (x64/ARM64)、Windows (x64) 五平台构建

## 架构概览

```
用户输入
  │
  ▼
┌─────────────────────────────────────────────────────────┐
│  main.go → tview.Application                            │
│    └── tui/tui.go                                       │
│         ├── ConfigPage (启动配置/初始化日志)              │
│         └── AgentPage  (对话界面: 状态栏+消息区+输入框)   │
├─────────────────────────────────────────────────────────┤
│  bootstrap/                                             │
│    ├── Initializer.go  (日志重定向 → 配置加载 → Agent创建)│
│    └── Bootstrap.go    (对话主循环，管理Session生命周期)   │
├─────────────────────────────────────────────────────────┤
│  handler/                                               │
│    ├── runIteratively.go  (用户交互循环，ESC取消)         │
│    ├── runOnce.go         (单轮Agent执行，事件流处理)     │
│    └── message.go         (流式/非流式消息渲染)           │
├─────────────────────────────────────────────────────────┤
│  agent/                                                 │
│    ├── baseAgent.go       (LLMAgent配置: 技能/工具/提示词)│
│    ├── OpenaiAgent.go     (OpenAI兼容 Agent)             │
│    └── AnthropicAgent.go  (Anthropic Agent)              │
├─────────────────────────────────────────────────────────┤
│  models/                                                │
│    ├── openai.go     (OpenAI适配, DeepSeek变体自动检测)   │
│    └── anthropic.go  (Anthropic适配)                     │
├─────────────────────────────────────────────────────────┤
│  toolsets/                                              │
│    ├── localexec/    (内置本地命令执行工具集, 始终启用)    │
│    ├── bochaMCP.go   (博查搜索 MCP)                      │
│    ├── chromeMCP.go  (Chrome浏览器 MCP)                   │
│    └── shellMCP.go   (远程Shell MCP)                     │
└─────────────────────────────────────────────────────────┘
```

## 项目结构

```
HyperBot/
├── main.go                 # 入口：创建 tview 应用，启动 TUI
├── config.yaml             # 运行时配置（首次运行时自动生成）
├── Makefile                # 交叉编译脚本
├── build.ps1               # Windows PowerShell 构建脚本
│
├── agent/                  # Agent 创建与配置
│   ├── baseAgent.go        #   核心：LLMAgent 选项组装（技能、工具集、系统提示词）
│   ├── OpenaiAgent.go      #   OpenAI 兼容 Agent 工厂
│   └── AnthropicAgent.go   #   Anthropic Agent 工厂
│
├── bootstrap/              # 启动引导
│   ├── Initializer.go      #   初始化流程：日志 → 配置 → 技能目录 → Agent 创建
│   └── Bootstrap.go        #   对话主循环：Session 管理、对话状态轮转
│
├── config/                 # 配置定义
│   ├── config.go           #   配置结构体（Model、User、各MCP配置）
│   ├── yaml_template.go    #   默认配置模板（首次运行写入）
│   └── systemPrompt.go     #   系统提示词（含 {{DATE}}、{{OSTYPE}}、{{DIARYPATH}} 占位符）
│
├── handler/                # 对话处理层
│   ├── model.go            #   数据模型：TurnResult、AgentRunner、对话状态码
│   ├── runIteratively.go   #   交互循环：用户输入 → 指令解析 → Agent调用 → 状态返回
│   ├── runOnce.go          #   单轮执行：Runner.Run() → Event 流消费
│   └── message.go          #   消息渲染：流式/非流式、推理内容、工具调用/结果
│
├── models/                 # 模型适配层
│   ├── openai.go           #   OpenAI 模型初始化（含 DeepSeek 变体自动检测）
│   └── anthropic.go        #   Anthropic 模型初始化
│
├── toolsets/               # 工具集
│   ├── localexec/          #   内置本地命令执行（始终启用）
│   │   ├── definition.go   #     ToolSet 接口实现
│   │   ├── tools.go        #     6 个工具：submit/start/status/output/intervene/kill
│   │   ├── manager.go      #     进程管理器：命令提交、启动、信号、输出捕获
│   │   ├── model.go        #     数据模型：Job、Manager、状态常量
│   │   └── cache.go        #     全局 Manager 单例
│   ├── bochaMCP.go         #   博查搜索 MCP ToolSet
│   ├── chromeMCP.go        #   Chrome 浏览器控制 MCP ToolSet
│   └── shellMCP.go         #   远程 Shell MCP ToolSet
│
├── functionTools/          # 示例功能工具
│   └── BookRecommender.go  #   图书搜索工具（示例/模板）
│
├── tui/                    # TUI 界面
│   └── tui.go              #   双页面布局：ConfigPage（启动日志）→ AgentPage（对话）
│
├── utils/                  # 工具库
│   └── pretty/
│       └── pretty.go       #   终端美化输出（ANSI颜色 + tview 标签 双套方案）
│
└── release/                # 编译产物
    ├── linux-arm64/
    ├── linux-x64/
    ├── macos-arm64/
    ├── macos-x64/
    └── windows-x64/
```

## 核心流程

### 启动流程

1. **`main.go`** 创建 `tview.Application`，加载 `ConfigPage`
2. **`bootstrap.Init()`** 在后台 goroutine 中异步执行：
   - 将框架日志重定向到 `hyperbot.log` 文件
   - 检查/创建 `config.yaml`（首次运行自动生成，含随机 UserID）
   - 加载并解析 YAML 配置
   - 检查/创建 `skills/` 目录
   - 替换系统提示词中的占位符（日期、操作系统类型、日记路径）
   - 根据 `APIType` 创建对应的 LLMAgent 和 Runner
3. 初始化完成后，通过 `app.QueueUpdateDraw()` 切换到 `AgentPage`

### 对话流程

1. **`bootstrap.AgentStart()`** 启动对话主循环
2. **`handler.AgentRunIteratively()`** 等待用户输入：
   - `/exit` → 退出程序
   - `/new` → 重置 Session 开始新对话
   - `ESC` → 取消当前 Agent 响应（通过 `context.WithCancel`）
   - 正常文本 → 调用 `AgentRunOnce()`
3. **`handler.AgentRunOnce()`** 调用 `runner.Run()` 获取事件流，逐事件渲染到 TUI
4. **`handler.printMessage()`** 处理消息渲染：
   - **推理内容**: `»` 开始 → 黄色暗色文本 → `«` 结束
   - **正文内容**: 直接输出
   - **工具调用**: 洋红色 `⚙` 图标 + 工具名 + 参数
   - **工具结果**: 灰色缩进展示

### 本地命令执行 (LocalExec)

Agent 通过 6 个工具实现完整的命令生命周期管理：

| 工具 | 功能 |
|------|------|
| `submit_command` | 提交命令（指定进程名和参数），返回命令 ID |
| `start_command` | 根据 ID 启动已提交的命令 |
| `get_status` | 查询命令状态（pending/running/done/failed/killed） |
| `get_output` | 获取 stdout/stderr 输出，支持窗口大小限制 |
| `intervene_command` | 向运行中的命令写入 stdin 或发送信号 |
| `kill_command` | 强制终止命令 |

## 配置说明

首次运行时会在可执行文件同目录自动生成 `config.yaml`。也可手动创建：

```yaml
# 用户配置
user:
  userid: "auto-generated-uuid"  # 自动生成，用于 Session 标识

# 模型配置
model:
  model: "deepseek-reasoner"              # 模型名称
  baseurl: "https://api.deepseek.com"     # API 端点
  apikey: "your-api-key"                  # API 密钥
  apitype: "openai"                       # 模型类型: openai 或 anthropic
  stream: true                            # 流式输出（可实时查看推理过程和工具调用）

# 博查 MCP 搜索（可选）
bochamcp:
  enabled: false
  apikey: "your-api-key"
  mcptype: "streamable_http"
  mcpendpoint: "https://mcp.bochaai.com/mcp"

# MCP Exec 远程Shell（可选，项目: https://github.com/lfz97/mcp-exec）
mcpexec:
  enabled: false
  mcptype: "streamable_http"
  mcpendpoint: "http://1.2.3.4:8080/mcp"

# Chrome MCP 浏览器控制（可选，项目: https://github.com/hangwin/mcp-chrome）
# 注意：开始新对话前需在 Chrome 中重启 MCP 端点
chromemcp:
  enabled: false
  mcptype: "streamable_http"
  mcpendpoint: "http://127.0.0.1:12306/mcp"
```

### 支持的模型

| APIType | 模型示例 | 说明 |
|---------|----------|------|
| `openai` | `deepseek-reasoner`, `deepseek-chat`, `gpt-4o`, `gpt-4o-mini` | OpenAI 兼容接口，DeepSeek 模型自动启用变体适配 |
| `anthropic` | `claude-3-5-sonnet-20241022`, `claude-3-opus` | Anthropic 原生接口 |

## 快速开始

### 环境要求

- **Go 1.25+**
- API 密钥（OpenAI 兼容 / Anthropic）

### 1. 克隆与构建

```bash
git clone https://github.com/your-repo/HyperBot.git
cd HyperBot

# 直接运行
go run .

# 或编译后运行
go build .
./HyperBot        # Linux/macOS
./HyperBot.exe    # Windows
```

### 2. 首次运行

首次启动时会自动在可执行文件同目录生成 `config.yaml` 和 `skills/` 目录，修改配置后重新运行即可。

### 3. 交互命令

| 命令 | 功能 |
|------|------|
| 输入文本 + `Enter` | 发送消息 |
| `/new` | 开始新对话（重置 Session） |
| `/exit` | 退出程序 |
| `ESC` | 中断当前 Agent 响应 |

### 4. 交叉编译

```bash
# 编译所有平台
make all

# 编译特定平台
make linux-x64
make linux-arm64
make macos-arm64
make macos-x64
make windows-x64

# 清理编译产物
make clean
```

## 编译产物

| 平台 | 路径 |
|------|------|
| Linux x64 | `release/linux-x64/HyperBot` |
| Linux ARM64 | `release/linux-arm64/HyperBot` |
| macOS x64 | `release/macos-x64/HyperBot` |
| macOS ARM64 | `release/macos-arm64/HyperBot` |
| Windows x64 | `release/windows-x64/HyperBot.exe` |

## 技能系统

HyperBot 使用 Knowledge-Only 模式加载技能：技能仅注入知识上下文，所有命令执行统一通过 LocalExec 工具集。

技能目录位于可执行文件同级的 `skills/` 文件夹（首次运行自动创建），由框架的 `skill.NewFSRepository()` 自动发现和加载。

### 创建自定义技能

```
skills/
└── my-skill/
    ├── SKILL.md           # 技能定义（必须）
    └── references/        # 参考文档（可选）
        └── guide.md
```

## 日志

框架日志输出到可执行文件同目录下的 `hyperbot.log`，避免干扰 TUI 界面。

## 依赖

| 依赖 | 用途 |
|------|------|
| [trpc-agent-go](https://trpc.group/trpc-go/trpc-agent-go) v1.8.1 | Agent 框架核心（Runner、LLMAgent、Tool、Skill） |
| [trpc-agent-go/model/anthropic](https://trpc.group/trpc-go/trpc-agent-go) v1.8.0 | Anthropic 模型适配 |
| [trpc-mcp-go](https://trpc.group/trpc-go/trpc-mcp-go) v0.0.10 | MCP 工具协议支持 |
| [tview](https://github.com/rivo/tview) v0.42.0 | TUI 终端界面框架 |
| [tcell/v2](https://github.com/gdamore/tcell) v2.13.8 | 终端底层事件处理 |
| [openai-go](https://github.com/openai/openai-go) v1.12.0 | OpenAI API SDK |
| [anthropic-sdk-go](https://github.com/anthropics/anthropic-sdk-go) v1.19.0 | Anthropic API SDK |
| [google/uuid](https://github.com/google/uuid) v1.6.0 | Session/Request ID 生成 |
| [yaml.v2](https://gopkg.in/yaml.v2) v2.4.0 | YAML 配置解析 |
| [zap](https://go.uber.org/zap) v1.27.0 | 日志框架 |

## 许可证

MIT License
