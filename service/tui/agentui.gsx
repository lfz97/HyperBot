package tui

import (
	"HyperBot/utils/pretty"

	tui "github.com/grindlemire/go-tui"
)

// agentUI 是 go-tui 的根组件：视图在本文件（模板），交互逻辑在 ui.go。
//
// 渲染契约：Render() 每个 dirty 帧重新执行、构建全新的元素树；
// 有状态 widget（markdown / textarea）以稳定 key 跨帧复用实例。
// staging 一律经带锁的 snapshot 方法读取（模板里禁止直接摸可变字段）。
type agentUI struct {
	t *Tui

	ta *tui.TextArea // 输入框组件实例（跨帧同一实例；经 input 包装组件 mount 渲染）

	// ── 以下字段仅主循环读写 ──
	follow   bool       // 贴底跟随：新内容到达时自动滚到底
	scrollY  int        // 非跟随态的滚动偏移
	spinN    int        // spinner 帧计数
	msgsRef  *tui.Ref   // 消息区滚动容器（滚动计算的参照）
	helpOpen *tui.State[bool] // 帮助面板（原生 modal）开关
	input    *pasteSafeInput // 输入框的粘贴防护包装（见 ui.go）
}

func newAgentUI(t *Tui) *agentUI {
	a := &agentUI{t: t, follow: true, msgsRef: tui.NewRef(), helpOpen: tui.NewState(false)}
	a.ta = tui.NewTextArea(
		tui.WithTextAreaAutoFocus(true),
		// 不设 maxHeight：库的 TextArea 没有 scroll-to-cursor，
		// 内容超过钳制行数后光标行被裁掉、编辑全部发生在不可见处
		// （粘贴/超长输入"看起来死了"）。放开后输入框随内容生长，
		// 光标永远可见——与 tview 时代行为一致。
		tui.WithTextAreaTextStyle(mainStyle),
		tui.WithTextAreaElementOptions(
			tui.WithFlexGrow(1),
			tui.WithBackground(inputBg),
		),
		tui.WithTextAreaOnSubmit(t.submitInput),
	)
	a.input = newPasteSafeInput(a.ta)
	return a
}

templ (a *agentUI) Render() {
	<div class="flex-col" background={bgStyle}>
		// 消息区：可滚动、占满剩余空间。退出态仍保留（退出消息必须可见）。
		<div
			ref={a.msgsRef}
			class="flex-col grow overflow-y-scroll"
			background={bgStyle}
			scrollOffset={0, a.offsetY()}>
			if a.t.bannerSetSnapshot() {
				for _, line := range a.t.bannerLinesSnapshot() {
					<span class="truncate" textStyle={subStyle}>{line}</span>
				}
			}
			for _, s := range a.t.segsSnapshot() {
				if s.kind == segMarkdown {
					<markdown source={s.text} key={s.id} />
				} else {
					@newTextSegView(s.spans)
				}
			}
		</div>
		// 退出态剥掉输入区：没有任何 focusable，"任意键退出"的 AnyKey
		// 绑定才不会被聚焦的 textarea 抢先消费。
		if !a.t.exiting.Load() {
			if a.t.todoTextSnapshot() != "" {
				for _, line := range a.t.todoLinesSnapshot() {
					<span class="truncate" textStyle={todoLineStyle(line)}>{line}</span>
				}
			}
			<span class="w-full text-right" height={1} textStyle={a.noticeStyle()}>{a.noticeText()}</span>
			<div class="flex items-end">
				<span width={2} height={1} background={inputBg} textStyle={a.indicatorStyle()}>{a.indicatorText()}</span>
				@a.inputView(app)
			</div>
			// 帮助面板：原生 modal（backdrop/Esc 关闭/焦点圈定），打开期间
			// trapFocus 拦截父组件按键，ctrl+k 经 modal keyMap 关闭。
			<modal
				open={a.helpOpen}
				class="justify-center items-center"
				backdrop="dim"
				keyMap={a.helpModalKeyMap()}>
				<div
					class="flex-col"
					width={60}
					border={tui.BorderRounded}
					borderTitle=" slash commands — ctrl+k 关闭 "
					padding={1}
					background={bgStyle}>
					for _, it := range a.t.helpItemsSnapshot() {
						<div class="flex">
							<span class="truncate" width={16} textStyle={mainStyle}>{it.cmd}</span>
							<span class="truncate grow" textStyle={subStyle}>{it.desc}</span>
						</div>
					}
				</div>
			</modal>
		}
	</div>
}
