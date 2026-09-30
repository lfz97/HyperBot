# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build Commands

```bash
# Run directly（入口在 cmd/，根目录没有 main.go）
go run ./cmd

# Build script（必须在 cmd/ 目录运行，输出到 cmd/release/HyperBot）
cd cmd && ./build.sh

# Build manually
# 注意：go build ./cmd 会因输出名与 cmd/ 目录冲突失败，必须用 -o
go build -ldflags "-s -w" -buildvcs=false -o HyperBot ./cmd

# Cross-compile via Makefile
make linux-x64        # → release/linux-x64/HyperBot
```

**CGO required** — `service/engine/memory/sqlite` depends on `mattn/go-sqlite3`. Cross-compilation needs C cross-compilers per target; for now, build natively on each platform.

```bash
# Tidy dependencies after adding/removing imports
go mod tidy
```

## Architecture Overview

HyperBot is a TUI AI agent chatbot built on [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go), supporting OpenAI-compatible APIs and Anthropic Claude models with MCP tool protocol integration.

### Layer Structure

```
cmd/main.go → boot.Boot() → service/engine + service/tui
    ├── boot/boot.go         (启动编排: GetTuiService → goroutine{GetEngineService → AgentStart} → tui.Run())
    │
    ├── service/engine/      (业务核心: Engine struct 封装全部状态，通过 tuiService 接口注入 TUI)
    │   ├── engineCore.go    (Engine struct、GetEngineService 工厂、AgentStart 主循环、newSessionID、重试策略常量)
    │   ├── init.go          (preCheckLoad 初始化序列: config/skills/prompt/memory/工具集；newRunner 工厂)
    │   ├── engineRun.go     (agentRunIteratively 输入循环 /exit /new ESC；agentRunOnce 事件流消费；turnCode/turnResult)
    │   ├── messagerender/messageRender.go (renderStreamEvent/renderNonStreamEvent/renderToolMessages; toolMsgBuffer)
    │   ├── agent/           (base.go LLMAgent 装配，内含按 APIType 选模型的分支; status.go BeforeModel 状态栏注入 + Display 接口)
    │   ├── config/          (config.go Config 结构+LoadConfig; mcp.go SSE/streamable_http+stdin MCP 配置; template.go embed)
    │   ├── memory/          (sqlite.go SQLite 记忆服务工厂)
    │   ├── models/          (openai.go/anthropic.go 模型适配器，DeepSeek 变体自动检测)
    │   ├── prompt/          (systemprompt.md embedFS)
    │   ├── session/         (memSessionService.go 摘要会话; summarizer.go; prompt/ 摘要模板)
    │   └── tools/
    │       ├── functions/   (file.go, datetool.go, filesystem.go 给 LLM 的文件/日期/文件系统工具)
    │       └── toolsets/    (mcp.go SSE/streamable_http/stdin MCP; localexec/ 命令执行 5 工具)
    │
    ├── service/tui/         (go-tui 界面)
    │   ├── agentui.gsx      (根组件视图模板 + agentUI 结构体；tui generate 生成 agentui_gsx.go 一并入库)
    │   ├── ui.go            (agentUI 交互逻辑: KeyMap/鼠标/Watchers/滚动状态/指示器/通知栏/文本段组件)
    │   ├── tui.go           (Tui struct、TuiService 实现: staging+snapshot 访问器、MarkdownDelta/Done、Show*AndExit、Run)
    │   ├── style.go         (pretty.Span 颜色字符串 → go-tui 样式的边界映射 + richEl 富文本元素)
    │   └── tools.go         (build tag tools, 钉住 tui CLI 版本供 go generate)
    │
    └── utils/pretty/        (终端颜色 helpers)
```

### Key Design Patterns

**UI Layout**: `agentui.gsx` 单模板声明整棵树（flex-col 根 + 深底色 `pretty.TuiBg`），从上到下：消息区（可滚动 `grow`，含启动横幅行、`segText` 富文本段、`segMarkdown` 段）+ TodoBar（0 到 N 行动态，每任务一行）+ NoticeBar（1 行常驻、右对齐）+ InputRow（左 2 列运行指示器 + textarea）。帮助面板是原生 `<modal open={State[bool]}>`（backdrop/Esc 关闭/焦点圈定）。模板管视图、ui.go 管逻辑；`go generate ./service/tui` 重新生成（生成器版本由 tools.go 的 build tag 钉在 go.mod）。go-tui 渲染契约与踩坑记录见 `TUI_NOTES.md`（**先读它再动 service/tui**）。

**Startup Flow**: `main()` → `boot.Boot()`:
- `tui.GetTuiService()` — synchronously creates the go-tui App, root component, help items
- `go func() { engine.GetEngineService(...); engine.AgentStart() }()` — **必须在 goroutine 里执行**：init 序列（`preCheckLoad` → `newRunner`）里的错误路径会调用 `Show*InMsgViewAndExit`，它们阻塞在 `<-done` 且依赖 `app.Run()` 事件循环处理按键回调才退出。同步调用会在 `tui.Run()` 之前死锁（首启无配置、配置错误、sqlite 初始化失败时白屏卡死）
- `tui.Run()` — runs `app.Run()` on the main goroutine; `app.Stop()` (triggered by exit/error paths) causes clean return without deadlock

**Agent Creation Flow**: `GetEngineService()` runs `preCheckLoad()`: redirects framework logs to `hyperbot.log`, loads/creates `hyperbot.yaml` and `skills/` folder, replaces `{{OSTYPE}}`, `{{HOME}}` and other placeholders in system prompt (`{{DATE}}`/`{{CWD}}` removed — now provided by the status bar), inits memory/session services, calls `loadSkills()` (populates `helpItems` for HelpTable), then `newRunner()` registers an agent factory that builds the LLMAgent based on `APIType`. All init progress/error messages go to `AgentMessage`.

**Agent Status Bar**: （**这里的 "Status Bar" 指注入 LLM prompt 的 `[STATUS]` 文本行，与 TUI 界面无关；TUI 顶部曾有的装饰性状态栏已移除，两者不要混淆。**）`service/engine/agent/status.go` — `SetBeforeModelStatusCallback()` appends a system status line (TIMENOW/CWD/MEMORY) to the **tail** of every LLM request via `llmagent.WithModelCallbacks`, mounted as one of the `llmagent.Option`s assembled in `init.go` (`newRunner`), which is why it covers both APITypes. Constraints: ① append at tail, never prepend — the status changes every call, a head position breaks automatic prefix caching (measured: 95-99% hit at tail vs 0 at head); ② requires `openai.WithOptimizeForCache(false)` in `service/engine/models/openai.go` — otherwise the framework reorders trailing system messages to the front, defeating the cache; ③ `{{DATE}}`/`{{CWD}}` were removed from `systemprompt.md`/`init.go` since the status bar overlaps them. The status never enters session/history, so summaries and compaction are unaffected. **Todo injection**: the same callback also appends `functionTools.TodoStatusBar(ctx)` — it fetches the invocation via `agent.InvocationFromContext(ctx)` (BeforeModel ctx carries it, `llmflow.go` `withInvocationContextIfMissing`), reads the checklist with the framework's public `todo.GetTodos(inv.Session, inv.Branch)` (same key `temp:todos[:branch]` as todo_write), and renders a vertical summary with every element on its own line: a `[TODO]` header line, one `◐ in-progress` line, one `☐ pending` line per item, and a trailing `(N more · N done)` count line; pending capped at 8 items. **The exact same string is also pushed to the UI** via `Display.SetTodoText(...)` so `TodoBar` can show it — `todo.go` is untouched and there is only one renderer. `Display` is a 1-method interface declared inside the `agent` package, and `init.go` passes `(*e).tui` straight into `SetBeforeModelStatusCallback` at the assembly site (non-interactive callers pass `nil`, which still injects into the prompt but skips the UI push). **The display must be called even when the string is empty** — `todo_write` auto-clears the list when all items are completed, at which point `TodoStatusBar` returns `""`, and TodoBar relies on that empty push to `ResizeItem` its height back to 0. Empty when no checklist → nothing appended to the prompt. Checklist changes only affect the tail message, so prefix caching stays intact; takes effect on the very next LLM hop after todo_write runs, and survives compaction (which only trims history, not session state).

**Adding Function Tools**: create function → wrap with `function.NewFunctionTool()` → register in `Get*Tools()` (e.g. `GetFileSystemTools`) → assembled in `loadFunctionTools()` (`service/engine/init.go`). Missing registration means the tool silently won't appear.

**Framework Built-in Tool (todo_write)**: 框架自带工具（`tool/todo`），封装在 `service/engine/tools/functions/todo.go`：任务清单写进 session state（`temp:todos[:branch]`，按 invocation branch 分键隔离），每次调用整份替换；在 `loadBuiltinTools()`（`init.go`）并入 `builtinTools`；使用说明由 `{{TODO_PROMPT}}` 占位符注入 `prompt/systemprompt.md`，内容取 `todo.DefaultToolPrompt`（随框架版本走，不硬编码）。渲染字段在 `jsonmapper.go` 用常量 `todoWriteToolName` 注册，只显示 `message`（避免整份清单 JSON 刷屏）。**注意**：框架还有个 `await_user_reply`（`tool/awaitreply`）追问路由工具，**本项目未启用**——单 agent 场景下"下一条用户消息回到该 agent"本来就必然发生，路由是 no-op；它是为多 agent 系统里"子 agent 追问后直达"设计的，将来引入多 agent 再考虑。

**Dialog Loop**: `Engine.agentRunIteratively()` manages user input:
- `/exit` → terminate
- `/new` → reset session
- `ESC` → cancel current agent response via `context.WithCancel`
- text → invoke `agentRunOnce()`
- error → 退避 `errorSleepGap` 后自动重试，连续 `errorMaxTimes` 次后放弃并把控制权交还用户（`Code` 保持 `Error`，由 `agentRunIteratively` 的 `errorStreak < errorMaxTimes` 判定转入 `else` 兜底分支等输入）

- **`turnCode` 的六个值与 `Startup` 的作用** — `Startup=0`（程序启动，尚未有任何一轮对话）、`New=1`（用户敲 `/new`）、`Int=2`（ESC 中断）、`Error=3`、`Exit=4`、`Continue=5`。承载它的结构体是 `turnInfo`（曾名 `turnResult`）。`AgentStart` 的**初始** `MsgContext` 用 `Startup` 而不是 `New`：刚启动时不存在"上一轮对话"，推一条"新对话已开始"到 NoticeBar 是噪音，此时显示 `ctrl+k for help` 更有用；`New` 只留给用户真的敲 `/new` 的场合。`Startup` **永远不会被 `agentRunIteratively` 返回**，只作为初始值存在——这也让它成为**启动横幅的唯一挂载点**（`agentRunIteratively` 顶部 `if Code == Startup { ShowStartupBanner(...) }`），横幅因此必然只打印一次。刻意取 `0` 是为了让 `turnInfo` 的零值就是它——未显式设置 `Code` 时会落到最安静、最安全的默认行为（不推通知、不打横幅、走 `else` 兜底等用户输入）。这也正是"Error 优先 + else 兜底"那个结构的收益兑现：**新增一个 turnCode 不需要在任何条件里登记**，它会自动落到"等用户输入"这条安全路径上；若仍用旧的 `New || Continue || Int` 列举写法，新增 code 会两个分支都不进、导致循环体空转 100% CPU。

**Note**: `/flush` no longer exists — since commit c1d966e the runner reloads config/skills/tools automatically on every run (`reload()` in the agent factory). Session history is preserved because `reload()` never rebuilds the session service.

**Streaming Events**: `Engine.agentRunOnce()` calls `runner.Run()` and consumes an event stream. Messages render with:
- Reasoning content: yellow dim text (suppressible via `show_reasoning: false`)
- Tool calls/results: compact single-line via `TToolCompact` — green `●` dot + orange tool name + dim gray `args → result_summary`. Short results (≤60 chars, single-line) inline; long results show stats (`3 lines, 12.5KB`).

**LocalExec ToolSet**: Built-in 5-tool system for command lifecycle（框架加前缀后 LLM 看到的实际名是 `LocalExec_<tool>`）:
| Tool | Purpose |
|------|---------|
| `run` | Execute a command; blocks up to 20s — returns Status/ExitCode/Output inline if it finishes, otherwise switches to background and returns the command ID |
| `status` | Query status; `WaitSeconds` blocks until done or timeout (每秒轮询，完成即返回) |
| `output` | Get stdout or stderr of a command |
| `intervene` | Write to stdin or send signals |
| `kill` | Force terminate |

`run` 与 `output` 的输出体量不可控，超过 `inlineMaxBytes`（64KB，定义在 `localexec/tooloutput.go`）就整份落盘到 `<exeDir>/output/tool-outputs/`，只内联头部 2KB 预览，并在结果里附 `OutputFile`/`TotalBytes`/`Note`，引导 agent 用 `ReadFile`（靠 `NextOffset` 分页）或 `SearchInFile` 取用完整内容。落盘是「成功交付」而不是失败，所以走正常返回值、不走 error——否则调用方的 `if err != nil` 会把整个结果 map 连同文件路径一起丢掉。落盘文件名是 `<id>.<创建时间戳>.<seq>.output`（`20060102-150405` + 进程内自增序号）：时间戳供 GC 判新旧——落盘文件是 `deliver` 一次性写完的快照、写完不再改动，所以创建时间就是全部信息，不必 stat mtime；seq 兜住同一秒内的多次落盘，因为同一 job 会被反复取用（run 落一次、output 查 stdout/stderr 又各落一次），只用 id 命名会让后一次悄悄覆盖前一次。GC 对解析不出时间戳的名字一律跳过，宁可漏删不可误删。这套机制只有 localexec 用，所以是包内私有而非独立包；`functions/file.go` 只需要其中的 rune 边界裁剪，自带一份 `trimPartialRune`。

工具用法不写进 `systemprompt.md`——参数形状与跨工具引用都由 `tools.go` 的 `function.WithDescription` 和 jsonschema tag 承载（描述跟着工具走，cronagent 复用时也带得上）。prompt 的 `## 2. Command Execution` 只保留"该写什么命令"（OS-aware 选型），不讲"该调哪个工具"。

**MCP Integration**: Configured via `hyperbot.yaml` with support for `sse` and `streamable_http` transport types. Also supports stdin-based MCP via `stdin_mcp` config.

**Session Memory**: `InMemorySessionService` is stored in `(*e).SessionService_p` (Engine struct). `reload()` (called from the agent factory on every run) refreshes config/tools/skills without touching the session service, so conversation history always survives. If adding a new reload step, keep it before the factory builds the agent.

**Session Summarization**: `service/engine/session/summarizer.go` — token + time thresholds via `WithChecksAny`, plus `WithSkipRecent`, `WithToolResultFormatter`, `WithSyncSummaryIntraRun`, and `WithSessionSummaryInjectionMode(SessionSummaryInjectionUser)`. Requires `session.NewMemorySessionService` with summarizer AND `llmagent.WithAddSessionSummary(true)`. Key gotchas: `contextwindow` in hyperbot.yaml MUST match the actual API provider limit; `WithToolResultFormatter` truncates tool results before token estimation, affecting both summary input and threshold counting. See Context Management section for full details.

**Deployed Config**: User config lives in `<cwd>/.hyperbot/hyperbot.yaml` (the working directory where the binary runs). On the author's machine this is `C:\Users\<user>\OneDrive - ...\应用\hyperbot\.hyperbot\`, but it varies by platform. The repo's `.hyperbot/` is for development only.

## Configuration

Auto-generated to `hyperbot.yaml` on first run. Supports:
- User ID (auto-generated UUID)
- Model config (model name, base URL, API key, API type: `openai` or `anthropic`)
- `anthropicAuthHeaderTransfer` (bool): if `true`, uses `Authorization: Bearer <apikey>` header instead of default `X-Api-Key` — for proxies/gateways that require Bearer auth
- `contextwindow` (int): context window size in tokens, MUST be ≤ actual model limit
- `maxtokens` (int): max generation tokens per request, default 12800 (`config.Model.MaxTokens`, wired into `model.GenerationConfig.MaxTokens` via `WithGenerationConfig` in `service/engine/init.go`)
- `show_reasoning` (bool): display reasoning/thinking content
- `stream` (bool): enable streaming output
- MCP services (SSE or streamable_http)
- Stdin MCP processes

## Dependencies

- **glamour** v2.0.1 (`charm.land/glamour/v2`): Markdown → ANSI renderer (Dracula theme, OSC 8 hyperlinks)
- **trpc-agent-go** v1.11.1-0.20260820131707-cdaece75b478: Agent framework core（含 #2501 修复；注意：session/summary 摘要机制在 v1.11 有重构，改动前对比两版行为）
- **trpc-agent-go/model/anthropic** v1.11.1-0.20260820131707-cdaece75b478: Anthropic model adapter（独立子模块，须单独升级）
- **trpc-agent-go/model/tiktoken** v1.11.0: Tiktoken-based token counter (replaces SimpleTokenCounter default)
- **trpc-mcp-go** v0.0.18: MCP tool protocol support（独立于 agent-go 版本，可单独升降）
- **tview** v0.42.0: TUI framework
- **tcell/v2** v2.13.10: Terminal event handling
- **openai-go** v1.12.0: OpenAI API SDK
- **anthropic-sdk-go** v1.66.0: Anthropic API SDK
- **zap** v1.28.0: Logging
- **otiai10/copy** v1.14.1: Cross-device file/directory copy for CP and MV tools

## Notes

- Go 1.26.4+ required
- No test files exist in this repository
- Skills are loaded from `skills/` directory in Knowledge-Only mode (commands go through LocalExec)
- Framework logs are redirected to `hyperbot.log` to avoid TUI interference
- **HelpTable dynamic items** — Default slash commands in `helpItems` initialized via `defaultHelpItems()` (`service/tui/tui.go`). Skills appended via `loadSkills()` → `ResetHelpItems()` + `AddHelpItems()` (`service/engine/init.go`). `refreshhelpTable()` rebuilds Table cells and is called in `toggleHelpPage()` on every open (`service/tui/Internal.go`).
- **embedFS case sensitivity** - `//go:embed` + `ReadFile` paths are case-sensitive on Linux. File named `systemprompt.md` but code reading `systemPrompt.md` silently returns empty string (error ignored with `_`). Always match exact file name case between `go:embed` glob patterns and `ReadFile` calls.
- **`model/anthropic` 子模块版本对齐** — 根模块与 `model/anthropic` 子模块在 go.mod 中独立锁版本，升级根模块不会带上子模块修复。DeepSeek anthropic 端点要求 object schema 的 properties 非 null；无参 MCP 工具在旧版（v1.11.2 及更早）下序列化为 `"properties":null` 导致 400。升级时必须显式同时升级两者到含修复版本（#2501，commit cdaece75；现锁 `v1.11.1-0.20260820131707-cdaece75b478`）
- **Skills identity contamination** - The deployed `skills/` folder (`~/.hyperbot/skills/`) contains OpenClaw skill files (self-improving-agent, find-skills, etc.) that reference "OpenClaw", "Claude Code", "clawdhub". When loaded via `llmagent.WithSkills()`, these contaminate the agent's identity. Use only HyperBot-specific skills or keep the folder empty.
- **bracketed paste** - tview TextArea 对大块粘贴支持不好，需在 `service/tui/tui.go` 中启用 `EnableBracketedPaste()` 让终端分片发送，框架才能正常处理。参考 commit 70bb8f3。
- **Input multiplexing (inputChan)** - Tui 字段 `inputChan`（**unbuffered**；包外无引用，已改为未导出），`ListenUserInput() chan string` 把焦点交回输入区、注册 Enter 提交捕获（阻塞到 UI 线程应用完毕）并返回该 channel，引擎循环用 `select { case userPrompt = <-tui.ListenUserInput(): }` 读取（`service/engine/engineRun.go`）。三条约束：① 发送用 select-default——对端（引擎）未监听时不投递且**不清空输入框**（`SetText("")` 只在投递成功时执行），unbuffered send 永不阻塞 tview 事件循环、用户输入永不丢失；② **捕获常驻**——Enter 提交后不注销捕获（旧 `SetInputCapture(nil)` 已删除），agent 运行期间 Enter 被捕获消费（不换行不投递，Shift+Enter 仍可换行、Ctrl+K 帮助仍可用）；③ select 是扩展点——计划任务结果回传（schedule agent）将在 select 上加 TriggerCh 分支 + 前置 DrainPending 检查，勿改回阻塞式 `ListenUserInput() string`。**`ListenUserInput()` 写在 select 的 case 操作数里是安全的**：Go 规范保证各 case 的 channel 操作数在进入 select 时按源码顺序只求值一次，所以"先注册捕获、再阻塞接收"的顺序成立；但它带 SetFocus + SetInputCapture + 一次 QueueUpdateDraw 重绘，不是纯 getter。
- **Go RE2 regex in tool descriptions** - 在给 agent 暴露正则的 tool 中，jsonschema description 必须写明 RE2 限制：`(?s)` 让 `.` 匹配换行、`(?m)` 让 `^`/`$` 匹配行边界、不支持 lookahead/lookbehind/backreference。参考 `service/engine/tools/functions/file.go` 中 SearchInFile 的 description。
- **MV/CP tools use otiai10/copy** - `service/engine/tools/functions/filesystem.go` 的 CP 和 MV 跨设备移动使用 `github.com/otiai10/copy`（不区分文件/目录）。MV 同设备优先 `os.Rename`（快速+原子），跨设备走 `os.RemoveAll(dst)` → `copy.Copy` → `os.RemoveAll(src)`（先删目标避免合并语义）。
- **EditFile replace_all semantics** - `replace_all=false`（默认）是安全检查：多处匹配时报错拒绝替换，而非只替换第一处。`replace_all=true` 才执行全量替换。修改时不要移除 `len(Indexes) > 1` 的检查。
- **strings.Index loop pattern** - 在 `Now[offset:]` 上循环搜索时，`offset += idx` 定位到匹配起点后，必须再 `offset += len(matched)` 跳过已匹配内容，否则同一位置重复匹配导致死循环。
- **Tool call 后 agent 停止输出** - 如果 `hyperbot.log` 无 error，且对 agent 说"继续"能恢复对话，说明是模型侧在 tool result 后概率性预测了 stop token，不是框架 bug。不需要迁就式修改。
- **`models.Openai()` / `models.Anthropic()` are the canonical model constructors** — `service/engine/session/summarizer.go` and agent creation use these two functions. They handle DeepSeek variant detection, reasoning backfill, and API auth. When creating a new model instance from config, call these instead of manually assembling openai/anthropic options.
- **APIType dispatch duplicated 4×** — `service/engine/agent/base.go`、`service/engine/session/summarizer.go`、`service/engine/memory/sqlite.go`、`service/engine/models/*` 各有一份 `if APIType == "openai" ... else if "anthropic"` 分支。新增 provider 类型必须同步改这 4 处，否则 agent 会静默使用 nil summary/memory model。
- **markdown 正文渲染是原生组件** — `messageRender.go` 的正文（流式与非流式）经 `TuiService.MarkdownDelta` 直流进消息区的 markdown 段（go-tui 内置 Markdown 组件：代码高亮/表格/列表），`MarkdownDone` 定稿（TrimSpace）。前提是 Message.Content 与 Delta 拼接字节相同（两 provider 实测如此），所以无需回填；旧的"流原文 + ReplaceTail 尾部替换"两段式与 glamour 管线已整体删除。
- **Tool response content must be skipped in content rendering** — `NewToolMessage` stores tool result in `Content` field alongside `Role="tool"`. Both stream and non-stream content paths check `Role != "tool"` to prevent tool JSON from leaking through the main content renderer. Without this, tool results appear as raw JSON in body text while also being formatted via `TToolCompact`.
- **Multi-tool results handled in `engineRun.go`** — Framework merges parallel tool results into a single `tool.response` event with N Choices. `agentRunOnce` detects `ObjectTypeToolResponse` and iterates ALL Choices (not just `Choices[0]`), ensuring every result is rendered.
- **Glamour / tview 渲染管线已整体删除** — 正文 markdown 由 go-tui 内置组件渲染（见上条）；`glamourRenderer`、`TranslateANSI`、`[-:-:-]` 追加、per-width 渲染器缓存等约束全部失效，不要再照搬。
- **`show_reasoning` config** — `config.Model.ShowReasoning` (`yaml:"show_reasoning"`) controls whether reasoning/thinking content is displayed. Default `false`. Affects both stream (chunk-level skip) and non-stream (whole-block skip) paths. Set `show_reasoning: true` in `hyperbot.yaml` to enable.
- **`messageRender.go` refactored** — `printMessage` split into `renderStreamEvent`, `renderNonStreamEvent`, `renderToolCall`, `renderToolResult` (`service/engine/messageRender.go`). Tool call/result rendering uses shared `addToolCallMsg`/`addToolResultMsg` helpers with `toolMsgBuffer` mutex. Compact single-line format via `pretty.TToolCompact` — no trailing `\n` (double-newline with next tool's leading `\n` causes alignment shift).
- **Glamour default WordWrap is 80 columns** — without `WithWordWrap`, glamour wraps all markdown at 80 columns regardless of terminal width. Always pass the current view width when creating a renderer. See `glamourRenderer()` in `service/tui/tui.go` for the pattern (cached per width, width read on the UI goroutine).
- **Engine init must stay in a goroutine** — `boot.Boot()` 把 `GetEngineService`（内含 `preCheckLoad`）放在 goroutine 里跑，与 `tui.Run()` 并发。不要改回同步：`ShowErrorInMsgViewAndExit`/`ShowSuccessInMsgViewAndExit`/`ShowMsgAndExitNoTrigger` 都是 `showMsgAndExit(msg, waitForKey)`（`service/tui/tui.go`）的薄封装，末尾用 `select{}` **故意永久阻塞**，靠 `app.Stop()` → `tui.Run()` 返回 → `main()` 返回来收尾；事件循环未启动时会永久死锁（白屏卡死）。两个不要改的点：① 不要把 `select{}` 换成"回调 close(chan) 后返回"——调用方（init 序列）打完致命错误后并没有 `return`，helper 一返回引擎 goroutine 就会带着未初始化的状态（如 nil memory service）继续跑，最后在别处 panic；② 不要在按键回调里直接 `os.Exit`——`screen.Fini()` 是在 `app.Run()` 的返回路径上调的，硬退出会跳过它，终端会留在 alt-screen + raw mode。
- **`go build ./cmd` 会失败** — 输出文件名与 `cmd/` 目录冲突（"build output cmd already exists and is a directory"）。构建必须带 `-o`（`go build -o HyperBot ./cmd`）或使用 `cmd/build.sh`（需在 cmd/ 目录内运行）。
- **TUI 依赖接口按消费方切分** — `requirements.TuiService`（15 方法）只给真正需要全套的 `Engine` 用（`engineCore.go` / `init.go`）。其余消费方各自在本地声明小接口：`messagerender.tuiService`（3 方法：`PrintToMsgView` + `MarkdownDelta` + `MarkdownDone`）、`session.msgPrinter`（1 方法：`PrintToMsgView`）、`agent.Display`（1 方法：`SetTodoText`，`init.go` 在装配点把 `(*e).tui` 直接传给 `agent.SetBeforeModelStatusCallback`——刻意在 `agent` 包内声明，避免把胖接口渗进 `agent` 包；名字按能力而非当前唯一调用方命名，所以不叫 `TodoTextSink`，将来加别的展示项直接扩方法）。给 TUI 加/改方法时只需动 `requirements.TuiService` 和真正用到它的地方，session 这类无关包不会被牵连。
- **`agent.OpenaiAgent` / `agent.AnthropicAgent` 已删除** — 它们是零逻辑的纯透传包装（函数体只有一行 `ConfigBaseAgent(...)`），而 `ConfigBaseAgent` 内部本来就按 `m.APIType` 分支选模型，`init.go` 又在同一个字段上分支一次去选包装函数——**同一个判断做两遍，中间那层唯一的作用是被穿过去**。现在 `init.go` 直接调 `agent.ConfigBaseAgent`，`agent/` 目录只剩 `base.go` + `status.go`。`APIType` 校验改用排除法单独做一次且**必须保留**：`ConfigBaseAgent` 对未知类型是静默不设模型（agent 照样建得出来、跑起来才失败），那个 `if apiType != "openai" && apiType != "anthropic"` 是唯一能给出可读配置错误的地方。⚠️ 将来给 `ConfigBaseAgent` 加参数时**不要再引入 per-APIType 的包装层**——参数穿透的噪音来源是包装函数本身，不是参数。同理，新的 `llmagent.Option`（如状态栏回调 `agent.SetBeforeModelStatusCallback(display)`）**一律在 `init.go` 的 `opts` 列表里构造**，不要在 `ConfigBaseAgent` 内部追加：`ConfigBaseAgent` 只负责按 APIType 选模型，装配意图集中在调用点才看得全。
- **staging + MarkDirty 是线程契约，不是补丁** — `service/tui/tui.go`。引擎 goroutine 只写 `Tui` 的 staging 字段（全部走 `mu`，永不阻塞）+ `app.MarkDirty()`（atomic 标志，30fps 主循环消费合并成帧）；主循环的 Render 经带锁 snapshot 访问器读 staging 重建整棵树。**不要**在引擎侧调 `State.Set`/`QueueUpdate`（前者主循环专用，后者队列满会丢 token）。
- **输入区运行指示器** — `agentui.gsx` 输入行左 2 列的 span：空闲灰色 `> `（`pretty.TuiSubText`），agent 运行中显示淡紫色盲文 spinner（`pretty.TColorLightMagenta`，90ms/帧，由 `agentUI.tick` 这个 OnTimer watcher 驱动、每帧模板重渲）。引擎侧只通过 `SetAgentRunning(bool)` 写一个 `atomic.Bool`，不起 goroutine。
- **TodoBar 与 NoticeBar** — 模板内联区块：todo 每任务一行（`todoLineStyle` 按行首 `◐`/`☐` 上色）、空串整栏消失；NoticeBar 固定 1 行右对齐，`noticeSpan()` 取"临时通知(4s TTL) > 运行提示 > 空闲提示"。样式经 `spanStyle` 从 pretty.Span 映射。
- **pretty.Span 是新的展示契约，无需转义** — `PrintToMsgView([]pretty.Span)`、`ShowNotice(pretty.Span)`：颜色是结构化字段（字符串常量 + Bold/Dim），不再是文本里的标记语法，tview.Escape 时代的"受信 markup vs 纯文本必须反向处理"契约整体失效——LLM/框架文本里的 `[TODO]`、`[CN]` 就是字面文本，不会被吞。`style.go` 做颜色字符串→go-tui 样式的边界映射（命名色 + #hex，未知色回落主文本色）。
- **TodoBar 的 `◐`/`☐` 是"系统消息不带符号"规则的豁免** — 三条理由：① 它们承载 **per-item 状态语义**（进行中 vs 待办），不是装饰；② 各行的状态差别靠 `renderTodoLines` 按行首符号逐行上色（进行中青色、待办正文色、头行/计数行暗灰）——按前缀判色不需要 sink 推结构化数据，但前提就是符号必须存在，去掉符号颜色区分随之失效；③ `[TODO]` 前缀保留（它已被 `tview.Escape` 覆盖，且在专门的 bar 里起到"这栏是什么"的标识作用）。**不要拿"系统消息不带符号"那条规则来"修"它。**
- **滚动跟随是组件状态** — `agentUI.follow/scrollY`：跟随模式向滚动容器写"足够大"的偏移（`bigOffset`，布局时钳到底部，等价 tview trackEnd），PgUp/滚轮上翻退出跟随、滚回底部恢复；退出态强制贴底（退出消息必须可见）。go-tui 的可滚动容器不会自己滚，键盘/滚轮事件也由 `KeyMap`/`HandleMouse` 处理——不要指望原生行为。
- **Startup Banner** — Engine 的 `startupInfoLines()`（`engineCore.go`）决定显示**什么**（label 补齐到 13 列、`$HOME`→`~`、BaseURL 只取 host、session id 截前 8 位、`SkillRepo` 判空——`loadSkills` 里 `NewFSRepository` 的错误被 `_` 忽略了，不判空会启动即 panic），TUI 的 `ShowStartupBanner` 把信息行物化在消息区顶部（次级色、随对话滚动）。**挂载在 `Startup` turnCode 上**，而 `Startup` 永远不会被 `agentRunIteratively` 返回，所以横幅必然只打印一次。**它是"死文本"**：写入后随对话滚动、resize 不重排——这是刻意的语义（启动横幅本来就是一次性历史记录）。tview 时代的 `banner.go` 三列拼版（logo 渐变/面板）已随 go-tui 迁移删除。
- **滚动跟随语义**（tview trackEnd 的 go-tui 等价物）见上面"滚动跟随是组件状态"条；`PrintToMsgView` 不做任何滚动操作，跟随由 `agentUI.follow` 状态 + `offsetY()` 每帧写入大偏移实现。
- **`ShowSuccessInMsgView` 已删除** — 迁去 NoticeBar 后成为死方法，已从 `Tui` 与 `requirements.TuiService` 一并移除。⚠️ 名字相近的 **`ShowSuccessInMsgViewAndExit` 保留**，它有独立调用方（首启创建默认配置文件后要求用户改配置再重启）。`pretty.TNewConversation`/`TCancelled`（消息区版）与 `TColorSkyBlue`、未用的 `TBg` 色板预设也已随 tview 标签清理删除（均无调用方）；bar 版保留且文案统一小写英文（与 NoticeBar 常驻 hint 风格一致）。
- **`SetAgentRunning` 的调用位置** — `service/engine/engineRun.go` 的 `agentRunIteratively` 里，紧贴 `agentRunOnce` 调用：`SetAgentRunning(true)` + `defer SetAgentRunning(false)`。两个不要改的点：① 用 `defer` 而不是调用后直接复位——`agentRunOnce` 跑的是框架代码，panic 时指示器会永远转；② **不要挂到 `agentRunIteratively` 开头那个 `Ctx, cancel := context.WithCancel(Ctx)` 上**——那个 ctx 是给 ESC 中断用的，生命周期包含前面等用户输入的阶段，挂上去 spinner 会在用户还没打字时就转起来。指示器逻辑刻意不放在 `agentRunOnce` 内部：UI 表现是调用方的关注点，且 "Input multiplexing" 条里计划的 schedule agent 可能非交互地调用 `agentRunOnce`。
- **滚动跟随的 go-tui 实现** — `PrintToMsgView`/`MarkdownDelta` 不做任何滚动操作；跟随/上翻/贴底全部由 `agentUI` 的滚动状态条目实现（`offsetY` 每帧写入偏移）。tview 时代 `ScrollToEnd`/`trackEnd` 的整条机制已不存在。
- **连续错误重试策略** — `service/engine/engineCore.go` 的 `AgentStart` + `service/engine/engineRun.go` 的 `agentRunIteratively`。改前 `Code == Error` 会直接构造一句"之前的对话发生了错误……请继续完成对话"再跑一轮，**无等待、无次数上限**：API key 失效/断网/额度耗尽这类持久性错误会导致无限热循环，每轮都发真实 API 请求、都打一条错误、都新建 `context.WithCancel`、都重注册 ESC 捕获。现在：两个编译期常量 `errorMaxTimes = 3` / `errorSleepGap = 3 * time.Second`（刻意不做成 yaml 配置，也没做成 Engine 字段——它们是常量，做成字段只是多一层间接且无人会赋别的值）；唯一的运行时状态是 `Engine.errorStreak int`，**递增**而非递减（递增只需归零，不需要"记住初始值再恢复"）。`errorStreak` 的归零时机：① **任意带 Choices 的 Response 事件**（`agentRunOnce` 事件循环内，第一个 token 即归、含流式部分块——语义是"配置层健康检查"，详见下条）；② `Ctx.Done`（ESC 打断，人已介入，同在 `agentRunOnce` 里归）；③ `New`（`/new` 在输入分支里提前 return，跑不到 `agentRunOnce`，必须在 `AgentStart` 的 `New` 分支单独归）；④ `AgentStart` 的兜底 `else` 分支对 `Continue`/`Int` 再归一次。**用户手动提交输入不归零**（2026-09 改，旧的第四个归零点已删）。耗尽只可能由**零输出失败**触发（鉴权 401/连接拒绝/上下文超限 400 这类配置层错误）；中途死亡类失败（流式出字后断）在 ① 的作用下永不耗尽，自动重试到成功或人介入为止——刻意的设计，见下条。**只有"零输出的自动重试链"内部不归零**——那正是要计数的时候。耗尽时三个不要改的点：`Code` 保持 `Error`（`Int` 的语义是"用户按了 ESC"，填假状态码去蹭"回到输入循环"的副作用是错的）；`errorStreak` **不归零**（归零会让下一轮重新满足自动重试条件，无限循环照旧）；用 `MsgContext = *EndTurn_p` 整体复用而不新造 literal（新建会静默丢掉 `Reason` 与 `PartialOutput`）。已知限制：`time.Sleep(errorSleepGap)` 期间引擎 goroutine 阻塞、不监听 `inputChan`，打字无反应（文本会留在框里），ESC 捕获也已被 defer 清掉，只有 Ctrl+C 能中断——要可中断得引入信号 channel 或 ctx，属额外机制，暂不做，靠 3 秒短间隔 + sleep **之前**先打提示来缓解。
- **errorStreak 的语义是"配置层健康检查"，不是"有界重试预算"** —— 归零条件：`agentRunOnce` 收到任何带 Choices 的 Response 事件，**第一个 token 就归零（含流式部分块）**。理由：能吐 token = key/端点/鉴权/本地网络全部证明可用，故障只可能来自传输层抖动（代理掐断、服务端断流），随机性强、重试期望为正，自动重试到成功为止；**零输出**失败（鉴权 401、连接拒绝、上下文超限 400）才是"配置有问题"，重试期望为零，3 次耗尽交还用户，"连续 N 次失败"提示的实际含义就是"检查配置"。历史：曾按"有界预算"语义否决"有输出就归零"（2026-09 上，理由是确定性中途失败会无限循环），同日把计数器目的重新定义为健康检查后改为采纳——**确定性中途失败（超时、内容过滤）会无限自动重试是刻意接受的代价**，靠人盯着兜底，配套动作是把主模型 `httptimeout` 调大让超时死亡本身变罕见。`PartialOutput` 不作为任何判据（它不含 `ReasoningContent` 与 `ToolCalls`，见下条）。手动输入不归零（见上条）。
- **`agentRunIteratively` 的输入循环用 "Error 优先 + else 兜底"** — 条件是 `if inputContext.Code == Error && (*e).errorStreak < errorMaxTimes { 自动重试 } else { 等用户输入 }`。**不要改回列举 `New || Continue || Int`**：① 那种写法没有最终 `else`，而 `inputContext` 是入参、循环体内从不被重新赋值，一旦有未列举的 code 走进来两个分支都不执行 → 循环体空转、100% CPU（今天 `Exit` 到不了那里只是因为 `showMsgAndExit` 末尾的 `select{}` 永久阻塞，是个脆弱前提）；② 兜底分支是"等用户输入"，比"自动构造 prompt 再打一次 API"安全得多——将来新增 `turnCode` 忘记登记，后果是多等一次用户输入，而不是无上限烧 API。重试预算判定刻意**内联**、不抽 `retriesExhausted` 中间变量：内联与循环外算一次等价（`errorStreak` 在本循环内从不变化——归零点都在 `agentRunOnce` 的 Ctx.Done / Response 事件分支与 `AgentStart` 的 New/else 分支）。
- **每类事件只在源头打印一次** — 错误在 `agentRunOnce` 的两个 return 分支各打一次（`RunError` 与 `TerminalError`，留消息区以便回溯），中断在 `agentRunOnce` 的 `Ctx.Done` 分支打一次（`ShowNotice(pretty.TBarCancelled())`，去 NoticeBar）。`agentRunIteratively` 循环顶部**只保留** `New` 的提示语（`ShowNotice(pretty.TBarNewConversation())`，同样去 NoticeBar）。反面记录（改前的实际现象）：TerminalError 会打两条，第二条是三层嵌套的 `对话发生错误: 对话过程中发生错误: Event发生TerminalError: <原始错误>`，同一个错误说三遍；ESC 中断会打两条不同文案（`会话已取消` + `输入已打断`）；且 `RunError` 只打一次、`TerminalError` 打两次，两条错误路径行为不一致。新增错误/中断类输出前先确认源头有没有打过。
- **不要把 if/else-if 链改成 switch/case** — 用户明确的风格要求。`AgentStart`、`agentRunIteratively` 等多处用 if/else-if 链处理 `turnCode`，看起来"很适合 switch"，**不要重构**。
- **`PartialOutput` 不可删** — `turnResult` / `AgentError` 的字段（原名 `OutputPart`，语义含糊已改），承载失败那一轮已累积的部分 assistant 输出，由 `gatherPartialOutput`（原名 `gatherContentMessage`）在事件循环里累积，唯一的读取点是 `agentRunIteratively` 的自动重试 prompt（`inputContext.PartialOutput != ""` 时拼进"之前的输出内容是: %s"）。**不能靠框架 session 代替**，证据链：① `runner.attachSessionAppender`（`runner.go:836`）确实逐个 event 落盘，但闸门 `shouldPersistEvent`（`runner.go:2818`）是 `len(StateDelta) > 0 || (Response != nil && !IsPartial && IsValidContent())` —— **流式 delta 全部被过滤**，中途失败时那个"完整最终 event"根本没产生，这一轮 assistant 文本在 session 里一个字都没有（**屏幕上看得见 ≠ session 里有**）；② 框架的补救机制 `WithPersistInterruptedAssistant` 默认 false，本项目 `init.go` 只设了 `WithSessionService` / `WithMemoryService`，未开启；③ 且 `persistInterruptedAssistant`（`runner.go:2383`）第一行是 `if ctx.Err() == nil || ... { return }` —— **只在 ctx 被取消时触发，TerminalError 时 ctx 健康，不触发**；④ 而取消那条路本项目自己的代码本来就丢弃部分输出（`agentRunOnce` 的 `Ctx.Done` 分支 `return nil`）。合起来：`PartialOutput` 只在 TerminalError 时携带数据，而这恰好是框架不覆盖的场景。两个附带认识：把它拼进一条 **user** 消息，角色语义是错位的（模型看到"用户告诉我我说过 X"），这是没有别的口子时的务实做法；Int 路径若要保留上下文，正确机制是 `WithPersistInterruptedAssistant`（落盘为 assistant 角色），代价是被打断的半句话永久进入历史并影响后续所有轮次，本项目未采用。
- **`AgentError.ErrorType` 是死字段** — 定义并赋值为 `"RunError"` / `"TerminalError"`，**全仓零读取点**。将来若要区分"可重试 vs 不可重试"错误（鉴权失败重试无意义），这是入口。注意它区分的是"哪一层报的错"，不是"有没有干活"；后者更好的代理是"本轮是否收到过任何 response event"，但 **`PartialOutput` 不能充当该代理**——`gatherPartialOutput` 只累积 `Choice.Delta.Content` / `Choice.Message.Content`，不含 `ReasoningContent` 也不含 `ToolCalls`，所以"只输出了思考内容"或"只发了 tool call 并跑完了工具"的轮次 `PartialOutput` 是空串。
- **系统消息不带装饰符号** — 语义完全由颜色承载（红=错误、黄=警告/中断、绿=成功、青=信息）。边界取 `utils/pretty/pretty.go` **自身的分节注释**：`── 状态消息 ──`（`TError` `TErrorF` `TSuccess` `TSuccessF` `TWarning` `TWarningF`）与 `── 生命周期 ──`（`TExit`）两节**去符号**；`── 对话内容 ──`（`TUserInput` 的 `▶`）、`── 推理区块 ──`（`»` `«`）、`── 工具区块 ──`（`●` `↪`）**保留**，因为那些是对话内容的视觉标记而非通知，且工具行的"绿点 + 橙色工具名"另有记录。新增系统消息 helper 时必须遵守；也不要在调用点用 `pretty.Symbol*` 手工拼符号前缀。去符号后这些 helper 实质上塌缩成 `TColoredText + 首尾换行`，但**刻意不合并**——`TError`/`TSuccess`/`TWarning` 的语义命名比裸的 `TColoredText` 有价值。helper 内部已改为返回 `[]Span`（结构化片段），调用方签名不变。
- **`(*e).Field` 风格** — 项目从包级全局变量重构为 `Engine`/`Tui` struct 时保留了 `(*e).Config_p` 这类指针解引用写法（init.go 里 100+ 处）。可以直接写 `e.Config_p`；看到新代码沿用此风格是历史原因，不必模仿。

## Auto-Extraction Memory (SQLite)

Persistent long-term memory using SQLite, with background LLM-based extraction after each turn. Agent can also manually use memory tools as a supplement.

### Architecture
- `service/engine/memory/sqlite.go` — factory: creates `memorysqlite.Service` with `extractor.NewExtractor(model)` + `WithExtractor(ext)`. Exposes `memory_search`, `memory_load`, `memory_add`, `memory_update` via `WithAutoMemoryExposedTools`. `memory_delete` and `memory_clear` are not exposed to agent (extractor handles delete internally).
- `service/engine/engineCore.go` — `SqliteMemoryService *memorysqlite.Service` field on Engine struct
- `service/engine/init.go` — `initSqliteMemoryService()` creates extractor model from config (via `models.Openai()` / `models.Anthropic()`), passes to `NewSQLiteMemoryService(config.Model, dbPath)`. Called after `loadConfig()`, before `newRunner()`.
- `service/engine/init.go` — the agent factory (`newRunner`) appends `SqliteMemoryService.Tools()` to agent tools and sets `WithPreloadMemory(10)` + `runner.WithMemoryService()`
- The framework runner auto-calls `EnqueueAutoMemoryJob()` after each turn — no manual trigger needed
- `service/engine/prompt/systemprompt.md` — brief `# Memory` section: explains auto-extraction runs in background, lists available manual tools

### Key behaviors
- **Auto-extraction**: extractor runs after each turn via `EnqueueAutoMemoryJob`. Uses the same model as the main agent. Determines what to store/update/delete through a dedicated LLM call with its own system prompt (`extractor/defaultPrompt`).
- **Agent supplement**: agent has `memory_search`, `memory_load`, `memory_add`, `memory_update` exposed. Can manually store or correct when it notices something the extractor missed.
- **Preload**: `WithPreloadMemory(10)` adaptively loads all memories (≤10) or searches top-10 by user query. Injected into system prompt.
- **Search**: keyword-based (BM25 + CJK gse segmentation). No embedder needed.
- **Reconcile**: extractor's `reconcileOps` checks new Add ops against existing memories for near-duplicates (BM25 score ≥0.90 or Jaccard ≥0.70 → skip; ≥0.60 or ≥0.40 → rewrite as update).

### Known tolerable issues
- Extractor's BM25 dedup may miss semantically-similar but lexically-different duplicates → occasional near-duplicate memories. Impact is negligible since retrieval is also BM25-based (fuzzy).
- Agent and extractor can both write (dual-writer). Extractor runs async after turn; agent writes inline. Minor race potential but practically harmless — fuzzy retrieval masks any duplicates.
- `UpdateMemory` changes memory ID (content-hash based) → extractor referencing old ID falls back to Add → may create duplicate. Again, fuzzy retrieval makes this invisible in practice.

### Gotchas
- `initSqliteMemoryService()` MUST be called before `newRunner()` — agent factory reads `SqliteMemoryService.Tools()`, nil service → panic → black screen
- `NewSQLiteMemoryService` imports `config`, `models`, `extractor`, and `model` — needs full config to create extractor model
- Default memory limit: 100000 (`service/engine/memory/sqlite.go:WithMemoryLimit`)
- Extractor model is the same as main model (same API endpoint/credentials)

## Context Management

HyperBot uses three complementary mechanisms to prevent context overflow:

### 1. Session Summarization (`service/engine/session/summarizer.go` + `service/engine/init.go`)
- `WithAddSessionSummary(true)` on the LLM agent enables async summary injection
- Summarizer triggers at `CheckTokenThreshold(0.6 * contextwindow)` OR `CheckTimeThreshold(10min)` via `WithChecksAny`
- `WithSkipRecent` preserves the last complete interaction cycle (from last user message to tail) from being summarized — keeps current turn intact in prompt
- `WithToolResultFormatter` truncates tool results to 1000 runes (head 500 + tail 500) before entering summary model input — reduces noise, improves summary quality. Only affects summary input; original events remain intact in session for `session_search`/`session_load`
- `WithSyncSummaryIntraRun(true)` enables synchronous summary refresh between LLM loop iterations in the same run — ensures compressed state is visible to next LLM call in long ReAct chains
- `WithSessionSummaryInjectionMode(SessionSummaryInjectionUser)` injects summary into user message instead of system message — keeps system prompt clean (SOP rules only), summary participates in normal window management
- Token counting uses `model/tiktoken` (BPE), configured via `summary.SetTokenCounter(counter)`
- Summary model is the same as main model; for DeepSeek reasoning models, the token counter falls back to `cl100k_base` (within ~4-7% of DeepSeek's actual count per empirical testing)
- If summaries fail silently (check `hyperbot.log` for "summary worker failed"), session continues uncompressed → context grows unbounded → API errors
- Post-summary hook strips `<think>...</think>` tags from summary text
- `WithToolResultFormatter` affects summary input AND threshold token counting — the formatter truncates content before token estimation, so the effective threshold is based on truncated content, not original. This is intentional: summary model sees cleaner input and produces better state recovery
- `extractTokenThresholdMessage` includes `ReasoningContent` in calculation (previously dropped silently, causing delayed summarization for DeepSeek reasoning models)
### 2. Context Compaction (`service/engine/init.go` — agent factory in `newRunner()`)
- `WithEnableContextCompaction(true)` enables deterministic tool result compression before each LLM call
- **Pass 1**: Historical tool results > 1024 tokens → replaced with placeholder (`event_id`/`tool_call_id` preserved for `session_load` recovery)
  - Protects current invocation + `KeepRecentRequests` (default 1) most recent completed invocations
- **Pass 2**: Any tool result > 8192 tokens → head+tail truncation with `[...N chars truncated...]` marker
  - Applies to ALL invocations including current; gated on `OversizedToolResultMaxTokens > 0`
- Triggers at 70% context window (`ContextCompactionThresholdRatio`, default 0.7)
- If still over threshold after compaction → sync `CreateSessionSummary` runs as fallback → request rebuilt

### 3. On-Demand Session (`service/engine/agent/base.go`)
- `WithEnableOnDemandSession(true)` gives agent `session_load`/`session_search` tools
- Compacted/truncated tool results can be retrieved by `event_id` with `content_offset`/`content_limit` for sliced loading

### Troubleshooting Context Overflow
- **Symptom**: API error "requested X tokens exceeds maximum Y" (X >> Y)
- **Check**: `hyperbot.log` for "summary worker failed" — if present, summaries are failing
- **Verify**: `contextwindow` in config MUST be ≤ actual model limit (not larger, or threshold triggers too late)
- **Note**: tiktoken `cl100k_base` vs DeepSeek API token count differs ~4-7% (empirically verified) — not enough to explain large discrepancies
- **Root cause pattern**: first summary attempt fails → delta grows unbounded → all subsequent attempts also fail (cascade failure)
- **Fix**: enable Context Compaction + lower `CheckTokenThresholdPercent` if needed

## Documentation Style

- README.md and README_en.md should be feature-focused and user-facing — highlight what the project does, not internal architecture or code organization
- Keep both language versions in sync when updating either one
