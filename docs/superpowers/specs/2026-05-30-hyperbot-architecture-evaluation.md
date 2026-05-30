# HyperBot 架构评估报告

从 Claude Code harness 架构视角对 HyperBot 的全面评估。2026-05-30。

---

## 1. 当前架构映射

### 1.1 模块依赖图

```
main.go
  └── tui/tui.go (TUI 初始化 + 页面创建)
        ├── bootstrap/Initializer.go (Init: 文件检查→配置→日志→工具→Agent→Runner)
        │     ├── config/baseConfig.go (Config/Model/User 结构体)
        │     ├── session/memSessionService.go → summarizer.go
        │     │     └── 依赖 tui/global_object (方向倒置)
        │     ├── agent/baseAgent.go (LLMAgent 配置组装)
        │     │     ├── models/openai.go / anthropic.go
        │     │     └── 加载 skills/ (FSRepository)
        │     ├── functionTools/ (File.go, FileSystem.go, Datetool.go)
        │     └── toolsets/ (mcp.go, stdinMCP.go, localexec/)
        ├── handler/
        │     ├── model.go (TurnCode, TurnResult, AgentRunner)
        │     ├── runIteratively.go (用户输入循环)
        │     │     └── 依赖 tui/global_object + tui/tip (方向倒置)
        │     ├── runOnce.go (单轮 agent 执行)
        │     │     └── 依赖 tui/global_object + tui/tip (方向倒置)
        │     └── message.go (流式/非流式渲染)
        │           └── 依赖 tui/global_object (方向倒置)
        ├── tui/global_object/ (共享 TUI widget 引用)
        └── utils/pretty/ (终端颜色)
```

**箭头方向 = 编译依赖**。标记"方向倒置"处，是业务逻辑层（session、handler）依赖了展示层（tui），违反了分层架构原则。

### 1.2 全局状态清单

`bootstrap/Initializer.go` 定义了 12 个包级变量：

| 变量 | 类型 | 用途 | 问题 |
|------|------|------|------|
| `Config_p` | `*config.Config` | 当前配置 | 全局可变，多 goroutine 访问无保护 |
| `Agentname` | `string` | Agent 名称 | 只写一次，可以不是全局 |
| `CWD` | `string` | 工作目录 | 只写一次 |
| `ConfigFolderPath` | `string` | 配置目录路径 | 只写一次 |
| `HyperBotConfigPath` | `string` | 配置文件路径 | 只写一次 |
| `SkillFolderPath` | `string` | 技能目录 | 只写一次 |
| `AgentRunner` | `handler.AgentRunner` | 全局 Runner | `/flush` 时被替换，无并发保护 |
| `InMemorySessionService` | `*inmemory.SessionService` | 会话服务 | `/flush` 时需保留 |
| `frameworkLogFile` | `*os.File` | 日志文件句柄 | 仅用于防止 GC |
| `PromptFiles` | `embed.FS` | 嵌入的提示词 | 只读，合理 |
| `systemprompt` | `string` | 系统提示词 | 只写一次 |
| `Toolsets` / `Tools` | `[]tool.ToolSet` / `[]tool.Tool` | 工具注册 | `/flush` 时被重建 |

**核心问题**：包级变量充当了"穷人的依赖注入"。任何一个变量被多个 goroutine 读写时就可能出问题。当前因为在 TUI 事件循环（单 goroutine）中操作所以没暴露，但如果未来加了 HTTP API 或后台任务就会直接踩坑。

### 1.3 数据流向

```
用户输入 (tview.InputArea)
  → handler.runIteratively (判断 /exit /new /flush /ESC /text)
    → handler.runOnce (Runner.Run → event stream)
      → handler.message (渲染到 tview.TextView)
      → session.summarizer (后台异步压缩)
```

错误恢复路径（跨越三层）：
```
runOnce 返回 AgentError
  → runIteratively 检查 Code==Error
    → 拼接 recover prompt ("之前的对话发生了错误: ...")
      → 下一轮 runOnce
```

---

## 2. 问题清单

### 🔴 严重问题

#### P1: 全局状态 + 上帝包

**现象**：`bootstrap/Initializer.go` 414 行，包含文件检查、配置加载、日志初始化、提示词构建、工具注册、Agent 创建、Runner 组装。所有这些通过 12 个包级 `var` 串联。

**根因**：没有依赖注入容器或应用结构体。每个需要共享状态的包直接 `import "HyperBot/bootstrap"` 然后访问全局变量。

**Harness 对比**：Claude Code 通过 `Config` struct + `Repository` 模式管理状态，子系统通过接口通信而非共享全局变量。

**影响**：
- 无法单元测试任何函数（都依赖全局状态）
- 新增功能必须理解整个 `Init()` 流程
- 无法创建第二个 Agent 实例（比如同时跑两个不同的模型）

#### P2: 依赖方向倒置

**现象**：4 个底层包直接 import TUI 层：

```
session/summarizer.go    → "HyperBot/tui/global_object"  (L84-86, PostSummaryHook)
handler/runOnce.go       → "HyperBot/tui/global_object"  (L53, L65-67)
handler/runOnce.go       → "HyperBot/tui/tip"            (L24, status bar)
handler/runIteratively.go → "HyperBot/tui/global_object" (L20-24, L34-37, L93)
handler/message.go        → "HyperBot/tui/global_object" (L15-70)
```

**根因**：渲染/通知逻辑写死在业务代码里，没有抽象 callback/observer 接口。

**Harness 对比**：Claude Code 的 tool execution、agent loop、session 层都不依赖 terminal/UI 层。UI 通过 listener 模式订阅事件。

**影响**：
- 无法换 UI 框架（比如换成 web 界面）
- session 和 handler 包无法独立编译测试
- 新人阅读代码要从 handler 跳到 tui 再跳回来，心智负担大

#### P3: `/flush` 机制与框架能力重叠

**现象**：`/flush` 命令手动调用 `AgentRunner.Runner.Close()` → `LoadConfig()` → `NewRunner()`，重建整个 Runner。但 `ConfigBaseAgent` 已经设置了 `WithRefreshToolSetsOnRun(true)`（`agent/baseAgent.go:20`）。

**根因**：框架的 `WithRefreshToolSetsOnRun` 理论上应该在每次 `Run()` 时自动刷新 MCP toolset。`/flush` 手动重建可能是因为某些场景下自动刷新不生效，或历史上是 workaround 后来框架修了但代码没跟进。

**影响**：
- 重复逻辑，维护两套刷新路径
- Runner 重建时会短暂不可用
- 新建 Runner 和旧的 SessionService 之间的协调依赖隐式约定（全局变量）

### 🟡 中等问题

#### P4: 工具注册无 Registry 模式

**现象**：添加工具需要四步操作（写 function → wrap → 加到 `Get*Tools()` → 确保 `loadFunctionTools()` 调用），任何一步遗漏都静默失效。CLAUDE.md 里明确写了"Missing registration means the tool silently won't appear"——说明已经踩过坑。

**根因**：工具注册是手动拼接 slice，没有自注册机制（`init()` + registry map）。

**建议**：见 3.3 节。

#### P5: 无 Hook/Interceptor 机制

**现象**：整个 agent 生命周期没有任何可介入的扩展点：
- tool call 前后无法插入逻辑（如审计、限流、统计）
- agent run 前后无法插入逻辑（如预处理 user prompt、后处理 agent response）
- 错误处理路径是硬编码的（拼 recover prompt）

**根因**：`handler/runOnce.go` 直接消费 event stream，没有中间件链。

**Harness 对比**：Claude Code 有 settings.json 中的 hooks 系统，支持 `PreToolUse`、`PostToolUse`、`PreMessage` 等多种事件。

**影响**：
- 想加功能（比如工具调用耗时统计）必须改 handler 核心代码
- 无法通过配置定制行为（比如某些 MCP tool 需要额外鉴权）

#### P6: 会话无持久化

**现象**：`InMemorySessionService` 纯内存。进程重启 = 丢失所有对话历史和摘要。

**根因**：trpc-agent-go 目前只提供了 `inmemory` 实现（可能有其他实现但项目没用）。

**影响**：
- 用户重启程序后"失忆"
- 长对话总结的摘要无法跨 session 复用
- OperationRecord.md 是唯一跨会话的"记忆"，但它只是 Agent 手动追加的文本

#### P7: 配置无热重载

**现象**：配置只在启动和 `/flush` 时加载。修改 `hyperbot.yaml` 后必须手动 `/flush`。

**建议**：可以用 `fsnotify` 监听配置文件变化，自动触发重载。

#### P8: Error 恢复路径脆弱

**现象**：错误恢复跨越三层函数调用：

```
runOnce → 返回 AgentError{OutputPart, Error}
  → runIteratively → 检查 Code==Error
    → 拼 recover prompt: "之前的对话发生了错误，错误信息是: X, 之前的输出内容是: Y"
      → 下一轮 runOnce
```

**问题**：
- `OutputPart` 是 `gatherContentMessage` 收集的正文，但不包括 reasoning、tool call/result 信息。模型看到的不完整。
- recover prompt 是中文模板硬编码，对所有模型统一

### 🟢 优化建议

#### P9: 缺少结构化日志 / 可观测性

当前日志重定向到 `hyperbot.log`，但 handler/session 层用的是 `fmt.Fprint` 到 TUI widget，没有结构化日志。出现问题时排查依赖用户在 TUI 里看到的内容。

#### P10: 单 Agent 架构限制

当前 `AgentRunner` 全局唯一。无法同时跑多个 agent（比如一个做代码生成一个做代码审查），也无法启动 subagent。Harness 的 subagent 模式（Explorer、Plan、claude-code-guide）在 HyperBot 中无法实现。

#### P11: Skill 系统与 Harness Skill 系统不互通

HyperBot 的 Skill 是 `SkillToolProfileKnowledgeOnly`（仅注入知识），Claude Code 的 Skill 可以定义工具、hook 生命周期。两套 Skill 格式不兼容，意味着为一个系统写的技能无法在另一个中使用。

---

## 3. 改进建议矩阵

每条建议标注：**投入**（人天）、**收益**（高/中/低）、**风险**（高/中/低）、**前置依赖**。

| ID | 建议 | 投入 | 收益 | 风险 | 前置依赖 |
|----|------|------|------|------|----------|
| R1 | 引入 App Struct + 依赖注入 | 3-5d | 高 | 中 | 无 |
| R2 | 抽象 UI 接口层 | 2-3d | 高 | 低 | R1 |
| R3 | 工具 Registry 自注册 | 1d | 中 | 低 | 无 |
| R4 | Hook/Interceptor 机制 | 3-5d | 高 | 中 | R1 |
| R5 | 会话持久化 | 2-3d | 中 | 低 | 无 |
| R6 | 配置文件热重载 | 1d | 中 | 低 | 无 |
| R7 | 错误恢复重构 | 1-2d | 中 | 低 | R2 |
| R8 | 结构化日志 | 1d | 中 | 低 | 无 |
| R9 | 多 Agent 支持 | 5-7d | 中 | 高 | R1 |

### 3.1 R1: 引入 App Struct + 依赖注入

**现状**：12 个包级变量充当全局状态。

**方案**：创建 `app.App` struct 集中管理所有状态：

```go
type App struct {
    Config           *config.Config
    SessionService   *inmemory.SessionService
    AgentRunner      handler.AgentRunner
    ToolRegistry     *registry.Registry
    EventBus         *events.Bus       // 给 R2/R4 打基础
}
```

通过 `App.Init()` 初始化，各组件通过 `app` 参数接收依赖，不再 import 全局变量。

**为什么先做这个**：这是所有其他改进的基础。没有 App struct，R2（UI 接口）和 R4（Hook）都很难做。

### 3.2 R2: 抽象 UI 接口层

**现状**：handler、session 直接 print 到 `global_object.AgentMessageView_p`。

**方案**：定义事件接口，让底层包 emit 事件，UI 层 subscribe：

```go
// events/events.go
type EventType int
const (
    EventAgentMessage  EventType = iota  // agent 对话内容
    EventToolCall                        // 工具调用
    EventToolResult                      // 工具结果
    EventSummary                         // 摘要生成
    EventError                           // 错误
    EventStatusBar                       // 状态栏更新
)

type Bus struct {
    subscribers map[EventType][]chan Event
}
```

handler/session 发出事件 → UI 层消费事件。这样做的好处：
- handler/session 包不再 import tui
- 换 UI 框架只需替换 subscriber
- 可以加多个 subscriber（比如同时输出到文件和 TUI）

### 3.3 R3: 工具 Registry 自注册

**现状**：工具手动拼 slice。

**方案**：利用 Go 的 `init()` + 注册表模式：

```go
// functionTools/registry.go
var registry = map[string]tool.Tool{}

func Register(name string, t tool.Tool) {
    registry[name] = t
}

func All() []tool.Tool {
    tools := make([]tool.Tool, 0, len(registry))
    for _, t := range registry {
        tools = append(tools, t)
    }
    return tools
}
```

每个工具文件用 `init()` 自注册：

```go
// functionTools/File.go
func init() {
    Register("WriteFile", function.NewFunctionTool(WriteFile, ...))
}
```

CLAUDE.md 里那条"Missing registration means the tool silently won't appear"的警告就可以删掉了——添加新工具只需一个文件，不需要改任何注册代码。

### 3.4 R4: Hook/Interceptor 机制

**方案**：参考 Claude Code harness 的 hook 模型，定义中间件链：

```go
type ToolInterceptor interface {
    BeforeTool(ctx context.Context, name string, args map[string]any) (context.Context, error)
    AfterTool(ctx context.Context, name string, result string, err error)
}
```

初期实现 tool call 前后拦截即可，后续扩展到 agent run 前后。具体的 hook 行为可以通过配置文件声明（类似 settings.json 的 hooks），也可以用 Go 代码注册。

### 3.5 R5-R9 简要说明

- **R5 会话持久化**：将 `InMemorySessionService` 替换为文件或 SQLite 后端。trpc-agent-go 可能已支持，或可自行实现 `SessionService` 接口。
- **R6 热重载**：用 `fsnotify` 监听 `hyperbot.yaml`，变化时自动触发 `/flush` 逻辑。
- **R7 错误恢复**：将 recover prompt 模板化、可配置化。在 event 中携带更完整的上下文（而不只是 `OutputPart`）。
- **R8 结构化日志**：用 zap 替代 `fmt.Fprint` 输出到 TUI widget，日志和 UI 输出走两条通道。
- **R9 多 Agent**：在 R1 基础上，`AgentRunner` 从单个变成 map，支持同时运行多个 agent 实例。

---

## 4. 推荐路线图

如果决定动手，建议分三个阶段推进：

### Phase 1: 打基础（1 周）

```
R1 (App Struct + DI) → R2 (UI 接口抽象)
```

这两步完成后，代码结构会从：

```
bootstrap(全局变量) ← handler ← tui
session ← tui
```

变成：

```
app.App → handler(emit events)
        → session(emit events)
        → tui(subscribe events)
```

这是最大的架构收益——之后所有改动都基于这个干净的分层。

### Phase 2: 增量改进（1 周）

```
R3 (工具 Registry) + R6 (配置热重载) + R8 (结构化日志)
```

三个独立改进，可以并行做。都不依赖 R1/R2（但有了 R1/R2 后做起来更干净）。

### Phase 3: 高级特性（按需）

```
R4 (Hook) → R5 (持久化) → R7 (错误恢复) → R9 (多 Agent)
```

这些取决于实际需求。Hook 和持久化是最有实际价值的。

---

## 附录：与 Claude Code Harness 的架构对比

| 维度 | Claude Code Harness | HyperBot 当前 | 差距 |
|------|---------------------|---------------|------|
| 状态管理 | Config + Repository | 包级全局变量 | 大 |
| 分层 | 每层独立，接口通信 | 底层依赖上层 | 大 |
| 工具注册 | MCP + function tools 统一注册 | 手动拼接 slice | 中 |
| 扩展点 | Hook 系统（settings.json） | 无 | 大 |
| 配置 | 三层 (user/project/local) + 热重载 | 单文件 + 手动重载 | 中 |
| 会话 | 持久化 + Memory 系统 | 纯内存 | 中 |
| 技能 | 知识+工具+生命周期 | 仅知识注入 | 中 |
| Agent 模型 | 主 agent + 多 subagent | 单一 agent | 大 |
| 可观测性 | 结构化日志 + 事件流 | fmt.Fprint + 文件日志 | 中 |
