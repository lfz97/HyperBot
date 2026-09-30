# TUI（go-tui）交接笔记

> 二阶段已完成：视图 .gsx 化 + tview 补丁清除，均通过 pty 冒烟（提交 cfcc1ad / c4655e5）。
> 本文件现在的作用：① 改 `service/tui` 前必读的库契约与踩坑记录 ② 回归冒烟的操作手册。

## 交接三原则（做任何改动前先读）

1. **优先框架原生能力**：视图用 .gsx + `tui generate`，组件用库内置的
   `<markdown>` / `<modal>` / RichText。用框架就不要自己引入复杂性。
2. **tview 时代的补丁不要复活**：tagbridge、平面 buf/ReplaceTail、内联帮助面板
   已删（见"二阶段改了什么"）；go-tui 原生能解决的，直接用原生。
3. **代码越简单易懂越好**：模板管视图，Go 管逻辑；拿不准时选更简单的那版。

## 一、现状（二阶段后）

- 依赖：`github.com/grindlemire/go-tui v0.22.1`（已无 tview/tcell/glamour）
- `service/tui/` 文件职责：
  - `agentui.gsx` —— 视图模板（+ agentUI 结构体/构造器）；`agentui_gsx.go` 是
    生成物，与 .gsx 一起提交。改模板后 `cd service/tui && go generate ./...`
    （`//go:generate` 指令在 ui.go 顶部；生成器依赖 x/tools 已进 go.mod）。
  - `ui.go` —— 交互逻辑：KeyMap / 鼠标 / Watchers / 滚动状态 / 指示器与
    通知栏内容计算 / textSegView 组件。
  - `tui.go` —— TuiService 实现：staging（带锁）+ snapshot 访问器 +
    `MarkdownDelta`/`MarkdownDone` + `showMsgAndExit`。
  - `style.go` —— pretty.Span（颜色字符串）→ go-tui 样式的边界映射 +
    `richEl`（WithRichText 富文本元素）。
  - `tools.go` —— build tag `tools`，钉住 tui CLI 版本。
- 展示契约：`pretty.TXxx` 返回 `[]pretty.Span`（text + 颜色字符串 + Bold/Dim），
  pretty 是纯数据不依赖 TUI 框架；通知是单 `pretty.Span`（`TBarXxx`）。

## 二、go-tui 库契约与踩坑（实测得出，v0.22.1）

**这些是二阶段冒烟真实踩出来的坑，比文档直觉更可靠。**

1. **`@expr` 的两种形态天差地别**：
   - `@a.ta`（字段/值）→ 只调 `Render(app)`，元素**不打 component 标**。
   - `@fn(args)` / `@a.method(app)`（函数调用）→ 走 `app.Mount`（打标、缓存、
     BindApp/Init 生命周期全生效）。
   - 陷阱 A：把 textarea 的 KeyMap 聚合到宿主组件 = **dispatch table 硬错误**
     （focus-gated 绑定的宿主必须实现 IsFocused 或被 mount）。所以 textarea
     用 `@a.inputView(app)`（返回自身实例的薄方法）渲染。
   - 陷阱 B：`*tui.Element` 实现了 Component（Render 返回自身）但**不实现
     PropsUpdater** → Mount 缓存后 factory 不再执行、内容冻结在首帧。任何
     内容会变的 @ 函数调用都必须包成实现 `UpdateProps` 的组件（见
     `textSegView`）。
2. **焦点是双层状态**：app 级（focusManager 的 element 索引）与组件级
   （`TextArea.focused` flag）。`ta.Focus()` 只置组件 flag，**不接**
   `app.BlurFocused()` 链路——失焦自愈/重聚焦必须用 `ke.App().FocusNext()`
   （textarea 是唯一 focusable 时等价于聚焦它），否则 Esc 永远被 blur 绑定
   吃掉、中断链路死锁。
3. **分发表的 validate 是硬错误**：两个**同名非 focus-gated 的 OnStop** 绑定
   （如根组件与 modal keyMap 都绑 ctrl+k）会让整表构建失败、dispatch 降级
   legacy 路径，表现是"按键行为全乱 + 屏幕残损"。修法：modal 打开时根组件
   不再绑 ctrl+k（`KeyMap()` 里按 `helpOpen.Get()` 条件生成）。
4. **modal 的 `WithModalKeyMap` 自定义绑定必须用 `OnPreemptStop`**：trapFocus
   的 AnyKey catch-all 是 preempt 轮（先于普通轮），普通 OnStop 绑定永远
   轮不到。modal 内建的 Esc 关闭本身就是 preempt，不受影响。
5. **`<markdown source={...} key={...}>` 流式更新无需 State**：Mount 缓存对
   PropsUpdater 组件每帧执行 factory + `UpdateProps(fresh)`，source 变化即
   重渲染（`ensureParsed` 按 source 串比对）。staging 里的段文本直接喂
   source 即可；`State[string]` 在这里没有额外收益（已验证 Mount 路径后弃用）。
6. **粘贴没有 bracketed paste 支持（v0.22.1 最新版也没有）**：终端粘贴的
   `\r` 与手敲 Enter 在字节层不可区分，多行粘贴会在第一个换行处被提交。
   解法是 app 层的 `pasteSafeInput` 包装组件（ui.go）：输入节奏启发式
   （窗口 50ms 内 ≥4 击 = 粘贴流，人类打字/按键重复率远够不着）判定粘贴
   时 Enter 改插换行。注意 pty 测试驱动一次性写入字符串 = 粘贴节奏，
   测"真人打字"必须逐字符发送。
7. **TextArea 没有 scroll-to-cursor（库注释原话）**：内容（含折行）超过
   `maxHeight` 钳制行数后，光标行被直接裁掉——超长输入/粘贴后所有编辑
   都发生在不可见处，看起来"输入框死了"。所以**不要设 maxHeight**：输入框
   随内容生长（tview 时代行为），光标永远可见。
8. **`//go:generate` 指令容易在整文件重写时弄丢**：丢了之后 `go generate`
   和 build.sh 的生成防护都会静默空转。改 ui.go 时检查指令还在
   `package tui` 之后。
6. **RichText 原生处理换行**：`WithRichText(spans...)` + `WithWrap(true)`，
   span 内的 `\n` 自然分行、行内多样式按词折行（`wrapSpans`）——不需要手工
   切行再横向拼装。span 的零值 Style 字段在渲染时继承元素基样式。
7. **滚动容器不会自己滚**：键盘滚动要组件 KeyMap、滚轮要 MouseListener，且
   每帧新树会重置偏移 → 滚动位置（follow/scrollY）是组件状态，模板经
   `scrollOffset={0, a.offsetY()}` 应用；贴底跟随写 `bigOffset`(1<<28) 由
   布局钳到底部。容器引用用 `ref={a.msgsRef}`（MaxScroll/ViewportSize）。
8. **staging mutex + MarkDirty 是线程契约**（引擎 goroutine 永不阻塞、UI 变更
   只在主循环落地），不是补丁，保留。`showMsgAndExit` 末尾的 `select{}`
   （引擎调用方永不返回）是引擎层契约，也保留。

## 三、二阶段改了什么（防复犯清单的反面）

| 已删除 | 替代物 |
|---|---|
| `tagbridge.go` 整个（tview 标签解析器） | `pretty.Span` 结构化片段 + `style.go` 边界映射 + `WithRichText` |
| `ReplaceTailInMsgView` + 平面 buf/LastIndex 匹配 | `MarkdownDelta`/`MarkdownDone` 直流进 markdown 段（messageRender 的"流原文+结尾替换"两段式消失） |
| 内联帮助面板 | 原生 `<modal open={State[bool]}>`（backdrop/Esc 关闭/焦点圈定） |
| `RenderMarkdown`（恒等函数） | 删除（无调用方） |
| 死代码：`TWelcome`/`TReady`/`TNewConversation`/`TInterrupted`/`TCancelled`/`TContentNoneStreamTag`/`TToolCall`/`TToolArgs`/`TToolResult`/`TDivider`/`TBg*`、未用色板 | 删除 |

注意：`compactLine` 里的 `ansi.Strip` 是数据清洗（命令输出真带 ANSI），**保留**。

## 四、回归验证清单（每次改动后过一遍）

冒烟方法（临时程序，不进库）：
1. 独立 module（`replace HyperBot => <repo>`）写 main：`GetTuiService` +
   goroutine 推内容（banner → 流式 `MarkdownDelta` → `MarkdownDone` →
   工具块/错误富文本 → todo/notice/running → 注册 escFn）+ 输入回声循环 +
   `/exit` 走 `ShowMsgAndExitNoTrigger`；主 goroutine `t.Run()`。
2. python `pty.fork()` + `TIOCSWINSZ`(80×24) + `pyte` 屏幕模拟，按下面键序
   发送并断言屏幕内容（断言"看屏幕"而非"看字节流"，流式 diff 输出不可靠）。

- [ ] 打字实时上屏；Enter 提交（inputChan 收到）；提交后输入框清空
- [ ] **多行粘贴**：首行不被提交、整段留在输入框、Backspace/打字可见、
      手动 Enter 整段提交（pty 驱动一次写入即粘贴节奏）
- [ ] Esc 失焦后打字自愈（重新聚焦并补上字符）
- [ ] Esc×2 / Ctrl+C 触发 escFn（GOT-INTERRUPT 通知出现）
- [ ] Ctrl+K 帮助 modal 开（backdrop+居中）；**Esc 关闭后输入立即可用**；
      Ctrl+K 也能开/关
- [ ] PgUp 上翻后新内容**不**把视图拽回底部；PgDn 翻回底部恢复跟随
- [ ] markdown（标题/列表/代码高亮）、工具块、用户回显、错误红色、CJK 对齐
- [ ] todo 栏 ◐/☐ 着色；通知 TTL 到期回落 "ctrl+k for help"；spinner 转动
- [ ] /exit 退出、退出消息贴底可见、终端复原（raw 末尾 `?1049l`/`?1000l`/`?25h`）

已知取舍（可接受，不要"顺手修"）：聚焦时 Esc 第一击失焦、第二击中断；
Home/End/↑/↓ 聚焦时归输入框光标、失焦时滚动；长会话流式期间每帧全量重建
消息树（先跑对再跑快，将来增量优化从 markdown 段的 mount 缓存下手）。
