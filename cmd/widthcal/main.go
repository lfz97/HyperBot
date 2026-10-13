package main

// widthcal 终端宽度校准工具：诊断 TUI 表格错位的根因。
//
// 用法：go run ./cmd/widthcal
// 在运行 HyperBot TUI 的同一个终端里执行，观察每行行尾是否与标注的
// "应到列位"竖线对齐。哪一行偏了，哪个字符就是宽度分歧源。
//
// 背景：TUI 的所有排版（表格列宽/折行/对齐）按 x/ansi 的宽度测量计算
// （CJK/emoji=2 列，歧义宽字符=1 列）。若终端实际渲染宽度与测量不一致
// （字体 fallback、emoji 渲染、歧义宽模式），含该字符的行会整体偏移——
// 表格竖线/交界点就错位。本工具逐字符验证。

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
)

func main() {
	const N = 30 // 每行目标列数

	fmt.Println("=== 终端宽度校准（在跑 TUI 的同一终端里执行）===")
	fmt.Println("每行行尾应与『应到30列』的竖线对齐；偏了 = 该字符是宽度分歧源")
	fmt.Println()

	row := func(label, s string) {
		fmt.Printf("[%s] %s%s 应到30列│\n", label, s, strings.Repeat(" ", max(0, 0)))
	}
	_ = row

	// 基准：30 列标尺
	fmt.Printf("[基准] %s 应到30列│\n", strings.Repeat("0123456789", 3))
	// CJK：每字 2 列，15 字 = 30
	fmt.Printf("[中文] %s 应到30列│\n", strings.Repeat("一二三四五六七八九十一二三四五", 1))
	// 全角标点：每字 2 列，15 字 = 30
	fmt.Printf("[全角] %s 应到30列│\n", "？？？？？？？？？？？？？？？？？")
	// emoji：x/ansi 计 2 列，10 个 = 20？不——✅ 按 2 列算：15 个 = 30
	fmt.Printf("[对勾] %s 应到30列│\n", strings.Repeat("✅", 15))
	fmt.Printf("[叉号] %s 应到30列│\n", strings.Repeat("❌", 15))
	fmt.Printf("[警告] %s 应到30列│(带变体选择符，x/ansi 计 2)\n", strings.Repeat("⚠️", 15))
	fmt.Printf("[信息] %s 应到30列│\n", strings.Repeat("ℹ️", 15))
	// 歧义宽字符：x/ansi 计 1 列，30 个 = 30（若终端按 2 列渲染会到 60）
	fmt.Printf("[中点] %s 应到30列│\n", strings.Repeat("·", 30))
	fmt.Printf("[省略] %s 应到30列│\n", strings.Repeat("…", 30))
	fmt.Printf("[长划] %s 应到30列│\n", strings.Repeat("—", 30))
	fmt.Printf("[箭头] %s 应到30列│\n", strings.Repeat("→", 30))
	fmt.Printf("[框线] %s 应到30列│\n", strings.Repeat("─│", 15))

	fmt.Println()
	fmt.Println("=== 真实表格复现（glamour 同款配置：Margin(0,1) + NormalBorder 无外框 + Width 116）===")
	fmt.Println()

	// 与 glamour ansi/table.go setStyles/setBorders 同款配置
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

	fmt.Println("判定：")
	fmt.Println("- 上表 ✅ 行与 OK 行的竖线位置不同 → 终端对 ✅ 的渲染宽度 ≠ 2 列（emoji 分歧）")
	fmt.Println("- [对勾]/[警告] 行尾没到 30 列 → 同上")
	fmt.Println("- [中点]/[省略]/[箭头] 行尾超到 60 列 → 终端开了歧义宽=2 模式")
	fmt.Println("- 全部对齐 → 宽度不是问题，把上表复制发我继续查")
}
