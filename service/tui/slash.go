package tui

// 斜杠命令注册表：命令语义与维护都在前端，后端只暴露能力接口（引擎侧
// 明确不处理斜杠）。命令、浮层文案、执行体绑在同一条记录里——分成两份
// 清单会出现「浮层能选但派发不认识」的静默错配。

import (
	tea "charm.land/bubbletea/v2"
)

// slashCmd 注册表的一条记录。run 返回 tea.Cmd（/exit 需要 tea.Quit），
// 无需返回值的命令返回 nil。
type slashCmd struct {
	cmd  string
	desc string
	run  func(t *TUI) tea.Cmd
}

// slashRegistry 内置斜杠命令，顺序即浮层展示顺序。
// NewConversation/ClearRecords 在 running 时拒绝并自行 setNotice，前端不看返回值。
var slashRegistry = []slashCmd{
	{"/new", "start a new conversation", func(t *TUI) tea.Cmd {
		t.engine.NewConversation()
		return nil
	}},
	{"/clear", "clear the screen", func(t *TUI) tea.Cmd {
		t.engine.ClearRecords()
		return nil
	}},
	{"/resume", "resume an existing session", func(t *TUI) tea.Cmd {
		t.openResume()
		return nil
	}},
	{"/exit", "exit HyperBot", func(t *TUI) tea.Cmd {
		t.engine.Shutdown()
		return tea.Quit
	}},
}

// dispatchSlash 在注册表里查命令并执行；handled=false 表示不是内置命令，
// 由调用方按普通输入兜底提交给引擎（引擎技能就是这样工作的）。
func (t *TUI) dispatchSlash(input string) (tea.Cmd, bool) {
	for _, sc := range slashRegistry {
		if sc.cmd == input {
			return sc.run(t), true
		}
	}
	return nil, false
}

// builtinSlashItems 注册表在浮层里的投影（每次新建切片，调用方可安全 append）。
func builtinSlashItems() []slashItem {
	items := make([]slashItem, 0, len(slashRegistry))
	for _, sc := range slashRegistry {
		items = append(items, slashItem{Cmd: sc.cmd, Desc: sc.desc})
	}
	return items
}

// fetchSlashItems 浮层候选全量：内置命令在前，引擎技能目录在后（拉取式
// 天然拿到最新版）。跨界拉取只在 engine.go，这里只负责合并。
func (t *TUI) fetchSlashItems() []slashItem {
	return append(builtinSlashItems(), t.fetchSkillItems()...)
}
