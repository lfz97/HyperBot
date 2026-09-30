package tui

import (
	"testing"
)

// todoLineStyle 的标记是多字节 rune（"◐ " 共 4 字节），历史实现按字节切片
// 导致两个彩色分支永远匹配不上、全部回落暗灰。用回归测试钉住：
// 彩色分支必须命中（区别于兜底的 subStyle）。
func TestTodoLineStyle(t *testing.T) {
	if todoLineStyle("◐ 写代码") == subStyle {
		t.Error("进行中行（◐）应上色，不应回落暗灰")
	}
	if todoLineStyle("☐ 待办项") == subStyle {
		t.Error("待办行（☐）应上正文色，不应回落暗灰")
	}
	if todoLineStyle("✓ 已完成") != subStyle {
		t.Error("无标记行应回落暗灰")
	}
}
