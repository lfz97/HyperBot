# TUI 分离设计

## 目标

将 TUI 渲染逻辑从业务层（handler/、session/、bootstrap/）中抽离，通过 `bridge/` 包统一管理事件生产和消费，使：

- 业务层不再 import tview/tcell
- 所有渲染相关代码（EventBus、RenderEvent、WidgetOps、tview 实现）集中在一个 `bridge/` 包
- 为后续 CLI 模式换输出方式预留可能性

## 不在本次范围

- 废除 global/ 包的其他全局变量
- 引入完整的依赖注入框架
- 为所有包写测试
- 支持 CLI / Web 模式（只是不阻塞这个方向）

---

## 新增：bridge/ 包

统一负责渲染事件的生产和消费。四个文件：

```
bridge/
├── renderEvent.go   // Target 常量 + RenderEvent 结构体（无 tview 依赖）
├── eventBus.go      // EventBus：channel + Emit + Events + Shutdown（无 tview 依赖）
├── widgetOps.go     // WidgetOps 接口定义（无 tview 依赖）
└── tuiRender.go     // TUI 渲染 goroutine + WidgetOps 的 tview 实现（唯一 import tview 的文件）
```

业务层只 import 一个 `HyperBot/bridge`，调用 `bridge.Emit(...)` 和 `bridge.WidgetOps.XXX()`。

### bridge/renderEvent.go

```go
package bridge

type Target int

const (
    TargetMessage Target = 1 // AgentMessageView
    TargetLog     Target = 2 // LogView
    TargetStatus  Target = 3 // StatusBar
)

type RenderEvent struct {
    Target  Target
    Content string // 已由 pretty/ 格式化好的字符串，渲染层只管 append
}
```

### bridge/eventBus.go

```go
package bridge

var defaultBus *EventBus

func SetDefault(eb *EventBus) { defaultBus = eb }

// Emit 向默认 EventBus 非阻塞发送事件，供业务层直接调用
func Emit(evt RenderEvent) {
    if defaultBus != nil {
        select {
        case defaultBus.ch <- evt:
        default: // channel 满时丢弃，不阻塞 agent 推理
        }
    }
}

func Shutdown() {
    if defaultBus != nil {
        close(defaultBus.ch)
    }
}

type EventBus struct {
    ch chan RenderEvent
}

func NewEventBus(bufSize int) *EventBus {
    return &EventBus{ch: make(chan RenderEvent, bufSize)}
}

// Events 返回只读 channel，供渲染层 range 消费
func (eb *EventBus) Events() <-chan RenderEvent { return eb.ch }
```

- `SetDefault` 在初始化时调用一次，注册全局 EventBus
- 业务层只需 `bridge.Emit(bridge.RenderEvent{...})` 一行
- `Emit` 是包级函数，直接操作 `defaultBus`，无循环依赖

### bridge/widgetOps.go

```go
package bridge

var WidgetOps WidgetOpsImpl

type WidgetOpsImpl interface {
    EnableInput(prompt string, onSubmit func(text string))
    DisableInput()
    RegisterGlobalEscape(onEsc func())
    UnregisterGlobalEscape()
    StopApp()
}
```

不暴露任何 tview 类型，`onSubmit` 是纯 Go 回调。业务层直接 `bridge.WidgetOps.EnableInput(...)`。

### bridge/tuiRender.go

整个项目中唯一 import tview/tcell 的文件，包含两部分：

**1. TUI 渲染 goroutine：**

```go
func StartRenderer(bus *EventBus, app *tview.Application,
    msgView *tview.TextView, logView *tview.TextView, statusBar *tview.TextView) {
    go func() {
        for evt := range bus.Events() {
            switch evt.Target {
            case TargetMessage:
                app.QueueUpdateDraw(func() {
                    fmt.Fprint(msgView, evt.Content)
                    msgView.ScrollToEnd()
                })
            case TargetLog:
                app.QueueUpdateDraw(func() {
                    fmt.Fprint(logView, evt.Content)
                })
            case TargetStatus:
                app.QueueUpdateDraw(func() {
                    statusBar.Clear()
                    fmt.Fprint(statusBar, evt.Content)
                })
            }
        }
    }()
}
```

**2. WidgetOps 的 tview 实现：**

```go
func InitWidgetOps(app *tview.Application, inputArea *tview.TextArea, sidebar *tview.TextView) {
    WidgetOps = &tviewWidgetOps{app, inputArea, sidebar}
}

type tviewWidgetOps struct { ... }

// 各方法内部使用 app.QueueUpdateDraw 驱动 tview
```

---

## 修改：global/appCore.go

`global/` 不再持有 EventBus/WidgetOps 的引用——这两个由 `bridge/` 包自己管理。

---

## 修改：业务层

### handler/message.go

| 当前 | 重构后 |
|------|--------|
| `global.Print2AgentMessageView(pretty.TReasoningStart())` | `bridge.Emit(bridge.RenderEvent{Target: bridge.TargetMessage, Content: pretty.TReasoningStart()})` |
| `global.Print2AgentMessageView(Choice.Delta.Content)` | `bridge.Emit(bridge.RenderEvent{Target: bridge.TargetMessage, Content: Choice.Delta.Content})` |
| `global.Print2AgentMessageView(pretty.TToolCall(name) + pretty.TToolArgs(args))` | `bridge.Emit(bridge.RenderEvent{Target: bridge.TargetMessage, Content: pretty.TToolCall(name) + pretty.TToolArgs(args)})` |
| `global.Print2AgentMessageView(pretty.TToolResult(content))` | `bridge.Emit(bridge.RenderEvent{Target: bridge.TargetMessage, Content: pretty.TToolResult(content)})` |

`gatherContentMessage()` 不涉及渲染，不改动。

### handler/runOnce.go

错误提示、状态栏提示改为 `bridge.Emit`。删除对 `global.Print2AgentMessageView` 的调用。

### handler/runIteratively.go

- 提示语输出 → `bridge.Emit`
- `SetInputCapture` / `SetDisabled` / `SetFocus` → `bridge.WidgetOps.EnableInput` / `DisableInput`
- `App_p.SetInputCapture` (ESC) → `bridge.WidgetOps.RegisterGlobalEscape`

### session/summarizer.go

```go
// 重构前
fmt.Fprint(global.AgentMessageView_p, pretty.TColoredText(...))

// 重构后
bridge.Emit(bridge.RenderEvent{Target: bridge.TargetMessage, Content: pretty.TColoredText(pretty.TColorGreen, fmt.Sprintf("已生成摘要：%v", cleanSummary))})
```

### bootstrap/Initializer.go

- Init() 中新增：`bus := bridge.NewEventBus(256)`、`bridge.SetDefault(bus)`、`bridge.InitWidgetOps(...)`、`bridge.StartRenderer(bus, ...)`
- `ShowError`/`ShowSuccess`/`ShowSuccessAndExit`：`Print2LogView` → `bridge.Emit(TargetLog, ...)`，`App_p.Stop` → `bridge.WidgetOps.StopApp()`

### bootstrap/Bootstrap.go

- 退出分支：`global.App_p.Stop()` → `bridge.WidgetOps.StopApp()`
- 消息输出：`fmt.Fprint(global.AgentMessageView_p, ...)` → `bridge.Emit(...)`

---

## 改动清单

| 文件 | 操作 | 估计行数 |
|------|------|----------|
| `bridge/renderEvent.go` | 新建 | ~15 |
| `bridge/eventBus.go` | 新建 | ~35 |
| `bridge/widgetOps.go` | 新建 | ~10 |
| `bridge/tuiRender.go` | 新建 | ~65 |
| `global/tui.go` | 删 Print2AgentMessageView 函数 | -10 |
| `handler/message.go` | Print2AgentMessageView → bridge.Emit | 改 ~15 处 |
| `handler/runOnce.go` | 同上 + 状态栏 | 改 ~5 处 |
| `handler/runIteratively.go` | widget操作 → WidgetOps + bridge.Emit | 改 ~15 处 |
| `session/summarizer.go` | fmt.Fprint → bridge.Emit | 改 ~3 处 |
| `bootstrap/Initializer.go` | 创建 EventBus/WidgetOps/StartRenderer，ShowError 改造 | 改 ~10 处 |
| `bootstrap/Bootstrap.go` | App.Stop → WidgetOps，消息 → bridge.Emit | 改 ~5 处 |

**总计：新建 bridge/ 包 ~125 行，修改 ~9 个文件约 70 处调用点。零 build circle。**

---

## 技术决策

1. **bridge/ 包统一生产和消费**：抽象（EventBus/RenderEvent/WidgetOps）和实现在同包不同文件，减少包数量，业务层一个 import 搞定
2. **Emit 非阻塞丢弃**：channel 满时丢弃而非阻塞，256 缓冲足够覆盖峰值，agent 推理不受渲染拖慢
3. **Content 由业务层预格式化**：`pretty/` 的格式化调用保留在业务层，渲染层只做 `fmt.Fprint`
4. **WidgetOps 只抽象行为**：不暴露 tview 类型，换 CLI 时只需换一个 WidgetOps 实现
5. **bridge 包自己管理全局状态**：`defaultBus` 和 `WidgetOps` 变量在 bridge 包内，通过 `SetDefault`/`InitWidgetOps` 注册，通过 `Emit`/`WidgetOps.XXX()` 调用，无循环依赖
