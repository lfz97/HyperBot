package tui

// 工具块渲染（设计文档：docs/superpowers/specs/2026-10-13-tui-tool-block-rendering-design.md）。
// 纯函数模块：composeView 每帧全量重放，无状态、无副作用。
//
// 数据流：args/result JSON → ParamMapper 字段提取（无登记走兜底）→
// 参数渲染 key value 行、结果按内容类型路由（diff 双色块 / 多行文本块 / 标量行）。

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	tools "HyperBot/service/engine/tools"
)

const (
	argValueMaxRunes = 40 // 参数/标量值单行截断
	blockMaxLines    = 10 // diff/长文本默认显示行数
)

// paramMappers 工具名 → 字段登记，包级一次性索引（查询 O(1)）。
// 测试可整体替换后恢复。
var paramMappers = indexMappers(tools.GetParamMapper())

// indexMappers 线性登记表建按名索引。
func indexMappers(ms *tools.Mappers) map[string]*tools.ParamMapper {
	idx := make(map[string]*tools.ParamMapper, len(*ms))
	for _, m := range *ms {
		idx[m.Name] = m
	}
	return idx
}

// kvRow 工具块内的一行键值（参数字段或结果标量字段）。
type kvRow struct{ key, val string }

// textBlock 结果里的多行内容块（diff 或长文本）。
type textBlock struct {
	text   string
	isDiff bool
}

// ── 参数行 ──────────────────────────────────────────────

// argRows 参数字段行：按 mapper.In 登记顺序提取；无登记遍历顶层键（字典序）；
// JSON 数组显示项数（todo_write 的整份清单）；非法 JSON 退化为原文单行截断。
func argRows(name string, args []byte) []kvRow {
	if len(args) == 0 {
		return nil
	}
	if obj, ok := decodeJSONObject(args); ok {
		return rowsFromKeys(obj, orderedKeys(name, obj, true))
	}
	if n, isArray := decodeJSONArray(args); isArray {
		return []kvRow{{key: "args", val: fmt.Sprintf("共 %d 项", n)}}
	}
	// 非法 JSON：截断原文兜底，宁可朴素不可消失
	s := truncateRunes(compactLine(string(args)), argValueMaxRunes)
	if s == "" || s == "{}" || s == "()" {
		return nil
	}
	return []kvRow{{key: "args", val: s}}
}

// rowsFromKeys 从 JSON 对象按键序提取键值行（值折叠多行、截断）。
func rowsFromKeys(obj map[string]any, keys []string) []kvRow {
	var rows []kvRow
	for _, k := range keys {
		v, ok := obj[k]
		if !ok || v == nil {
			continue
		}
		rows = append(rows, kvRow{
			key: k,
			val: truncateRunes(compactLine(stringValue(v)), argValueMaxRunes),
		})
	}
	return rows
}

// orderedKeys 键序：有 mapper 登记按登记顺序（登记为空切片则无行），
// 无登记（MCP/未知工具）字典序兜底。
func orderedKeys(name string, obj map[string]any, in bool) []string {
	var registered []string
	if m := paramMappers[name]; m != nil {
		if in {
			registered = m.In
		} else {
			registered = m.Out
		}
	}
	if registered != nil {
		return registered
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ── 值与 JSON 工具函数 ───────────────────────────────────

// stringValue 标量值转显示文本：字符串原样，其余紧凑 JSON。
func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// decodeJSONObject 严格解析 JSON 对象（空输入/非对象/非法返回 false）。
func decodeJSONObject(data []byte) (map[string]any, bool) {
	if len(data) == 0 {
		return nil, false
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		return nil, false
	}
	return obj, true
}

// decodeJSONArray 解析 JSON 数组，返回元素数与是否为数组。
func decodeJSONArray(data []byte) (int, bool) {
	var arr []any
	if err := json.Unmarshal(data, &arr); err != nil {
		return 0, false
	}
	return len(arr), true
}

// truncateRunes 按 rune 截断（多字节安全），截断时追加省略号。
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// padRight rune 宽度右填充（key 列对齐）。
func padRight(s string, w int) string {
	if gap := w - len([]rune(s)); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}
