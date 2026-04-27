# HyperBot 🤖

基于 [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go) 框架构建的 TUI（终端用户界面）AI Agent 聊天机器人。支持 OpenAI 兼容接口与 Anthropic（Claude）模型，统一接入任意 MCP 工具（SSE / Streamable HTTP / stdin），具备本地命令全生命周期管理、Knowledge-Only 技能体系、自动会话摘要等能力。

## 功能特性

- 🖥️ **TUI 交互界面**：基于 [tview](https://github.com/rivo/tview) 的双页面终端图形界面（启动配置页 → 对话页），鼠标 + bracketed paste 全启用，长文本粘贴不再卡顿
- 🤖 **多模型支持**：兼容 OpenAI API（含 DeepSeek 等兼容提供商，DeepSeek 自动启用变体适配）和 Anthropic Claude 系列
- 🔄 **流式 / 非流式输出**：支持实时流式推理过程展示（Reasoning Content 黄色高亮）和非流式完整响应
- 🔧 **统一 MCP 接入**：通过列表配置任意数量的 MCP 工具集
  - HTTP 类：`sse` 与 `streamable_http`
  - 本地类：`stdin_mcp`（用 `command + args` 启动子进程，例如 `npx -y mcp-exec`）
- 💻 **本地命令全生命周期**：内置 `localexec` 工具集，提供 6 个工具实现完整闭环（提交 → 启动 → 状态轮询 → 输出读取 → 干预 stdin/信号 → 强制终止）
- 📚 **Knowledge-Only 技能体系**：基于文件系统的技能仓库，自动加载 `.hyperbot/skills/` 下的 SKILL.md，仅注入知识上下文，所有命令统一交由 `localexec` 执行
- 🧠 **自动会话摘要（Session Summarizer）**：基于 [tiktoken](https://github.com/tiktoken-go/tokenizer) 精准计 token，多触发条件 OR 组合自动压缩历史，并把生成的摘要绿色实时回显到 TUI 对话区
- 💬 **多轮对话与会话指令**：Session 级上下文记忆 + `/new` 重置会话、`/flush` 热重载工具配置、`/exit` 退出、`ESC` 中断当前响应
- 📝 **操作记录**：Agent 在复杂任务中将关键操作以 Markdown 追加到 `.hyperbot/OperationRecord.md`，便于跨会话回溯
- 🔄 **交叉编译**：支持 Linux (x64/ARM64)、macOS (x64/ARM64)、Windows (x64) 五平台一键构建

## 架构概览

```
用户输入
  │
  ▼
┌─────────────────────────────────────────────────────────┐
│  main.go → tui.TuiInit (tview.Application)              │
│    └── tui/tui.go                                       │
│         ├── ConfigPage  (启动 Banner + 初始化日志)        │
│         └── AgentPage   (对话: Sidebar + 消息区 + 输入框) │
├─────────────────────────────────────────────────────────┤
│  bootstrap/                                             │
│    ├── Initializer.go  (日志重定向 → 配置加载 → SystemPrompt 填充 → Agent/Session 创建) │
│    └── Bootstrap.go    (主循环: New / Continue / Flush / Int / Exit 状态机) │
├─────────────────────────────────────────────────────────┤
│  handler/                                               │
│    ├── runIteratively.go  (用户交互循环 + ESC 取消 + /flush 等指令解析) │
│    ├── runOnce.go         (单轮 Runner.Run，事件流消费) │
│    └── message.go         (流式 / 非流式渲染、推理块、工具调用 / 结果) │
├─────────────────────────────────────────────────────────┤
│  agent/                                                 │
│    ├── baseAgent.go       (LLMAgent 选项装配: 技能仓库 / 工具集 / SystemPrompt / 摘要注入) │
│    ├── OpenaiAgent.go     (OpenAI 兼容工厂)             │
│    └── AnthropicAgent.go  (Anthropic 工厂)              │
├─────────────────────────────────────────────────────────┤
│  session/                                               │
│    ├── memSessionService.go (内存 Session 服务 + 异步摘要)│
│    ├── summarizer.go        (Summarizer + 触发器 + PostSummaryHook 回显) │
│    ├── toolformatter.go     (摘要输入中工具调用 / 结果的紧凑格式) │
│    └── prompt/{system,user}.md (摘要 system / user 提示词) │
├─────────────────────────────────────────────────────────┤
│  models/                                                │
│    ├── openai.go     (OpenAI 适配, DeepSeek 自动检测)    │
│    └── anthropic.go  (Anthropic 适配)                    │
├─────────────────────────────────────────────────────────┤
│  toolsets/                                              │
│    ├── localexec/    (内置本地命令执行工具集, 始终启用)   │
│    ├── mcp.go        (HTTP MCP: sse / streamable_http)   │
│    └── stdinMCP.go   (Stdin MCP: command + args 启动子进程)│
└─────────────────────────────────────────────────────────┘
```

## 项目结构

```
HyperBot/
├── main.go                       # 入口：调用 tui.TuiInit() 启动 tview 应用
├── Makefile                      # 五平台交叉编译脚本
├── build.ps1                     # Windows PowerShell 构建脚本
│
├── agent/                        # Agent 创建与配置
│   ├── baseAgent.go              #   核心：LLMAgent 选项组装
│   ├── OpenaiAgent.go            #   OpenAI 兼容 Agent 工厂
│   └── AnthropicAgent.go         #   Anthropic Agent 工厂
│
├── bootstrap/                    # 启动引导
│   ├── Initializer.go            #   日志重定向 → 配置 → 技能目录 → SystemPrompt 占位符 → Agent / Session 创建
│   ├── Bootstrap.go              #   对话主循环：Session 管理、TurnResult 状态轮转
│   └── prompt/
│       └── systemprompt.md       #   全局 System Prompt（含 Traffic Light 协议、Execution Engine 工作流）
│
├── config/                       # 配置定义
│   ├── baseConfig.go             #   Config / Model / User 结构体
│   ├── mcpConfig.go              #   MCP（HTTP）配置：sse / streamable_http
│   ├── stdinMcpConfig.go         #   StdinMCP（子进程）配置
│   └── yaml_template.go          #   首次运行写入的默认 YAML
│
├── handler/                      # 对话处理层
│   ├── model.go                  #   TurnResult / TurnCode（New, Continue, Flush, Int, Error, Exit）+ AgentRunner
│   ├── runIteratively.go         #   交互循环：输入捕获、Ctrl+Enter 提交、指令分派、ESC 取消
│   ├── runOnce.go                #   单轮执行：Runner.Run() → Event 流消费 → TerminalError 处理
│   └── message.go                #   消息渲染：流式 / 非流式、推理块、工具调用 / 结果
│
├── models/                       # 模型适配层
│   ├── openai.go                 #   OpenAI 模型初始化（含 DeepSeek 变体自动检测）
│   └── anthropic.go              #   Anthropic 模型初始化
│
├── session/                      # 会话与摘要
│   ├── memSessionService.go      #   InMemory SessionService + 异步摘要队列
│   ├── summarizer.go             #   Summarizer 工厂：tiktoken 计数 + 多触发器 + PostSummaryHook
│   ├── toolformatter.go          #   摘要输入中工具调用/结果的紧凑格式（含截断）
│   └── prompt/
│       ├── system.md             #   摘要 System Prompt（强制 <analysis> + <summary> 两段输出）
│       └── user.md               #   摘要 User Prompt（9 段结构化模板）
│
├── toolsets/                     # 工具集
│   ├── localexec/                #   内置本地命令执行（始终启用）
│   │   ├── definition.go         #     ToolSet 接口实现
│   │   ├── tools.go              #     6 个工具：submit / start / status / output / intervene / kill
│   │   ├── manager.go            #     进程管理器：命令提交、启动、信号、stdin、输出捕获
│   │   ├── model.go              #     Job / Manager / 状态常量
│   │   └── cache.go              #     全局 Manager 单例
│   ├── mcp.go                    #   HTTP MCP（sse / streamable_http）
│   └── stdinMCP.go               #   Stdin MCP（subprocess）
│
├── functionTools/                # 示例 Function Tool
│   └── BookRecommender.go
│
├── tui/                          # TUI 界面
│   ├── tui.go                    #   双页面布局 + Banner + 鼠标 / bracketed paste
│   ├── global_object/            #   全局 *tview 组件单例
│   └── tip/                      #   状态栏滚动提示 / 侧边栏提示文案
│
├── utils/                        # 工具库
│   └── pretty/                   #   终端美化输出（ANSI / tview tag 双套方案）
│
└── release/                      # 交叉编译产物
    ├── linux-arm64/  linux-x64/  macos-arm64/  macos-x64/  windows-x64/
```

> 运行时数据全部位于可执行文件同目录的 `.hyperbot/` 与 `output/` 下，不会污染源码仓库。

## 核心流程

### 启动流程

1. `main.go` 调用 `tui.TuiInit()`：创建 `tview.Application`，加载 `ConfigPage`（Banner + 日志区），开启鼠标与 bracketed paste。
2. 后台 goroutine 中执行 `bootstrap.Init("HyperBot")`：
   - 取可执行文件所在目录为 `CWD`
   - 检查 / 创建配置目录 `.hyperbot/`
   - 检查 / 创建配置文件 `.hyperbot/hyperbot.yaml`（首次运行用 `yaml_template.go` 写入，并替换 `{USERID}` 为随机 UUID）
   - 检查 / 创建技能目录 `.hyperbot/skills/`
   - 将 trpc-agent-go 框架日志重定向到 `.hyperbot/hyperbot.log`，避免污染 TUI
   - 加载并解析 `hyperbot.yaml`
   - 用运行时信息（日期 / 时区 / OS / 架构 / 用户 / 主机名 / `CWD` / `OUTPUTDIR` 等）填充 SystemPrompt 占位符
   - 创建内存 SessionService（含异步 Summarizer），创建 Agent + Runner
3. 初始化完成后 `app.QueueUpdateDraw()` 切换到 `AgentPage`。

### 对话主循环（`bootstrap.AgentStart`）

`handler.TurnResult` 状态机驱动：

| Code | 含义 | 行为 |
|------|------|------|
| `New` | 新对话 | 重置 sessionID / requestID，进入用户输入 |
| `Continue` | 单轮正常结束 | 保留 sessionID，等待下一次输入 |
| `Int` | 用户 ESC 中断 | 不打印额外提示，等待下一次输入 |
| `Error` | 单轮发生错误 | 自动以错误信息构造 prompt，让 Agent 自我修复 |
| `Flush` | 用户 `/flush` | 重新加载配置 → 关闭旧 Runner → 用最新工具配置创建新 Runner，sessionID 不变 |
| `Exit` | 用户 `/exit` | 关闭 Runner、退出 tview 应用 |

### 单轮执行（`handler.AgentRunOnce`）

调用 `runner.Run(ctx, userID, sessionID, msg)` 拿到事件 channel，逐事件渲染：

- **推理内容（reasoning）**：以 `»` 开始 → 黄色暗色文本 → `«` 结束
- **正文内容**：直接输出
- **工具调用**：洋红色 `⚙` + 工具名 + 参数
- **工具结果**：灰色缩进展示
- 仅 `IsTerminalError()` 才会中断对话；非终端错误自动 `continue`，对话尽量不被一次性 5xx 等问题打断
- 全程响应 `ctx.Done()`（即用户 ESC），可立即中断输出

### 本地命令执行（LocalExec）

Agent 通过 6 个工具实现完整的命令生命周期管理：

| 工具 | 功能 |
|------|------|
| `submit_command` | 提交命令（`process` + `args`），返回命令 ID |
| `start_command` | 根据 ID 启动已提交的命令 |
| `get_status` | 查询命令状态（pending / running / done / failed / killed）；不传 ID 时返回全部 |
| `get_output` | 获取 stdout / stderr 输出，支持 `window` 字节窗口与 `stream` 选择 |
| `intervene_command` | 向运行中的命令写入 stdin，或发送 `SIGINT` / `SIGTERM` / `SIGKILL`（Windows 仅 stdin + 强制结束） |
| `kill_command` | 强制终止命令 |

> Workflow：`submit → start → 轮询 get_status / get_output → 必要时 intervene → 必要时 kill`。

### 自动会话摘要（Session Summarizer）

- **触发条件（任一满足即触发，OR 关系）**：
  - `EventThreshold = 20`：自上次摘要后新增事件数达到阈值
  - `CheckTokenThreshold = 150_000`：自上次摘要后新增 token 数达到阈值
  - `CheckTimeThreshold = 10 * time.Minute`：本次会话超过 10 分钟无活动
- **token 计数**：使用 `trpc-agent-go/model/tiktoken` 替换默认计数器，按当前模型规则精准计 token。
- **摘要内容**：强制双段输出
  - `<analysis>...</analysis>`：模型自身的取舍说明（≤150 词）
  - `<summary>...</summary>`：9 段结构化模板（Primary Request / Key Concepts / Files & Code / Errors & Fixes / Problem Solving / User Messages / Pending Tasks / Current Work / Optional Next Step）
- **紧凑输入**：通过 `WithToolCallFormatter` / `WithToolResultFormatter` 把工具调用与结果以 `[Tool: name, Args: ...]` 的紧凑形式喂给摘要模型，并对超长片段截断（args > 100 字符 / result > 300 字符）。
- **TUI 实时回显**：`PostSummaryHook` 把生成的摘要以绿色文本输出到对话区，方便用户随时看到当前压缩结果。
- **异步执行**：`AsyncSummaryNum=2`、`SummaryQueueSize=100`、`SummaryJobTimeout=60s`，摘要在后台并发计算，不阻塞主对话。
- **下一轮注入**：通过 `llmagent.WithAddSessionSummary(true)`，框架在下一轮自动把最近一次摘要拼接进上下文，避免重复读取完整历史。

## 配置说明

首次启动时会自动在可执行文件同目录下创建：

```
<CWD>/
├── .hyperbot/
│   ├── hyperbot.yaml        # 主配置
│   ├── skills/              # 技能仓库（Knowledge-Only）
│   ├── hyperbot.log         # 框架日志
│   └── OperationRecord.md   # （任务运行后由 Agent 追加）
└── output/                  # 默认产物输出目录
```

默认 `hyperbot.yaml` 模板：

```yaml
# 用户配置
user:
  userid: "<auto-generated-uuid>"   # 首次运行自动生成

# 模型配置
model:
  model: "deepseek-reasoner"
  baseurl: "https://api.deepseek.com"
  apikey: "your-api-key"
  apitype: "openai"          # openai 或 anthropic
  contextwindow: 64000       # 上下文窗口大小，影响自动摘要触发时机
  stream: true               # 是否启用流式输出（实时查看推理过程与工具调用）

# MCP 服务（HTTP 类）：可配置任意多个
# type 可选: "sse" 或 "streamable_http"
mcp:
  - name: "mcpexec"
    enabled: true
    type: "sse"
    endpoint: "http://127.0.0.1:8080/mcp"
    headers: {}              # 可选：如 {"Authorization": "Bearer xxx"}
  # - name: "another-mcp"
  #   enabled: true
  #   type: "streamable_http"
  #   endpoint: "http://127.0.0.1:8081/mcp"
  #   headers: {}

# 标准输入 MCP（子进程类）：用 command + args 拉起本地 MCP server
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

> ⚠️ 1.3.x 起，原先的 `bochamcp` / `mcpexec` / `chromemcp` 三个独立配置块已合并为统一的 `mcp:` 列表 + `stdin_mcp:` 列表，旧版配置需迁移。

### 支持的模型

| APIType | 模型示例 | 说明 |
|---------|----------|------|
| `openai` | `deepseek-reasoner`, `deepseek-chat`, `gpt-4o`, `gpt-4o-mini` | OpenAI 兼容接口；DeepSeek 模型自动启用变体适配 |
| `anthropic` | `claude-3-5-sonnet-20241022`, `claude-3-opus` | Anthropic 原生接口 |

## 快速开始

### 环境要求

- **Go 1.26+**
- API Key（OpenAI 兼容 / Anthropic）

### 1. 克隆与运行

```bash
git clone https://github.com/lfz97/HyperBot.git
cd HyperBot

# 直接运行
go run .

# 或编译后运行
go build .
./HyperBot         # Linux/macOS
./HyperBot.exe     # Windows
```

### 2. 首次运行

首次启动会自动在可执行文件同目录下生成 `.hyperbot/hyperbot.yaml` 与 `.hyperbot/skills/`，修改配置后重新运行即可。

### 3. 交互命令

| 输入 | 功能 |
|------|------|
| 文本 + `Ctrl+Enter` | 发送消息（Enter 用于换行） |
| `/new` | 开始新对话（重置 Session） |
| `/flush` | 热重载配置：保留当前 Session，仅重建工具集（适合修改 `mcp` / `stdin_mcp` 后立即生效） |
| `/exit` | 退出程序 |
| `ESC` | 中断当前 Agent 响应 |

> 长文本可以直接粘贴：TUI 已启用 bracketed paste，不会再像旧版那样逐字符插入导致 CPU 飙升 / 界面卡死。

### 4. 交叉编译

```bash
make all          # 编译全部 5 个平台

make linux-x64
make linux-arm64
make macos-x64
make macos-arm64
make windows-x64

make clean        # 清理 release/
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

HyperBot 使用 **Knowledge-Only** 模式加载技能：技能只向 Agent 注入知识上下文，不再为每个技能单独生成执行工具，所有命令统一通过 `localexec` 执行，避免重复造轮子。

技能目录位于 `.hyperbot/skills/`，由框架的 `skill.NewFSRepository()` 自动发现和加载。

### 创建自定义技能

```
.hyperbot/skills/
└── my-skill/
    ├── SKILL.md           # 技能定义（必须）
    └── references/        # 参考文档（可选）
        └── guide.md
```

## SystemPrompt 与执行协议

HyperBot 内置一套 **Traffic Light + Execution Engine** 协议（见 `bootstrap/prompt/systemprompt.md`），把任务划分为：

- **🟢 Simple Interaction**：单工具 / 单命令 / 纯问答 → 直接执行，跳过计划与日志
- **🔴 Complex Task**：存在数据依赖链、多文件重构或系统级配置 → 必须先给出计划并征得用户同意，再走 Execution Engine 工作流（含进度记录、关键步骤回写 `OperationRecord.md` 等）
- **🛑 Anti-Overengineering 红线**：禁止人为拆分简单任务、禁止增加未经请求的前置检查、严格"听从优于优化"

启动时框架会用运行时信息填充以下占位符：`{{NAME}}` `{{DATE}}` `{{TIMEZONE}}` `{{OSTYPE}}` `{{AARCH}}` `{{HOME}}` `{{TMPDIR}}` `{{CURRENTUSER}}` `{{HOSTNAME}}` `{{CWD}}` `{{CONFIGPATH}}` `{{HyperBotConfig}}` `{{SkillsFolder}}` `{{HyperBotLogFile}}` `{{OperationRecord}}` `{{OUTPUTDIR}}`。

## 日志与产物

| 路径 | 用途 |
|------|------|
| `.hyperbot/hyperbot.log` | trpc-agent-go 框架日志（重定向自 stdout，避免干扰 TUI） |
| `.hyperbot/OperationRecord.md` | Agent 在复杂任务中追加的 Markdown 操作记录 |
| `output/` | 任务最终产物默认输出目录 |

## 依赖

| 依赖 | 用途 |
|------|------|
| [trpc-agent-go](https://trpc.group/trpc-go/trpc-agent-go) v1.8.1 | Agent 框架核心（Runner、LLMAgent、Tool、Skill、Session） |
| [trpc-agent-go/model/anthropic](https://trpc.group/trpc-go/trpc-agent-go) v1.8.0 | Anthropic 模型适配 |
| [trpc-agent-go/model/tiktoken](https://trpc.group/trpc-go/trpc-agent-go) v1.8.0 | tiktoken 精准 token 计数（驱动自动摘要触发） |
| [trpc-mcp-go](https://trpc.group/trpc-go/trpc-mcp-go) v0.0.10 | MCP 工具协议支持 |
| [tview](https://github.com/rivo/tview) v0.42.0 | TUI 终端界面框架 |
| [tcell/v2](https://github.com/gdamore/tcell) v2.13.9 | 终端底层事件处理（含 bracketed paste） |
| [openai-go](https://github.com/openai/openai-go) v1.12.0 | OpenAI API SDK |
| [anthropic-sdk-go](https://github.com/anthropics/anthropic-sdk-go) v1.19.0 | Anthropic API SDK |
| [google/uuid](https://github.com/google/uuid) v1.6.0 | Session / Request ID 生成 |
| [yaml.v2](https://gopkg.in/yaml.v2) v2.4.0 | YAML 配置解析 |
| [zap](https://go.uber.org/zap) v1.27.1 | 日志框架 |

## 许可证

MIT License
