package tui

import (
	"HyperBot/utils/pretty"

	gotui "github.com/grindlemire/go-tui"
)

// 帮助面板（slash commands）的交互逻辑。视图在 agentui.gsx，结构是原生
// modal（贴底 palette，见模板注释）。
//
// 选择模型：焦点即选中。行是可聚焦元素，onFocus 回调把选中态同步回
// agentUI.helpSel 供渲染高亮；onActivate 由 modal 内建路径触发：
//   - Enter：modal 内建绑定激活聚焦元素（我们的自定义 Enter 会被内建
//     绑定抢先消费——同通道先到先得，所以必须走这条路）
//   - 鼠标点击：modal HandleMouse 沿祖先链找 onActivate
// ↑/↓/Tab 都只是移动焦点（FocusPrev/Next 被 trapFocus scope 圈在行的
// 范围内循环），任何路径下高亮与焦点都不会脱节。modal 关闭时库会还原
// 之前的焦点（输入框）。

// selBgStyle 选中行的高亮底色（与边框同源的暗蓝灰，克制）。
var selBgStyle = gotui.NewStyle().Background(mustColor(pretty.TuiBorderColor))

// toggleHelp 开关帮助面板。
func (a *agentUI) toggleHelp() {
	if !a.helpOpen.Get() {
		a.helpSel = 0 // 打开时选中复位；首个行的 onFocus 会再次对齐
	}
	a.helpOpen.Set(!a.helpOpen.Get())
}

// helpModalKeyMap modal 打开期间的补充绑定（ctrl+k 关闭 + ↑/↓ 移动焦点）。
// 必须用 OnPreemptStop：trapFocus 的 AnyKey catch-all 也是 preempt 且先于
// 普通 轮分发，非 preempt 的自定义绑定永远轮不到（Esc 关闭由 modal 内建）。
// 注意不要再绑 Enter——modal 内建的"激活聚焦元素"Enter 排在自定义之前，
// 同 key 同通道先匹配先消费，自定义的永远轮不到。
func (a *agentUI) helpModalKeyMap() gotui.KeyMap {
	return gotui.KeyMap{
		gotui.OnPreemptStop(gotui.Rune('k').Ctrl(), func(ke gotui.KeyEvent) { a.toggleHelp() }),
		gotui.OnPreemptStop(gotui.KeyUp, func(ke gotui.KeyEvent) { ke.App().FocusPrev() }),
		gotui.OnPreemptStop(gotui.KeyDown, func(ke gotui.KeyEvent) { ke.App().FocusNext() }),
	}
}

// setHelpSel 行 onFocus 回调：焦点即选中，同步高亮状态。同值不重绘
// （refreshFromTree 每帧恢复焦点位时会重放 onFocus，没有这个护栏会
// 每帧 MarkDirty 空转）。
func (a *agentUI) setHelpSel(i int) {
	if a.helpSel != i {
		a.helpSel = i
		a.t.app.MarkDirty()
	}
}

// helpAccept 接受第 i 项：命令文本插入输入框光标处并关闭面板。
// InsertText 推进光标到插入内容之后；modal 关闭时库自动把焦点还原输入框。
func (a *agentUI) helpAccept(i int) {
	items := a.t.helpItemsSnapshot()
	if i < 0 || i >= len(items) {
		return
	}
	a.ta.InsertText(items[i].cmd)
	a.helpOpen.Set(false)
}

// helpRow 帮助面板行组件（模板 @fn 调用形式 mount，生成器按迭代索引
// 分配稳定 key）。
func (a *agentUI) helpRow(i int, it helpItem) gotui.Component {
	return newHelpRowView(i, it.cmd, it.desc, i == a.helpSel,
		func() { a.setHelpSel(i) },
		func() { a.helpAccept(i) })
}

// helpRowView 帮助面板的命令行：可聚焦 + onActivate，选中行高亮底色。
// 元素每帧在 UpdateProps 重建（与 ta 同款生命周期，焦点位由库的
// refreshFromTree 按位置保持）。
type helpRowView struct {
	idx       int
	cmd, desc string
	selected  bool
	onFocus   func()
	onAccept  func()
	el        *gotui.Element
}

func newHelpRowView(idx int, cmd, desc string, selected bool, onFocus, onAccept func()) *helpRowView {
	v := &helpRowView{idx: idx, cmd: cmd, desc: desc, selected: selected, onFocus: onFocus, onAccept: onAccept}
	v.el = v.buildEl()
	return v
}

func (v *helpRowView) buildEl() *gotui.Element {
	bg := gotui.Style{}
	if v.selected {
		bg = selBgStyle
	}
	row := gotui.New(
		gotui.WithDisplay(gotui.DisplayFlex),
		gotui.WithDirection(gotui.Row),
		gotui.WithHeight(1),
		gotui.WithBackground(bg),
		// WithOnFocus 自动置 focusable+tabStop；选中态经回调同步（见上）
		gotui.WithOnFocus(func(*gotui.Element) { v.onFocus() }),
		gotui.WithOnActivate(v.onAccept),
	)
	row.AddChild(gotui.New(
		gotui.WithText(v.cmd),
		gotui.WithWidth(16),
		gotui.WithHeight(1),
		gotui.WithTruncate(true),
		gotui.WithTextStyle(cmdStyle),
	))
	row.AddChild(gotui.New(
		gotui.WithText(v.desc),
		gotui.WithWidth(46),
		gotui.WithHeight(1),
		gotui.WithTruncate(true),
		gotui.WithTextStyle(subStyle),
	))
	return row
}

func (v *helpRowView) Render(app *gotui.App) *gotui.Element { return v.el }

func (v *helpRowView) UpdateProps(fresh gotui.Component) {
	f, ok := fresh.(*helpRowView)
	if !ok {
		return
	}
	v.idx, v.cmd, v.desc, v.selected = f.idx, f.cmd, f.desc, f.selected
	v.onFocus, v.onAccept = f.onFocus, f.onAccept
	v.el = v.buildEl()
}
