# HyperBot

一个运行在终端里的 AI Agent 助手：支持 OpenAI / Anthropic 多模型、MCP 工具接入、本地命令执行与长期记忆，单文件部署、开箱即用。

## 核心特性

- **多模型**：兼容 OpenAI 接口（GPT、DeepSeek 等）与 Anthropic Claude，改一行配置即可切换，DeepSeek 自动适配
- **MCP 工具接入**：支持 SSE / Streamable HTTP / stdin 三种传输方式，通过 YAML 列表配置任意数量的工具集
- **本地命令执行**：内置命令生命周期工具（提交 → 状态 → 输出 → 干预 → 终止），基于 PTY 支持 `ssh`、`sudo` 等交互式命令，Agent 可自主完成"写代码 → 编译 → 运行 → 调试"闭环
- **上下文管理**：会话自动摘要 + 工具结果压缩 + 按需检索，长对话不会撑爆模型上下文窗口
- **长期记忆**：每轮对话自动提取关键信息存入 SQLite，下次对话自动检索注入
- **技能系统**：在 `.hyperbot/skills/` 下添加 `SKILL.md` 即可注入领域知识
- **实时流式输出**：推理过程与工具调用实时可见；思考/工具块默认折叠为单行摘要，点击块头部展开全文
- **可观测性**：可选接入 Langfuse，上报模型/工具调用 trace，并把上下文压缩的 summary/rebuild 阶段作为独立 span 呈现

## 快速开始

**环境要求**：Linux / macOS（Windows 请用 [WSL](https://learn.microsoft.com/zh-cn/windows/wsl/)，原生 Windows 不支持 PTY）、Go 1.26+（仅编译需要）、任意 OpenAI 兼容或 Anthropic API Key。

```bash
git clone https://github.com/lfz97/HyperBot.git
cd HyperBot
go build -o HyperBot ./cmd
./HyperBot
```

首次运行自动生成 `.hyperbot/hyperbot.yaml`，填入 API Key 后重启即可。

**常用操作**：

| 操作 | 功能 |
|------|------|
| Enter | 发送消息（Shift+Enter 换行） |
| ESC | 中断当前响应 |
| `/` | 命令列表（继续输入即前缀过滤；Enter 补全选中项，再按 Enter 执行） |
| `/new` | 开始新对话并清屏（会话上下文与屏幕记录一并重置） |
| `/clear` | 清屏（只清屏幕记录，会话上下文保留） |
| `/resume` | 恢复历史会话（弹框选择） |
| 鼠标左键点击 ▸ 头部 | 展开/收起思考块与工具块（默认折叠） |
| `/exit` | 退出 |

## 部署

单文件部署：一个二进制 + 一个 `.hyperbot/` 配置目录就是完整运行实例，无需 Docker、数据库或任何运行时依赖，对所在目录仅要求可读写。

```bash
# 放到任意目录直接运行，首次运行自动生成配置目录
./HyperBot
```

迁移只需拷贝两样东西，所有数据（配置、长期记忆、技能）完整保留：

```bash
scp HyperBot user@host:/opt/hyperbot/
scp -r .hyperbot user@host:/opt/hyperbot/
```

## 配置

`.hyperbot/hyperbot.yaml` 首次运行自动生成，核心字段：

```yaml
model:
  model: "deepseek-reasoner"      # 模型名
  baseurl: "https://api.deepseek.com"
  apikey: "your-api-key"
  apitype: "openai"               # openai 或 anthropic
  contextwindow: 64000            # 上下文窗口，须 ≤ 模型实际限制
  anthropicAuthHeaderTransfer: false  # true=Authorization Bearer 认证，false=X-Api-Key 认证
  stream: true                    # 流式输出
  maxtokens: 12800                # 每次请求的最大生成 token 数，默认 12800
```

MCP 服务通过 `mcp`（HTTP）和 `stdin_mcp`（子进程）列表配置。其余选项见自动生成的配置文件内注释。

## 可观测性（可选）

默认关闭，配置 `.hyperbot/hyperbot.yaml` 的 `observability` 段即可开启（改动需重启进程生效）：

```yaml
observability:
  service: "langfuse"
  host: "cloud.langfuse.com:443"   # 自建实例用 host:port，并设 insecure: true
  public: ""                       # 留空则读 LANGFUSE_PUBLIC_KEY
  secret: ""                       # 留空则读 LANGFUSE_SECRET_KEY
  insecure: false
  enabled: true
```

开启后每次模型调用与工具调用都会作为 trace 上报。上下文压缩触发时，trace 中额外出现两个阶段 span：

- `llmflow.context_compaction.create_summary` — 摘要生成
- `llmflow.context_compaction.rebuild` — 压缩后请求重建

压缩的最终结果（`success` / `no_update` / `summary_error` / `rebuild_unavailable` / `persistence_error` / `post_count_error`）以结构化日志写入 `hyperbot.log`，可直接检索：

```bash
grep "Pre-LLM context compaction result" hyperbot.log
```

> 启用后对话内容会上报 Langfuse 服务器；敏感对话建议只使用自部署实例。

## 许可证

MIT
