package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// helpBoxWidth 帮助浮层宽度（内容列数由 %-12s 格式决定，44 列足够指令+描述）。
const helpBoxWidth = 44

// helps 帮助浮层（demo 同款：居中圆角盒、上下选择、enter 插入指令）。
// 默认项 TUI 自持；技能项每次打开时从引擎状态拉取——loadSkills 在 init/refresh
// 序列里随时可能重建列表，拉取式天然拿到最新版。
type helps struct {
	items   [][2]string
	index   int
	visible bool
}

func (h *helps) helpContent() string {
	var b strings.Builder
	for i, v := range h.items {
		line := fmt.Sprintf("%-12s %s", v[0], v[1])
		if i == h.index { //如果index指向了这个索引值，说明是被用户选中了，加一个箭头标识
			line = "▶ " + lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Bold(true).Render(line) + "\n" //换行不要进render，render后再加
		} else { //未选中的，前面加个空占位符，防止排版错乱
			line = "  " + line + "\n"
		}
		fmt.Fprint(&b, line)
	}
	return strings.TrimRight("Key Bindings\n\n"+b.String(), "\n")
}

func (h *helps) isVisible() bool {
	return h.visible
}

func (h *helps) toggleVisibility() {
	h.visible = !h.visible
}

func (h *helps) moveUp() {
	h.index = (h.index - 1 + len(h.items)) % len(h.items)
}

func (h *helps) moveDown() {
	h.index = (h.index + 1) % len(h.items)
}

// refresh 重建帮助条目：默认项 + 引擎拉取的技能项（每次打开时调用）。
func (h *helps) refresh(e EngineView) {
	items := [][2]string{
		{"ctrl+k", "显示/关闭帮助"},
		{"/new", "开始新对话"},
		{"/exit", "退出程序"},
	}
	var ws []wireHelpItem
	if err := json.Unmarshal([]byte(e.HelpItemsJSON()), &ws); err == nil {
		for _, it := range ws {
			items = append(items, [2]string{it.Cmd, it.Desc})
		}
	}
	h.items = items
	h.index = 0
}
