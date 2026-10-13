package tui

// 工具块渲染（设计文档：docs/superpowers/specs/2026-10-13-tui-tool-block-rendering-design.md）。
// 纯函数模块：composeView 每帧全量重放，无状态、无副作用。
//
// 数据流：args/result JSON → ParamMapper 字段提取（无登记走兜底）→
// 参数渲染 key value 行、结果按内容类型路由（diff 双色块 / 多行文本块 / 标量行）。

import (
	"encoding/json"
	"fmt"
	"image/color"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	tools "HyperBot/service/engine/tools"
)

const (
	argValueMaxRunes = 40 // 参数/标量值单行截断
	blockMaxLines    = 10 // diff/长文本默认显示行数
)

// codeArgFields 工具参数中的"代码内容"字段：渲染为高亮代码块而非截断值行
// （write_file 的写入内容）。渲染知识留在 UI 层（同 crush per-tool
// renderer 思路），工具本身零改动。
var codeArgFields = map[string]string{
	"write_file": "Content",
}

// argPath 工具调用参数中的目标文件路径（选 lexer 用）。
func argPath(obj map[string]any) string {
	for _, k := range []string{"Path", "PathA", "File", "FilePath"} {
		if s, ok := obj[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

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

// rowsFromKeys 从 JSON 对象按键序提取键值行（值折叠多行、截断；空字符串值
// 跳过——传了但为空的字段（如 Id:""）显示成空尾巴是噪音）。
func rowsFromKeys(obj map[string]any, keys []string) []kvRow {
	var rows []kvRow
	for _, k := range keys {
		v, ok := obj[k]
		if !ok || v == nil {
			continue
		}
		s := truncateRunes(compactLine(stringValue(v)), argValueMaxRunes)
		if s == "" {
			continue
		}
		rows = append(rows, kvRow{key: k, val: s})
	}
	return rows
}

// orderedKeys 键序：有 mapper 登记按登记顺序（登记为空切片则无行），
// 无登记（MCP/未知工具）字典序兜底；纯数字键（search_in_file 的 "0"~"N"）
// 按数值排序——字典序会把 "10" 排在 "2" 前面。
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
	if allDigits(keys) {
		sort.Slice(keys, func(i, j int) bool {
			a, _ := strconv.Atoi(keys[i])
			b, _ := strconv.Atoi(keys[j])
			return a < b
		})
	}
	return keys
}

// allDigits 全部键均为纯数字（≥1 个）。
func allDigits(keys []string) bool {
	if len(keys) == 0 {
		return false
	}
	for _, k := range keys {
		if _, err := strconv.Atoi(k); err != nil {
			return false
		}
	}
	return true
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

// ── 结果路由 ────────────────────────────────────────────

// resultParts 结果解析：JSON 对象按 mapper.Out 顺序提取字段——标量成 ↪ 行、
// 多行内容成块；非对象结果（JSON 字符串或原文）整体按内容类型路由。
// 空/()/{} 无输出。
func resultParts(name string, result string) ([]kvRow, []textBlock) {
	s := strings.TrimSpace(result)
	if s == "" || s == "{}" || s == "()" {
		return nil, nil
	}
	if obj, ok := decodeJSONObject([]byte(s)); ok {
		var rows []kvRow
		var blocks []textBlock
		for _, k := range orderedKeys(name, obj, false) {
			v, exists := obj[k]
			if !exists || v == nil {
				continue
			}
			str := stringValue(v)
			if expanded, ok := expandJSONList(str); ok {
				// 值本身是 JSON 数组（ls/glob 的"JSON 套 JSON"形态）：展开成
				// 每元素一行，走多行块渲染
				blocks = append(blocks, textBlock{text: expanded})
				continue
			}
			if isMultiline(str) {
				blocks = append(blocks, textBlock{text: str, isDiff: isDiff(str)})
			} else {
				s := truncateRunes(compactLine(str), argValueMaxRunes)
				if s != "" { // 空字符串值（如 Error:""）不出行
					rows = append(rows, kvRow{key: k, val: s})
				}
			}
		}
		return rows, blocks
	}
	// 非对象：JSON 字符串解开引号，否则当原文
	var str string
	if err := json.Unmarshal([]byte(s), &str); err != nil {
		str = s
	}
	if isMultiline(str) {
		return nil, []textBlock{{text: str, isDiff: isDiff(str)}}
	}
	return []kvRow{{key: "result", val: truncateRunes(compactLine(str), argValueMaxRunes)}}, nil
}

// isMultiline 清洗后含真实换行（≥2 行）即多行内容。
func isMultiline(s string) bool {
	return strings.Contains(strings.TrimSpace(s), "\n")
}

// isDiff 统一 diff 识别：首行 --- / diff --git / @@ 起头。
func isDiff(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "--- ") ||
		strings.HasPrefix(t, "diff --git") ||
		strings.HasPrefix(t, "@@ ")
}

// expandJSONList 结果值本身是 JSON 数组字符串（工具返回 map[string]string 装
// 序列化好的列表——ls 的 Files、glob 的 Matches）时展开成每元素一行的文本：
// 标量元素取裸值（glob 的路径列表去引号），其余紧凑序列化（ls 的文件对象）。
// 非数组/空数组/解析失败返回 false，走原渲染路径。
func expandJSONList(s string) (string, bool) {
	t := strings.TrimSpace(s)
	if len(t) < 2 || t[0] != '[' {
		return "", false
	}
	var arr []any
	if err := json.Unmarshal([]byte(t), &arr); err != nil || len(arr) == 0 {
		return "", false
	}
	parts := make([]string, 0, len(arr))
	for _, e := range arr {
		if str, ok := e.(string); ok {
			parts = append(parts, str)
		} else if b, err := json.Marshal(e); err == nil {
			parts = append(parts, string(b))
		} else {
			return "", false
		}
	}
	return strings.Join(parts, "\n"), true
}

// diffLineKind diff 行分类：add(+)/del(-)/ctx(其余，含 --- +++ @@ 与上下文)。
func diffLineKind(l string) string {
	switch {
	case strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++"):
		return "add"
	case strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---"):
		return "del"
	default:
		return "ctx"
	}
}

// cleanBlock 多行内容清洗：剥 ANSI 色码（PTY 输出防串色）、\r\n 归一、去首尾
// 空白。与 compactLine 的区别：保留换行结构。
func cleanBlock(s string) string {
	s = ansi.Strip(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "")
	return strings.TrimSpace(s)
}

// ── 组装与块渲染 ────────────────────────────────────────

// toolView 工具块总装：头部行（✓ name）→ 参数字段行（most-subtle，最暗）→
// 空行分段 → 结果标量行 + 多行内容块（muted，亮一档）。参数与结果行同列
// 对齐，仅靠空行与明暗区分段。w 为块可用总宽（msgIndent 的缩进已扣）。
func toolView(name string, args []byte, result string, w int) string {
	avail := w - msgIndentW - 4 // 扣 msgIndent(2) 与块内容 4 空格缩进
	if avail < 10 {
		avail = 10
	}

	argsObj, _ := decodeJSONObject(args)
	path := argPath(argsObj)

	argRowList := argRows(name, args)
	resRows, blocks := resultParts(name, result)

	// 代码内容参数（write_file 的写入内容）：从参数行里摘出来，渲染为
	// 高亮代码块——40 rune 截断值行对代码内容没有意义。
	var codeLines []string
	codeField := codeArgFields[name]
	if codeField != "" {
		if content, isStr := argsObj[codeField].(string); isStr && strings.TrimSpace(content) != "" {
			rows := argRowList[:0]
			for _, r := range argRowList {
				if r.key != codeField {
					rows = append(rows, r)
				}
			}
			argRowList = rows
			codeLines = codeBlockLines(path, content, avail)
		}
	}

	all := make([]kvRow, 0, len(argRowList)+len(resRows))
	all = append(all, argRowList...)
	all = append(all, resRows...)
	if len(all) == 0 && len(blocks) == 0 && len(codeLines) == 0 {
		return toolHeader(name, "")
	}

	kw := 0
	for _, r := range all {
		if n := len([]rune(r.key)); n > kw {
			kw = n
		}
	}

	lines := []string{toolHeader(name, "")}
	for _, r := range argRowList {
		lines = append(lines, kvRowLines(r, kw, avail, cFgMostSubtle)...)
	}
	// 代码块属于参数区（展示"写了什么"）：与参数行之间空行分隔
	if len(codeLines) > 0 && len(argRowList) > 0 {
		lines = append(lines, "")
	}
	lines = append(lines, codeLines...)
	// 空行仅在两段都存在时出现（无参工具不凭空多空行）
	hasInput := len(argRowList) > 0 || len(codeLines) > 0
	if hasInput && (len(resRows) > 0 || len(blocks) > 0) {
		lines = append(lines, "")
	}
	for _, r := range resRows {
		lines = append(lines, kvRowLines(r, kw, avail, cFgMuted)...)
	}
	for _, b := range blocks {
		lines = append(lines, renderBlock(b, avail, path)...)
	}
	return strings.Join(lines, "\n")
}

// kvRowLines 键值行：值超宽按列硬折行（续行对齐 value 起始列，不越过
// padding），整行统一配色。值无 ANSI（已截断的纯文本），Hardwrap 安全。
func kvRowLines(r kvRow, kw, avail int, c color.Color) []string {
	prefix := "    " + padRight(r.key, kw) + "  "
	limit := avail - len(prefix) // prefix 全为窄字符，字节数即显示列数
	if limit < 4 {
		limit = 4
	}
	chunks := hardWrapIf(r.val, limit)
	lines := make([]string, len(chunks))
	for i, chunk := range chunks {
		p := prefix
		if i > 0 {
			p = strings.Repeat(" ", len(prefix))
		}
		lines[i] = colorText(c, p+chunk)
	}
	return lines
}

// renderBlock 多行内容块：每行 4 空格缩进、muted 色；diff 块分流到
// renderDiffBlock（行号列 + 行内语法高亮）；行内容按 avail 硬折行；
// 超过 blockMaxLines 截断并加最暗色尾注。path 为编辑目标文件路径（diff
// 块选 lexer 用，空则不高亮）。
func renderBlock(b textBlock, avail int, path string) []string {
	if b.isDiff {
		return renderDiffBlock(b, path, avail)
	}
	if avail < 10 {
		avail = 10
	}
	all := strings.Split(cleanBlock(b.text), "\n")
	total := len(all)
	if total > blockMaxLines {
		all = all[:blockMaxLines]
	}
	out := make([]string, 0, len(all)+1)
	for _, l := range all {
		for _, chunk := range hardWrapIf(l, avail) {
			out = append(out, "    "+colorText(cFgMuted, chunk))
		}
	}
	if total > blockMaxLines {
		note := fmt.Sprintf("… 共 %d 行，显示前 %d 行", total, blockMaxLines)
		for _, chunk := range hardWrapIf(note, avail) {
			out = append(out, colorText(cFgMostSubtle, "    "+chunk))
		}
	}
	return out
}

// diffHunkHeader 匹配 unified diff hunk 头，提取 old/new 起始行号。
var diffHunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// renderDiffBlock unified diff 块：行号列（@@ 头解析并逐行推进，add/del
// 行各自只占一侧）+ 行内语法高亮（lexer 取自 path）+ +/- 前缀染色。
// 语法色与 diff 色分层：前缀承载 +/- 红绿，内容承载 chroma 语法色。
func renderDiffBlock(b textBlock, path string, avail int) []string {
	if avail < 10 {
		avail = 10
	}
	all := strings.Split(cleanBlock(b.text), "\n")
	total := len(all)
	if total > blockMaxLines {
		all = all[:blockMaxLines]
	}

	// 第一遍：推进行号，算 old/new 列宽
	oldLine, newLine := 0, 0
	maxOld, maxNew := 0, 0
	for _, l := range all {
		if m := diffHunkHeader.FindStringSubmatch(l); m != nil {
			oldLine, _ = strconv.Atoi(m[1])
			newLine, _ = strconv.Atoi(m[2])
			continue
		}
		switch {
		case strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---"):
			if oldLine > maxOld {
				maxOld = oldLine
			}
			oldLine++
		case strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++"):
			if newLine > maxNew {
				maxNew = newLine
			}
			newLine++
		case strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\\"):
			if oldLine > maxOld {
				maxOld = oldLine
			}
			if newLine > maxNew {
				maxNew = newLine
			}
			oldLine++
			newLine++
		}
	}
	oldW := len(fmt.Sprintf("%d", max(maxOld, 1)))
	newW := len(fmt.Sprintf("%d", max(maxNew, 1)))
	numCol := oldW + 1 + newW + 1

	// 第二遍：渲染
	oldLine, newLine = 0, 0
	renderNums := func(o, n int) string {
		blank := strings.Repeat(" ", oldW+1+newW+1)
		if o == 0 && n == 0 {
			return colorText(cFgMostSubtle, blank)
		}
		var ob, nb string
		if o > 0 {
			ob = fmt.Sprintf("%*d", oldW, o)
		} else {
			ob = strings.Repeat(" ", oldW)
		}
		if n > 0 {
			nb = fmt.Sprintf("%*d", newW, n)
		} else {
			nb = strings.Repeat(" ", newW)
		}
		return colorText(cFgMostSubtle, ob+" "+nb+" ")
	}

	limit := avail - numCol - 2
	if limit < 4 {
		limit = 4
	}
	out := make([]string, 0, len(all)+1)
	for _, l := range all {
		if m := diffHunkHeader.FindStringSubmatch(l); m != nil {
			oldLine, _ = strconv.Atoi(m[1])
			newLine, _ = strconv.Atoi(m[2])
			out = append(out, renderNums(0, 0)+colorText(cFgMostSubtle, l))
			continue
		}
		var (
			o, n        int
			prefix      string
			kind        string
			content     string
		)
		switch {
		case strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---"):
			o = oldLine
			oldLine++
			prefix, kind, content = "-", "del", l[1:]
		case strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++"):
			n = newLine
			newLine++
			prefix, kind, content = "+", "add", l[1:]
		case strings.HasPrefix(l, " "):
			o, n = oldLine, newLine
			oldLine++
			newLine++
			prefix, kind, content = " ", "ctx", l[1:]
		default: // ---/+++/其他头行
			out = append(out, renderNums(0, 0)+colorText(cFgMostSubtle, l))
			continue
		}
		if path == "" {
			// 无文件上下文（如 diff 工具的裸 diff）：整行按 kind 染色，
			// 折行后每个 chunk 独立染色（kind 贯穿整逻辑行）
			lineColor := diffKindColor(kind)
			for i, chunk := range hardWrapIf(prefix+content, limit) {
				if i == 0 {
					out = append(out, renderNums(o, n)+colorText(lineColor, chunk))
				} else {
					out = append(out, colorText(cFgMostSubtle, strings.Repeat(" ", numCol))+colorText(lineColor, chunk))
				}
			}
			continue
		}
		// 有文件路径：语法色打底、+/- 前缀承载 diff kind（crush 分层同型）
		hl := highlightCode(content, path)
		chunks := hardWrapIf(hl, limit)
		for i, chunk := range chunks {
			if i == 0 {
				out = append(out, renderNums(o, n)+colorText(diffKindColor(kind), prefix+" ")+chunk)
			} else {
				out = append(out, colorText(cFgMostSubtle, strings.Repeat(" ", numCol+2))+chunk)
			}
		}
	}
	if total > blockMaxLines {
		note := fmt.Sprintf("… 共 %d 行，显示前 %d 行", total, blockMaxLines)
		for _, chunk := range hardWrapIf(note, limit) {
			out = append(out, colorText(cFgMostSubtle, strings.Repeat(" ", numCol+2)+chunk))
		}
	}
	return out
}

// diffKindColor diff 行分类配色。
func diffKindColor(kind string) color.Color {
	switch kind {
	case "add":
		return cSuccessSubtle
	case "del":
		return cError
	default:
		return cFgMuted
	}
}

// hardWrapIf 超宽才按列硬折行（ANSI 感知、保显示列对齐），否则原样单行。
func hardWrapIf(s string, limit int) []string {
	if limit < 1 || ansi.StringWidth(s) <= limit {
		return []string{s}
	}
	return strings.Split(ansi.Hardwrap(s, limit, false), "\n")
}
