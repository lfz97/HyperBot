# TUI（go-tui）交接笔记

> 一阶段已完成并通过 pty 冒烟验证；二阶段 = 视图 .gsx 化 + 清除 tview 补丁。
> 分支：`feat/go-tui`，一阶段提交见 git log。

## 交接三原则（做任何改动前先读）

1. **优先框架原生能力**：视图用 .gsx + `tui generate`，组件用库内置的
   `<markdown>` / `<modal>` / `State` / Mount 机制。用框架就不要自己引入复杂性。
2. **tview 时代的补丁不要照抄**：旧代码里很多处理是因为 tview 能力不足打的补丁，
   go-tui 能原生解决的，直接去掉（清单见下文"任务 2"）。
3. **代码越简单易懂越好**：模板管视图，Go 管逻辑；拿不准时选更简单的那版。

## 一、现状

- 依赖：`github.com/grindlemire/go-tui v0.22.1`（已无 tview/tcell/glamour）
- `service/tui/` 文件职责：
  - `tui.go` —— TuiService 接口实现：引擎侧只写 staging（带锁）+ `app.MarkDirty()`
    （atomic，跨 goroutine 安全）；`Render` 在主循环以 staging 为唯一事实来源。
  - `ui.go` —— 根组件 agentUI：滚动状态（follow/scrollY）、KeyMap、HandleMouse、
    Watchers、各视图区块构建（二阶段迁 .gsx 的就是这部分）。
  - `tagbridge.go` —— tview 标签解析器（`[fg:bg:flags]` → 样式 run）。**这是 tview
    兼容补丁，二阶段任务 2 的头号清除对象**。
  - `tools.go` —— build tag `tools`，用 go.mod 钉住 tui CLI 版本（`go generate` 用）。

### 上一版（bbeefcf）四个 bug 的根因（防止复犯）

| 现象 | 根因 |
|---|---|
| 输出乱码 | 引擎推的 tview 颜色标签没走解析器，被当纯文本上屏 |
| 输入框不显示输入 | 根组件 Render 只建一次"保留式"树。**go-tui 的契约是每个 dirty 帧
  重新调用 Render() 构建新树**，有状态组件（TextArea/Markdown）经 `app.Mount`
  以稳定 key 跨帧复用、每帧重新 Render。违反契约 → TextArea 子树冻结在第 0 帧 |
| 除 Enter 外按键失灵 | `WithGlobalKeyHandler` 在有根组件时**永远不会执行**（legacy 路径）；
  聚焦组件的 focus-gated 绑定在分发第一优先级独占消费；Esc 会 blur 输入框，
  blur 后所有 OnFocused 绑定失配 → 全键盘假死 |
| 无法滚动 | 可滚动容器不会自己滚：键盘滚动要组件 KeyMap、滚轮要 MouseListener；
  且每帧新树会重置偏移 → 滚动位置必须作为组件状态持有 |

## 二、任务 1：视图层迁 .gsx

### 步骤

1. `go get golang.org/x/tools`（cmd/tui 代码生成器的依赖；`tools.go` 已预置）。
2. 写 `service/tui/agentui.gsx`（package tui），根模板大致结构：
   ```gsx
   templ (a *agentUI) Render() {
       <div class="flex flex-col" background={mustColor(pretty.TuiBg)}>
           <div class="flex flex-col grow overflow-y-scroll"
                scrollOffset={0, a.offsetY()}>
               for _, s := range a.t.snapshotSegs() {
                   if s.kind == segMarkdown {
                       <markdown source={s.text} key={s.id} />
                   } else {
                       @a.richView(s.text)
                   }
               }
           </div>
           if !a.t.exiting.Load() {
               ...todo / help / notice / 输入行...
           }
       </div>
   }
   ```
   退出态靠 `if !exiting` 收起输入区，同时 `offsetY()` 在 exiting 时返回大偏移值
   强制贴底（退出消息必须可见）——这两个行为是验证过的，别丢。
3. `go generate ./service/tui`（等价于 `go run github.com/grindlemire/go-tui/cmd/tui
   generate agentui.gsx`），生成 `agentui_gsx.go` 与 .gsx 一起提交。
4. 删掉 `ui.go` 里手写的视图构建：`Render` / `buildMsgs` / `buildTodo` / `buildHelp` /
   `buildNotice` / `buildInputRow` / `buildBanner`。**保留**逻辑方法：
   `offsetY` / `scrollBy` / `pageScroll` / `scrollTop` / `scrollEnd` / `interrupt` /
   `indicatorText` / `indicatorStyle` / `todoLineStyle`，以及 KeyMap/HandleMouse/Watchers。

### 关键库行为（调研结论，照做能省半天）

- `@a.ta`（直接渲染组件实例字段）生成的代码是 `a.ta.Render(app)`，**不会**打
  component 标 → 分发表发现不了它的 KeyMap。必须像 `examples/15-inline-mode` 那样：
  `KeyMap()` 里 `append(a.ta.KeyMap())`、`Watchers()` 里 `append(a.ta.Watchers())`。
- 生成器会产出 `bindAppFields`，自动绑定组件里的 `*tui.TextArea` 字段 →
  **不要**手写 `BindApp`（要覆盖就先调生成的 helper）。
- 循环里的 `<markdown>` 必须带 `key={s.id}`（seg 的单调 id），mount 身份才稳定。
- 滚动偏移：`scrollOffset={0, a.offsetY()}` 属性；跟随模式返回大偏移值
  （`1<<28`），布局时被钳到底部——等价 tview trackEnd。
- 每个 `templ` 体只放**一个**根元素；staging 一律经带锁方法读取
  （`a.t.snapshotSegs()` 等），模板里禁止直接摸 `a.t` 的可变字段（数据竞争）。
- textarea 的 autoFocus / 样式 / elementOptions 没有对应属性时，用 `options={...}`
  属性传 `[]tui.TextAreaOption`。

## 三、任务 2：清除 tview 补丁（go-tui 原生能解决的一律去掉）

按收益排序：

1. **tagbridge 整个删掉**（最大补丁）：tview 标签语法在 go-tui 里没有存在理由。
   改源头——`utils/pretty` 的 `TXxx`/`TColoredText`（28 处 `[-:-:-]` 输出）改为输出
   纯文本或结构化片段（text + style），消息区/通知栏直接按片段上色。
   `TuiService` 契约可顺势调整。注意：`compactLine` 里的 `ansi.Strip` 是数据清洗
   （命令输出真带 ANSI），**保留**。
2. **ReplaceTailInMsgView + 平面 buf/LastIndex 匹配**（渲染管线补丁）：
   go-tui 的 `<markdown state={...}>` 是响应式的——每条 assistant 消息建一个
   `*State[string]`，流式 delta 直接 append 进 state，组件原生重渲染。
   `messageRender.go` 里的"流式原文 + 结尾替换"两段式可以整个消失。
3. **帮助面板 → 原生 `<modal open={state}>`**：现在是的内联面板是权宜，
   库的 Modal 自带 backdrop/Esc 关闭/焦点圈定。
4. **staging mutex + MarkDirty 不是补丁，保留**：那是线程安全契约
   （引擎 goroutine 永不阻塞、UI 变更只在主循环落地）。
   `showMsgAndExit` 末尾的 `select{}`（引擎调用方永不返回）是引擎层契约，也保留。

## 四、回归验证清单（每次改动后过一遍）

冒烟程序要点（临时程序，不进库）：`GetTuiService` + goroutine 里推内容
（banner → 流式 delta → `ReplaceTail` markdown → 富文本 → todo/notice/running →
注册 escFn）+ 主 goroutine `t.Run()`；在 80×24 真终端（或 python pty.fork +
TIOCSWINSZ 驱动）里按下面的键序核对。

- [ ] 打字实时上屏；Enter 提交（inputChan 收到）；提交后输入框清空
- [ ] Esc 失焦后打字自愈（重新聚焦并补上字符）
- [ ] Esc×2 / Ctrl+C 触发 escFn（GOT-INTERRUPT 通知出现）
- [ ] Ctrl+K 帮助面板开关
- [ ] PgUp 上翻后新内容**不**把视图拽回底部；PgDn 翻回底部恢复跟随
- [ ] markdown（标题/列表/代码高亮）、工具块、用户回显、错误红色、CJK 对齐
- [ ] todo 栏 ◐/☐ 着色；通知 TTL 到期回落 "ctrl+k for help"；spinner 转动
- [ ] /exit 退出、启动错误"按任意键退出"且退出消息贴底可见、终端复原（无残留）

已知取舍（可接受，不要"顺手修"）：聚焦时 Esc 第一击失焦、第二击中断；
Home/End/↑/↓ 聚焦时归输入框光标、失焦时滚动；长会话流式期间每帧全量重建
消息树（.gsx 化后再考虑增量优化，先跑对再跑快）。
