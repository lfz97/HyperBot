package main

// widthcal 终端宽度校准工具：诊断 TUI 表格错位的根因。
//
// 用法：go run ./cmd/widthcal
// 在运行 HyperBot TUI 的同一个终端里执行。
//
// 原理：每行内容后面补空格，把结束标记「│←30」钉在【按 x/ansi 测量的
// 第 30 列】——与 TUI 排版用的是同一套测量。若终端实际渲染宽度一致，
// 内容行尾应恰好顶到标记；若内容【没到】标记 = 终端渲得比测量窄，
// 【越过】标记 = 渲得比测量宽。含该字符的表格行就会整体偏移错位。

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
)

const target = 30

func cal(label, s string) {
	w := ansi.StringWidth(s)
	pad := target - w
	if pad < 1 {
		pad = 1
	}
	fmt.Printf("[%-4s] %s%s│← 第30列(按测量)\n", label, s, strings.Repeat(" ", pad))
}

func main() {
	fmt.Println("=== 终端宽度校准（在跑 TUI 的同一终端里执行）===")
	fmt.Println("内容行尾应顶到「│←」标记：没到=终端渲窄了，越过=渲宽了")
	fmt.Println()

	cal("基准", strings.Repeat("0123456789", 3))            // ASCII 30
	cal("中文", strings.Repeat("字", 15))                    // CJK×15 = 30
	cal("全角", strings.Repeat("？", 15))                    // 全角标点×15 = 30
	cal("对勾", strings.Repeat("✅", 15))                    // emoji×15 = 30
	cal("警告", strings.Repeat("⚠️", 15))                   // 带变体选择符×15 = 30
	cal("中点", strings.Repeat("·", 30))                     // 歧义宽计1×30 = 30
	cal("省略", strings.Repeat("…", 30))                     // 同上
	cal("箭头", strings.Repeat("→", 30))                     // 同上
	cal("框线", strings.Repeat("─", 30))                     // 同上
	cal("混排", "检查项：Linux / amd64，用户 lfz（测试）") // 真实形态混合

	fmt.Println()
	fmt.Println("=== 真实表格复现（glamour 同款配置）===")
	fmt.Println("（✅ 行与 OK 行的竖线应同列；错开=✅ 渲宽分歧）")
	fmt.Println()

	tbl := table.New().
		Width(116).
		Wrap(true).
		StyleFunc(func(_, col int) lipgloss.Style {
			return lipgloss.NewStyle().Inline(false).Margin(0, 1)
		}).
		Border(lipgloss.NormalBorder()).
		BorderTop(false).BorderLeft(false).BorderRight(false).BorderBottom(false).
		Headers("检查项", "状态", "结果").
		Row("运行环境", "✅", "Linux / amd64，用户 lfz，当前目录 /home/lfz/hyperbot").
		Row("系统时间", "✅", "2026-10-13 19:03（与日志时间戳一致）").
		Row("文件系统", "OK", "配置目录完整（hyperbot.yaml、skills、日志、memory.db）")
	fmt.Println(tbl.String())
}
