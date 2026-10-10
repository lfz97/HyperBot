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
    ├── boot/boot.go         (启动编排: NewTui(e) → goroutine{e.Init → e.AgentStart} → tui.Run())
    │
    ├── service/engine/      (业务核心: Engine struct 封装全部状态，经 EngineView 接口暴露给 TUI pull)
    │   ├── engineCore.go    (Engine struct、GetEngineService 工厂、AgentStart 主循环、newSessionID、重试策略常量)
    │   ├── init.go          (preCheckLoad 初始化序列: config/skills/prompt/memory/工具集；newRunner 工厂)
    │   ├── engineRun.go     (agentRunIteratively 输入循环 /exit /new ESC；agentRunOnce 事件流消费；turnCode/turnResult)
    │   ├── uistate.go       (EngineView 的引擎侧实现: 记录日志/运行态/通知槽位/清单/横幅/帮助项 + SubmitInput/Interrupt)
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
    ├── service/tui/         (bubbletea v2 界面，风格延续 charm 生态 demo)
    │   ├── tui.go           (TUI model: Init/Update/View、EngineView 接口 + wire JSON 形状、NewTui/Run、pull 循环与 frameMsg)
    │   ├── component.go     (NewPrettyTextArea/NewPrettyViewport、recalcComponentSize、draw 拼版、帮助浮层 Compositor)
    │   ├── handler.go       (keyMsgHandler: 终态等键/浮层/enter 提交/esc 中断/ctrl+k)
    │   ├── render.go        (composeView 记录重放状态机、toolBuf、glamourCache、renderBody)
    │   ├── styles.go        (全部 lipgloss 颜色与文本样式 helpers——替代已删除的 utils/pretty)
    │   ├── banner.go        (启动横幅：三段拼版、lipgloss/ansi 宽度度量)
    │   ├── helps.go         (帮助浮层 items/选中/refresh)
    │   ├── bottom.go        (底部单行栏：spinner + gap + notice)
    │   ├── spinner.go       (方块彩虹 spinner: HSV 色轮)
    │   └── render_test.go   (渲染/按键/pull 的单测，fakeEngine 桩)
    │
    └── utils/               (pretty 包已删除——文本样式全部收进 service/tui/styles.go)
```

### Key Design Patterns

**UI Layout**（bubbletea v2 + lipgloss，无 tview）：整屏 = `viewport`(消息区，占剩余高度) + TodoBar(0 到 N 行动态渲染文本，无清单时不占行) + `textarea`(圆角边框色 62、动态高度 1→5) + `bottom`(单行：spinner + gap + notice)，两侧各 2 列 padding；帮助浮层用 lipgloss `Compositor` 居中叠加(z=1)。渲染线程模型：**pull 循环**（独立 goroutine，30ms tick）从引擎拉状态、做重活（全量重组 + glamour），打包成 `frameMsg` 经 `program.Send` 投递；事件循环(Update)只收轻量帧与终端事件——按键永不被渲染阻塞。model 字段全部只在 Update 单 goroutine 写，无需锁；pull 循环读宽度走 `widthAtomic`。按键：enter 提交（`SubmitInput` 失败保留输入）、shift+enter/ctrl+j 换行（textarea 的 InsertNewline 必须显式改绑，默认绑裸 enter）、esc 关浮层→运行中中断、ctrl+k 浮层开关、ctrl+c 退出。终态：帧带 fatal → 渲染上屏 → waitKey 则任意键退出，否则 400ms 后 `tea.Quit`（告别语不至于一闪而过）。

高度分配（`recalcComponentSize`）：`viewport` = 总高 - `ta.View()` 高(含边框,动态 1→5 行) - bottom 1 行 - todo 行数(`lipgloss.Height`)，最小 1；无清单时 todo 不占行（`draw` 里空串不进 JoinVertical）。

**Startup Flow**: `main()` → `boot.Boot()`:
- `tui.NewTui(e)` — 轻量构造（只装配子组件，不触盘、不阻塞），`*Engine` 经 `EngineView` 接口注入
- `go func() { e.Init(); e.AgentStart() }()` — **必须在 goroutine 里执行**：init 序列（`preCheckLoad` → `newRunner`）的错误路径会 `parkWithFatal`（置终态 + `select{}` 永久驻留），靠 TUI 的 pull 循环观察到 fatal 后渲染并停止。同步执行会在 `tui.Run()` 之前把 goroutine 永久卡死（首启无配置、配置错误、sqlite 初始化失败时白屏）
- `tui.Run()` — 主 goroutine 上跑 `tea.Program.Run()`，阻塞到 `tea.Quit`（终态 waitKey→任意键 / ctrl+c）；pull 循环由 `Init()` 返回的 cmd 起链（此时 program 已就绪，`Send` 安全）

**Agent Creation Flow**: `GetEngineService()` runs `preCheckLoad()`: redirects framework logs to `hyperbot.log`, loads/creates `hyperbot.yaml` and `skills/` folder, replaces `{{OSTYPE}}`, `{{HOME}}` and other placeholders in system prompt (`{{DATE}}`/`{{CWD}}` removed — now provided by the status bar), inits memory/session services, calls `loadSkills()` (populates `HelpItemsJSON`), then `newRunner()` registers an agent factory that builds the LLMAgent based on `APIType`. All init progress/error messages go to the message log (`Records`).

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

- **bubbletea/v2** v2.0.10 (`charm.land/bubbletea/v2`): TUI framework（Elm 架构：Model/Update/View，`tea.View` 携带 AltScreen/MouseMode）
- **bubbles/v2** v2.2.1 (`charm.land/bubbles/v2`): textarea/viewport/spinner/cursor 组件
- **lipgloss/v2** v2.0.6 (`charm.land/lipgloss/v2`): 样式与拼版（JoinVertical/JoinHorizontal/Compositor 浮层）
- **glamour** v2.0.1 (`charm.land/glamour/v2`): Markdown → ANSI renderer (Dracula theme, OSC 8 hyperlinks)
- **trpc-agent-go** v1.11.1-0.20260820131707-cdaece75b478: Agent framework core（含 #2501 修复；注意：session/summary 摘要机制在 v1.11 有重构，改动前对比两版行为）
- **trpc-agent-go/model/anthropic** v1.11.1-0.20260820131707-cdaece75b478: Anthropic model adapter（独立子模块，须单独升级）
- **trpc-agent-go/model/tiktoken** v1.11.0: Tiktoken-based token counter (replaces SimpleTokenCounter default)
- **trpc-mcp-go** v0.0.18: MCP tool protocol support（独立于 agent-go 版本，可单独升降）
- **openai-go** v1.12.0: OpenAI API SDK
- **anthropic-sdk-go** v1.66.0: Anthropic API SDK
- **zap** v1.28.0: Logging
- **otiai10/copy** v1.14.1: Cross-device file/directory copy for CP and MV tools

## Notes

- Go 1.26.4+ required
- Tests: `service/engine/uistate_test.go`（引擎 wire 契约）+ `service/tui/render_test.go`（渲染/按键/pull，fakeEngine 桩）
- Skills are loaded from `skills/` directory in Knowledge-Only mode (commands go through LocalExec)
- Framework logs are redirected to `hyperbot.log` to avoid TUI interference
- **帮助浮层动态项** — 默认项（ctrl+k、/new、/exit）在 `helps.refresh()` 里写死（`service/tui/helps.go`），技能项每次打开浮层时从引擎 `HelpItemsJSON()` 拉取（`loadSkills` 随时可能重建列表）。浮层样式延续 charm demo：居中圆角盒（色 62）、上下选择、enter 把指令插入输入框。
- **embedFS case sensitivity** - `//go:embed` + `ReadFile` paths are case-sensitive on Linux. File named `systemprompt.md` but code reading `systemPrompt.md` silently returns empty string (error ignored with `_`). Always match exact file name case between `go:embed` glob patterns and `ReadFile` calls.
- **`model/anthropic` 子模块版本对齐** — 根模块与 `model/anthropic` 子模块在 go.mod 中独立锁版本，升级根模块不会带上子模块修复。DeepSeek anthropic 端点要求 object schema 的 properties 非 null；无参 MCP 工具在旧版（v1.11.2 及更早）下序列化为 `"properties":null` 导致 400。升级时必须显式同时升级两者到含修复版本（#2501，commit cdaece75；现锁 `v1.11.1-0.20260820131707-cdaece75b478`）
- **Skills identity contamination** - The deployed `skills/` folder (`~/.hyperbot/skills/`) contains OpenClaw skill files (self-improving-agent, find-skills, etc.) that reference "OpenClaw", "Claude Code", "clawdhub". When loaded via `llmagent.WithSkills()`, these contaminate the agent's identity. Use only HyperBot-specific skills or keep the folder empty.
- **bracketed paste** - bubbletea v2 默认启用 bracketed paste，终端粘贴的整段文本（含换行）以 `tea.PasteMsg` 到达，`Update` 里转发给 textarea 处理。
- **Input multiplexing (inputCh)** - 引擎字段 `inputCh`（**unbuffered**），TUI 侧 enter 提交走 `Engine.SubmitInput(line)`（内部 select-default）：引擎忙（无人接收）时返回 false，TUI **不清空输入框**（`ta.Reset()` 只在投递成功时执行），用户输入永不丢失、事件循环永不阻塞。引擎循环用 `select { case userPrompt = <-e.inputCh: }` 读取（`service/engine/engineRun.go`）。select 是扩展点——计划任务结果回传（schedule agent）将在 select 上加 TriggerCh 分支 + 前置 DrainPending 检查。agent 运行期间用户敲 enter：投递失败内容保留，Shift+Enter/Ctrl+J 仍可换行、Ctrl+K 帮助仍可用。
- **Go RE2 regex in tool descriptions** - 在给 agent 暴露正则的 tool 中，jsonschema description 必须写明 RE2 限制：`(?s)` 让 `.` 匹配换行、`(?m)` 让 `^`/`$` 匹配行边界、不支持 lookahead/lookbehind/backreference。参考 `service/engine/tools/functions/file.go` 中 SearchInFile 的 description。
- **MV/CP tools use otiai10/copy** - `service/engine/tools/functions/filesystem.go` 的 CP 和 MV 跨设备移动使用 `github.com/otiai10/copy`（不区分文件/目录）。MV 同设备优先 `os.Rename`（快速+原子），跨设备走 `os.RemoveAll(dst)` → `copy.Copy` → `os.RemoveAll(src)`（先删目标避免合并语义）。
- **EditFile replace_all semantics** - `replace_all=false`（默认）是安全检查：多处匹配时报错拒绝替换，而非只替换第一处。`replace_all=true` 才执行全量替换。修改时不要移除 `len(Indexes) > 1` 的检查。
- **strings.Index loop pattern** - 在 `Now[offset:]` 上循环搜索时，`offset += idx` 定位到匹配起点后，必须再 `offset += len(matched)` 跳过已匹配内容，否则同一位置重复匹配导致死循环。
- **Tool call 后 agent 停止输出** - 如果 `hyperbot.log` 无 error，且对 agent 说"继续"能恢复对话，说明是模型侧在 tool result 后概率性预测了 stop token，不是框架 bug。不需要迁就式修改。
- **`models.Openai()` / `models.Anthropic()` are the canonical model constructors** — `service/engine/session/summarizer.go` and agent creation use these two functions. They handle DeepSeek variant detection, reasoning backfill, and API auth. When creating a new model instance from config, call these instead of manually assembling openai/anthropic options.
- **APIType dispatch duplicated 4×** — `service/engine/agent/base.go`、`service/engine/session/summarizer.go`、`service/engine/memory/sqlite.go`、`service/engine/models/*` 各有一份 `if APIType == "openai" ... else if "anthropic"` 分支。新增 provider 类型必须同步改这 4 处，否则 agent 会静默使用 nil summary/memory model。
- **ANSI 直出，无标签转义层** — bubbletea 渲染器本身就是 ANSI 消费者：glamour/lipgloss 的输出直接进 viewport，不存在 `TranslateANSI`/`tview.Escape`/`[-:-:-]` 补标签那一族 hack。来自框架或 LLM 的纯文本里的 `[TODO]`、`[urgent]` 都是字面量，不会被吞。
- **Tool response content must be skipped in content rendering** — `NewToolMessage` stores tool result in `Content` field alongside `Role="tool"`. Both stream and non-stream content paths check `Role != "tool"` to prevent tool JSON from leaking through the main content renderer. Without this, tool results appear as raw JSON in body text while also being formatted via `TToolResult`.
- **Multi-tool results handled in `engineRun.go`** — Framework merges parallel tool results into a single `tool.response` event with N Choices. `agentRunOnce` detects `ObjectTypeToolResponse` and iterates ALL Choices (not just `Choices[0]`), ensuring every result is rendered.
- **Glamour markdown rendering** — 正文渲染（`renderBody`，`service/tui/render.go`）**按宽度缓存 renderer**（`glamourCache`，重建要解析 style JSON，不能每条消息重建；pull 循环单 goroutine 使用，无需加锁）；`WithWordWrap(w)` 用组件宽度（`widthAtomic`，<40 兜底 80）让内容铺满终端、resize 后按新宽度重组；`document.margin = 0` 去掉暗色主题的 2 列左缩进。渲染失败退回原文（不吞回复）。三个后处理必须保留：① `glamourTailPad` 正则剥行尾"空白+SGR"填充（实测 153B markdown 渲出 12.7KB、92% 是填充）；② 剥填充会剥掉行尾 reset，非空行必须补 `\x1b[m`，否则颜色泄漏到后续行；③ `● ` 正文标记加在**渲染结果**第一个有可见内容的行上（加在 markdown 源码前会塌掉首行块级结构），行首是空行的场景（代码块/表格开头）不能直接前置。 glamour 输出恒以换行开头，先 TrimLeft/TrimRight 再拼。
- **`show_reasoning` config** — `config.Model.ShowReasoning` (`yaml:"show_reasoning"`) controls whether reasoning/thinking content is displayed. Default `false`. Affects both stream (chunk-level skip) and non-stream (whole-block skip) paths. Set `show_reasoning: true` in `hyperbot.yaml` to enable.
- **composeView 记录重放状态机** — `service/tui/render.go`。全量重组：对全部记录重放"流式尾段（tailReasoning/tailContent）+ 工具调用缓冲（toolBuf，同 ID 合并、无 ID 按 index 兜底）"，无缓存、正确性不依赖记账（缓存曾制造三个静默失效 bug，已整体移除）。流式尾段与定稿同型渲染，观感一致无切换跳变；边界记录（user/slash/warn/error/summary）先 `flushTail` 再输出自己。重组按 `composeInterval`(100ms) 节流，delta 高频到达时的无效重组合并掉。
- **Engine init must stay in a goroutine** — `boot.Boot()` 把 `Init`（内含 `preCheckLoad`）放在 goroutine 里跑，与 `tui.Run()` 并发。不要改回同步：`parkWithFatal` 末尾用 `select{}` **故意永久阻塞**，靠 TUI 的 pull 循环观察到 fatal → 渲染 → 等键/延时 → `tea.Quit` → `Run()` 返回收尾。两个不要改的点：① 不要把 `select{}` 换成"回调 close(chan) 后返回"——调用方（init 序列）打完致命错误后并没有 `return`，helper 一返回引擎 goroutine 就会带着未初始化的状态（如 nil memory service）继续跑，最后在别处 panic；② 不要在按键回调里直接 `os.Exit`——bubbletea 的终端还原（退出 alt-screen、恢复 cooked mode）在 `p.Run()` 的返回路径上，硬退出会跳过它。
- **`go build ./cmd` 会失败** — 输出文件名与 `cmd/` 目录冲突（"build output cmd already exists and is a directory"）。构建必须带 `-o`（`go build -o HyperBot ./cmd`）或使用 `cmd/build.sh`（需在 cmd/ 目录内运行）。
- **TUI 依赖接口：单一 `EngineView`（消费方定义）** — `service/tui/tui.go` 里的 `EngineView`（9 方法：Version/Records/RunStateJSON/TodoText/NoticeJSON/StartupInfo/HelpItemsJSON/SubmitInput/Interrupt）是 TUI 对引擎的全部依赖，上下游零 import、跨界只有 stdlib 类型与 JSON 文本。引擎侧消费方的小接口不变：`session.msgPrinter`（1 方法）、`agent.Display`（1 方法：`SetTodoText`，名字按能力命名，将来加别的展示项直接扩方法）。给 `EngineView` 加/改方法时注意 wire JSON 形状（wireRecord/wireRunState/wireNotice/wireHelpItem）与引擎侧 `uistate.go` 的 schema 对齐。
- **`agent.OpenaiAgent` / `agent.AnthropicAgent` 已删除** — 它们是零逻辑的纯透传包装（函数体只有一行 `ConfigBaseAgent(...)`），而 `ConfigBaseAgent` 内部本来就按 `m.APIType` 分支选模型，`init.go` 又在同一个字段上分支一次去选包装函数——**同一个判断做两遍，中间那层唯一的作用是被穿过去**。现在 `init.go` 直接调 `agent.ConfigBaseAgent`，`agent/` 目录只剩 `base.go` + `status.go`。`APIType` 校验改用排除法单独做一次且**必须保留**：`ConfigBaseAgent` 对未知类型是静默不设模型（agent 照样建得出来、跑起来才失败），那个 `if apiType != "openai" && apiType != "anthropic"` 是唯一能给出可读配置错误的地方。⚠️ 将来给 `ConfigBaseAgent` 加参数时**不要再引入 per-APIType 的包装层**——参数穿透的噪音来源是包装函数本身，不是参数。同理，新的 `llmagent.Option`（如状态栏回调 `agent.SetBeforeModelStatusCallback(display)`）**一律在 `init.go` 的 `opts` 列表里构造**，不要在 `ConfigBaseAgent` 内部追加：`ConfigBaseAgent` 只负责按 APIType 选模型，装配意图集中在调用点才看得全。
- **pull 循环是渲染的心跳（drawLoop 的等价物）** — `service/tui/tui.go` 的 `startPullLoop`。独立 goroutine、30ms tick，每帧拉引擎五类状态（横幅一次/消息日志按版本/运行态/清单/通知）+ 终态观察，重活（全量重组、glamour）都在这个 goroutine 上；渲染结果打包 `frameMsg` 经 `program.Send` 投进事件循环。收益：按键永不被渲染阻塞（对比旧 tview 版把重组放 UI goroutine）。约束：① 帧去重（`frame != lastFrame` 才 Send）——空闲零开销，fatal/notice/todo 的变化天然触发；② model 字段只在 Update 写、pull 侧只读 `widthAtomic`（atomic.Int32），不碰 model；③ 帧里的 view/todo/notice 都是**已渲染的最终形态文本**（上色、截断都在 pull 侧完成），Update 只做赋值与布局重算；④ fatal 不 bump Version——`pullState.lastFatal` 变化必须显式置 pending，否则终态消息不上屏。
- **底部栏与 spinner** — `service/tui/bottom.go` + `spinner.go`。单行 `spinner + gap + notice`（charm demo 同款）；spinner 是方块彩虹帧（HSV 色轮逐帧推进），运行态由 `applyFrame` 检测 running 翻转调 `Start()` 起帧链（返回 tick cmd 续链），`Stop()` 后链自然断——不需要独立时钟。gap 为负时夹 0（`strings.Repeat` 负数会 panic）。notice 槽位 TTL（4s，从引擎 setAt 起算）与常驻 hint 的取舍在 `pullFrame` 里做，运行态 hint 多一条 "esc to interrupt"。
- **TodoBar 的 `◐`/`☐` 是"系统消息不带符号"规则的豁免** — 三条理由：① 它们承载 **per-item 状态语义**（进行中 vs 待办），不是装饰；② 各行的状态差别靠 `todoLines` 按行首符号逐行上色（进行中青色、待办正文色、头行/计数行暗灰）——按前缀判色不需要 sink 推结构化数据，但前提就是符号必须存在，去掉符号颜色区分随之失效；③ `[TODO]` 前缀保留（在专门的栏里起到"这栏是什么"的标识作用）。**不要拿"系统消息不带符号"那条规则来"修"它。** ANSI 输出下方括号是字面量，无需转义。
- **Startup Banner** — `service/tui/banner.go`（**刻意独立成文件**：横幅是纯数据 + 纯函数，与组件装配是两类东西）。程序启动时往消息区写一段一次性横幅：左侧 5×12 的蓝青渐变 `H` 字标、中间配置摘要（model+上下文窗口 / endpoint / cwd / tools·skills·mcp / session，label 补齐到 13 列）、右侧带方框的 `getting started` 键位提示（3 条，加边框正好 5 行，与 logo 和信息列**等高齐平**——加减面板条目会破坏这个等高关系）。**总宽不固定，由内容决定**：三段里只有信息列是弹性的，降级顺序是 ① 放得下 → 三列完整版零截断 ② 放不下 → 信息列压到剩余空间（截断补 `…`）保住三列 ③ 压到 `bannerConfigMinWidth`(24) 以下 → 纵向堆叠、去掉方框。**挂载时机**：pull 循环在 `StartupInfo` 就绪后的第一帧 `composeBanner` 一次（引擎 init 未完成时等下一帧再拉），必然只渲染一次。**它是"死文本"**：写入后随对话滚动、resize 不重排（viewport 软换行兜底）——这是刻意的语义（启动横幅本来就是一次性历史记录）。拼版按 OOP 组织：`banner` 结构体描述三段内容（`bannerLogo`/`bannerLogoColors` 包级数据 + `info` + `bannerPanel`），`newBanner(infoLines).compose(width)` 负责拼版——拼版方法不读 model、宽度作参数传入（保持可单测性）。职责划分：Engine 的 `startupInfoLines()` 决定**显示什么**（label 补齐到 13 列、`$HOME`→`~`、BaseURL 只取 host、session id 截前 8 位、`SkillRepo` 判空——`loadSkills` 里 `NewFSRepository` 的错误被 `_` 忽略了，不判空会启动即 panic），TUI 决定**怎么排**。不含版本号（项目无版本注入机制，`build.sh` 的 ldflags 只有 `-s -w`）。
- **宽度度量：`ansi.StringWidth` / `lipgloss.Width`/`Height`，不要用 rune 数** —— 中文是 2 cell、`█`/`╭╮╰╯│─`/`⬝`/`→` 实测都是 1 cell，且度量函数对 ANSI 序列与宽字符都正确处理。凡是要按显示宽度对齐/补齐/截断的地方（横幅拼版、底部栏 gap）一律走这两个 API；截断用 `ansi.Truncate(s, w-1, "…")`（display-width-aware）。
- **`SetAgentRunning` 的调用位置** — `service/engine/engineRun.go` 的 `agentRunIteratively` 里，紧贴 `agentRunOnce` 调用：`setRunning(true)` + `defer setRunning(false)`。两个不要改的点：① 用 `defer` 而不是调用后直接复位——`agentRunOnce` 跑的是框架代码，panic 时 spinner 会永远转；② **不要挂到 `agentRunIteratively` 开头那个 `Ctx, cancel := context.WithCancel(Ctx)` 上**——那个 ctx 是给 ESC 中断用的，生命周期包含前面等用户输入的阶段，挂上去 spinner 会在用户还没打字时就转起来。运行态逻辑刻意不放在 `agentRunOnce` 内部：UI 表现是调用方的关注点，且 "Input multiplexing" 条里计划的 schedule agent 可能非交互地调用 `agentRunOnce`。
- **viewport 底部跟随** — 写入前取 `AtBottom()`（写入后内容变长，同一滚动位置会被误判成"不在底部"），`SetContent` 后在底才 `GotoBottom()`——用户上翻查看历史时不被打扰，回到底部后恢复跟随。鼠标滚轮由 `tea.MouseWheelMsg` 转发 viewport 处理。
- **连续错误重试策略** — `service/engine/engineCore.go` 的 `AgentStart` + `service/engine/engineRun.go` 的 `agentRunIteratively`。改前 `Code == Error` 会直接构造一句"之前的对话发生了错误……请继续完成对话"再跑一轮，**无等待、无次数上限**：API key 失效/断网/额度耗尽这类持久性错误会导致无限热循环，每轮都发真实 API 请求、都打一条错误、都新建 `context.WithCancel`、都重注册 ESC 捕获。现在：两个编译期常量 `errorMaxTimes = 3` / `errorSleepGap = 3 * time.Second`（刻意不做成 yaml 配置，也没做成 Engine 字段——它们是常量，做成字段只是多一层间接且无人会赋别的值）；唯一的运行时状态是 `Engine.errorStreak int`，**递增**而非递减（递增只需归零，不需要"记住初始值再恢复"）。`errorStreak` 的归零时机：① **任意带 Choices 的 Response 事件**（`agentRunOnce` 事件循环内，第一个 token 即归、含流式部分块——语义是"配置层健康检查"，详见下条）；② `Ctx.Done`（ESC 打断，人已介入，同在 `agentRunOnce` 里归）；③ `New`（`/new` 在输入分支里提前 return，跑不到 `agentRunOnce`，必须在 `AgentStart` 的 `New` 分支单独归）；④ `AgentStart` 的兜底 `else` 分支对 `Continue`/`Int` 再归一次。**用户手动提交输入不归零**（2026-09 改，旧的第四个归零点已删）。耗尽只可能由**零输出失败**触发（鉴权 401/连接拒绝/上下文超限 400 这类配置层错误）；中途死亡类失败（流式出字后断）在 ① 的作用下永不耗尽，自动重试到成功或人介入为止——刻意的设计，见下条。**只有"零输出的自动重试链"内部不归零**——那正是要计数的时候。耗尽时三个不要改的点：`Code` 保持 `Error`（`Int` 的语义是"用户按了 ESC"，填假状态码去蹭"回到输入循环"的副作用是错的）；`errorStreak` **不归零**（归零会让下一轮重新满足自动重试条件，无限循环照旧）；用 `MsgContext = *EndTurn_p` 整体复用而不新造 literal（新建会静默丢掉 `Reason` 与 `PartialOutput`）。已知限制：`time.Sleep(errorSleepGap)` 期间引擎 goroutine 阻塞、不监听 `inputChan`，打字无反应（文本会留在框里），ESC 捕获也已被 defer 清掉，只有 Ctrl+C 能中断——要可中断得引入信号 channel 或 ctx，属额外机制，暂不做，靠 3 秒短间隔 + sleep **之前**先打提示来缓解。
- **errorStreak 的语义是"配置层健康检查"，不是"有界重试预算"** —— 归零条件：`agentRunOnce` 收到任何带 Choices 的 Response 事件，**第一个 token 就归零（含流式部分块）**。理由：能吐 token = key/端点/鉴权/本地网络全部证明可用，故障只可能来自传输层抖动（代理掐断、服务端断流），随机性强、重试期望为正，自动重试到成功为止；**零输出**失败（鉴权 401、连接拒绝、上下文超限 400）才是"配置有问题"，重试期望为零，3 次耗尽交还用户，"连续 N 次失败"提示的实际含义就是"检查配置"。历史：曾按"有界预算"语义否决"有输出就归零"（2026-09 上，理由是确定性中途失败会无限循环），同日把计数器目的重新定义为健康检查后改为采纳——**确定性中途失败（超时、内容过滤）会无限自动重试是刻意接受的代价**，靠人盯着兜底，配套动作是把主模型 `httptimeout` 调大让超时死亡本身变罕见。`PartialOutput` 不作为任何判据（它不含 `ReasoningContent` 与 `ToolCalls`，见下条）。手动输入不归零（见上条）。
- **`agentRunIteratively` 的输入循环用 "Error 优先 + else 兜底"** — 条件是 `if inputContext.Code == Error && (*e).errorStreak < errorMaxTimes { 自动重试 } else { 等用户输入 }`。**不要改回列举 `New || Continue || Int`**：① 那种写法没有最终 `else`，而 `inputContext` 是入参、循环体内从不被重新赋值，一旦有未列举的 code 走进来两个分支都不执行 → 循环体空转、100% CPU（今天 `Exit` 到不了那里只是因为 `showMsgAndExit` 末尾的 `select{}` 永久阻塞，是个脆弱前提）；② 兜底分支是"等用户输入"，比"自动构造 prompt 再打一次 API"安全得多——将来新增 `turnCode` 忘记登记，后果是多等一次用户输入，而不是无上限烧 API。重试预算判定刻意**内联**、不抽 `retriesExhausted` 中间变量：内联与循环外算一次等价（`errorStreak` 在本循环内从不变化——归零点都在 `agentRunOnce` 的 Ctx.Done / Response 事件分支与 `AgentStart` 的 New/else 分支）。
- **每类事件只在源头打印一次** — 错误在 `agentRunOnce` 的两个 return 分支各打一次（`RunError` 与 `TerminalError`，留消息日志以便回溯），中断在 `agentRunOnce` 的 `Ctx.Done` 分支打一次（`setNotice(NoticeCancelled, "")`，去通知栏）。`agentRunIteratively` 循环顶部**只保留** `New` 的提示语（`setNotice(NoticeNewConversation, "")`，同样去通知栏）。反面记录（改前的实际现象）：TerminalError 会打两条，第二条是三层嵌套的 `对话发生错误: 对话过程中发生错误: Event发生TerminalError: <原始错误>`，同一个错误说三遍；ESC 中断会打两条不同文案（`会话已取消` + `输入已打断`）；且 `RunError` 只打一次、`TerminalError` 打两次，两条错误路径行为不一致。新增错误/中断类输出前先确认源头有没有打过。
- **不要把 if/else-if 链改成 switch/case** — 用户明确的风格要求。`AgentStart`、`agentRunIteratively` 等多处用 if/else-if 链处理 `turnCode`，看起来"很适合 switch"，**不要重构**。
- **`PartialOutput` 不可删** — `turnResult` / `AgentError` 的字段（原名 `OutputPart`，语义含糊已改），承载失败那一轮已累积的部分 assistant 输出，由 `gatherPartialOutput`（原名 `gatherContentMessage`）在事件循环里累积，唯一的读取点是 `agentRunIteratively` 的自动重试 prompt（`inputContext.PartialOutput != ""` 时拼进"之前的输出内容是: %s"）。**不能靠框架 session 代替**，证据链：① `runner.attachSessionAppender`（`runner.go:836`）确实逐个 event 落盘，但闸门 `shouldPersistEvent`（`runner.go:2818`）是 `len(StateDelta) > 0 || (Response != nil && !IsPartial && IsValidContent())` —— **流式 delta 全部被过滤**，中途失败时那个"完整最终 event"根本没产生，这一轮 assistant 文本在 session 里一个字都没有（**屏幕上看得见 ≠ session 里有**）；② 框架的补救机制 `WithPersistInterruptedAssistant` 默认 false，本项目 `init.go` 只设了 `WithSessionService` / `WithMemoryService`，未开启；③ 且 `persistInterruptedAssistant`（`runner.go:2383`）第一行是 `if ctx.Err() == nil || ... { return }` —— **只在 ctx 被取消时触发，TerminalError 时 ctx 健康，不触发**；④ 而取消那条路本项目自己的代码本来就丢弃部分输出（`agentRunOnce` 的 `Ctx.Done` 分支 `return nil`）。合起来：`PartialOutput` 只在 TerminalError 时携带数据，而这恰好是框架不覆盖的场景。两个附带认识：把它拼进一条 **user** 消息，角色语义是错位的（模型看到"用户告诉我我说过 X"），这是没有别的口子时的务实做法；Int 路径若要保留上下文，正确机制是 `WithPersistInterruptedAssistant`（落盘为 assistant 角色），代价是被打断的半句话永久进入历史并影响后续所有轮次，本项目未采用。
- **`AgentError.ErrorType` 是死字段** — 定义并赋值为 `"RunError"` / `"TerminalError"`，**全仓零读取点**。将来若要区分"可重试 vs 不可重试"错误（鉴权失败重试无意义），这是入口。注意它区分的是"哪一层报的错"，不是"有没有干活"；后者更好的代理是"本轮是否收到过任何 response event"，但 **`PartialOutput` 不能充当该代理**——`gatherPartialOutput` 只累积 `Choice.Delta.Content` / `Choice.Message.Content`，不含 `ReasoningContent` 也不含 `ToolCalls`，所以"只输出了思考内容"或"只发了 tool call 并跑完了工具"的轮次 `PartialOutput` 是空串。
- **系统消息不带装饰符号** — 语义完全由颜色承载（红=错误、黄=警告/中断、绿=成功、青=信息）。样式 helpers 现全部在 `service/tui/styles.go`（lipgloss）：`errText`/`successText`/`warnText`/`exitText`（状态消息）与 `noticeXxx`（通知栏）**不带符号**；`userEcho` 的 `▶`、`contentTag` 的 `●`、`toolCompact` 的 `● ↪`、`reasoningBlock` **保留**——那些是对话内容的视觉标记而非通知，且工具行的"绿点 + 橙色工具名"另有记录。新增样式 helper 时必须遵守。语义命名（`errText` 等）比裸的 `colorText` 有价值，**刻意不合并**。
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
