# HyperBot

一个运行在终端里的 AI Agent 助手：支持 OpenAI / Anthropic 多模型、MCP 工具接入、本地命令执行与长期记忆，单文件部署、开箱即用。

## 核心特性

- **多模型**：兼容 OpenAI 接口（GPT、DeepSeek 等）与 Anthropic Claude，改一行配置即可切换，DeepSeek 自动适配
- **MCP 工具接入**：支持 SSE / Streamable HTTP / stdin 三种传输方式，通过 YAML 列表配置任意数量的工具集
- **本地命令执行**：内置命令生命周期工具（提交 → 状态 → 输出 → 干预 → 终止），基于 PTY 支持 `ssh`、`sudo` 等交互式命令，Agent 可自主完成"写代码 → 编译 → 运行 → 调试"闭环
- **上下文管理**：会话自动摘要 + 工具结果压缩 + 按需检索，长对话不会撑爆模型上下文窗口
- **长期记忆**：每轮对话自动提取关键信息存入 SQLite，下次对话自动检索注入
- **技能系统**：在 `.hyperbot/skills/` 下添加 `SKILL.md` 即可注入领域知识
- **实时流式输出**：推理过程与工具调用实时可见

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
| Ctrl+K | 帮助页 |
| `/new` | 开始新对话 |
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
  maxtokens: 32000                # 每次请求的最大生成 token 数，默认 32000
```

MCP 服务通过 `mcp`（HTTP）和 `stdin_mcp`（子进程）列表配置。其余选项见自动生成的配置文件内注释。

## 许可证

MIT
