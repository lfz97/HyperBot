package tui

import (
	"encoding/json"

	"HyperBot/utils/pretty"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// toggleHelpPage 切换帮助页显示/隐藏。
// 只会在 UI goroutine 上被调用（来自 inputArea / helpTable 的输入捕获器）。
func (t *Tui) toggleHelpPage() {
	ht := t.appLayout.helpTable
	if ht.helpPageVisible {
		t.app.SetRoot(t.appLayout.pages, true)
		t.app.SetFocus(t.appLayout.inputArea)
		ht.helpPageVisible = false
	} else {
		t.refreshhelpTable() // 每次打开时刷新，确保 skills 等动态项可见
		flex := tview.NewFlex().SetDirection(tview.FlexRow)
		flex.SetBackgroundColor(bg)
		flex.AddItem(ht.h, 0, 1, true)
		// SetRoot 内部会 SetFocus(root)，Flex.Focus 再委派给上面 AddItem 时 focus=true
		// 的 table，所以 helpTable 的 Esc/Ctrl+K 捕获器能正常收到按键
		t.app.SetRoot(flex, true)
		ht.helpPageVisible = true
	}
}

func (t *Tui) refreshhelpTable() {
	ht := t.appLayout.helpTable
	ht.h.Clear()

	// 默认项 TUI 自持；技能项每次打开时从引擎状态拉取——loadSkills 在 init/refresh
	// 序列里随时可能重建列表，拉取式天然拿到最新版，也省掉了旧的并发写锁。
	items := []helpItem{
		{cmd: "/new", desc: "开始新对话"},
		{cmd: "/exit", desc: "退出程序"},
	}
	var ws []wireHelpItem
	_ = json.Unmarshal([]byte(t.engine.HelpItemsJSON()), &ws)
	for _, it := range ws {
		items = append(items, helpItem{cmd: it.Cmd, desc: it.Desc})
	}

	mainColor := tcell.GetColor(pretty.TuiMainText)
	subColor := tcell.GetColor(pretty.TuiSubText)

	for index, item := range items {
		cmdCell := tview.NewTableCell(item.cmd).
			SetTextColor(mainColor).
			SetAlign(tview.AlignLeft).
			SetExpansion(0)

		descCell := tview.NewTableCell(item.desc).
			SetTextColor(subColor).
			SetAlign(tview.AlignLeft).
			SetExpansion(1)

		ht.h.SetCell(index, 0, cmdCell)
		ht.h.SetCell(index, 1, descCell)
	}
}
