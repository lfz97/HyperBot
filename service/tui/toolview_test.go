package tui

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	tools "HyperBot/service/engine/tools"
)

// withMappers 测试期替换包级 mapper 索引，结束后恢复（隔离测试）。
func withMappers(t *testing.T, m map[string]*tools.ParamMapper) {
	t.Helper()
	orig := paramMappers
	paramMappers = m
	t.Cleanup(func() { paramMappers = orig })
}

func TestArgRowsMapperOrder(t *testing.T) {
	withMappers(t, map[string]*tools.ParamMapper{
		"edit_file": {Name: "edit_file", In: []string{"Path", "Regex"}, Out: []string{"Diff"}},
	})
	rows := argRows("edit_file", []byte(`{"Regex":"foo","Path":"a.go"}`))
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d: %#v", len(rows), rows)
	}
	if rows[0].key != "Path" || rows[0].val != "a.go" {
		t.Errorf("row0 = %q %q, want Path a.go", rows[0].key, rows[0].val)
	}
	if rows[1].key != "Regex" || rows[1].val != "foo" {
		t.Errorf("row1 = %q %q, want Regex foo", rows[1].key, rows[1].val)
	}
}

func TestArgRowsFallbackSortedKeys(t *testing.T) {
	withMappers(t, map[string]*tools.ParamMapper{})
	rows := argRows("mcp_unknown", []byte(`{"b":"2","a":"1"}`))
	if len(rows) != 2 || rows[0].key != "a" || rows[1].key != "b" {
		t.Fatalf("want sorted keys a,b, got %#v", rows)
	}
}

func TestArgRowsInvalidJSONFallsBackToRaw(t *testing.T) {
	withMappers(t, map[string]*tools.ParamMapper{})
	rows := argRows("mcp_unknown", []byte(`not json`))
	if len(rows) != 1 || rows[0].key != "args" || rows[0].val != "not json" {
		t.Fatalf("want raw fallback row, got %#v", rows)
	}
}

func TestArgRowsArrayCount(t *testing.T) {
	withMappers(t, map[string]*tools.ParamMapper{
		"todo_write": {Name: "todo_write", In: []string{}, Out: []string{"message"}},
	})
	rows := argRows("todo_write", []byte(`[{"id":1},{"id":2},{"id":3}]`))
	if len(rows) != 1 || rows[0].val != "共 3 项" {
		t.Fatalf("want array count row, got %#v", rows)
	}
}

func TestArgRowsEmptyAndTruncate(t *testing.T) {
	withMappers(t, map[string]*tools.ParamMapper{})
	if rows := argRows("x", []byte(`{}`)); len(rows) != 0 {
		t.Fatalf("want no rows for {}, got %#v", rows)
	}
	if rows := argRows("x", nil); len(rows) != 0 {
		t.Fatalf("want no rows for nil, got %#v", rows)
	}
	rows := argRows("x", []byte(`{"k":"`+strings.Repeat("一", 45)+`"}`))
	if len([]rune(rows[0].val)) > 41 { // 40 rune + 省略号
		t.Fatalf("value not truncated: %q", rows[0].val)
	}
	if !strings.HasSuffix(rows[0].val, "…") {
		t.Fatalf("want ellipsis suffix, got %q", rows[0].val)
	}
}

func TestStringValue(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{1234.0, "1234"},
		{true, "true"},
		{"s", "s"},
		{[]any{1.0, 2.0}, "[1,2]"},
	}
	for _, c := range cases {
		if got := stringValue(c.in); got != c.want {
			t.Errorf("stringValue(%#v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIndexMappers(t *testing.T) {
	ms := &tools.Mappers{}
	ms.AddMapping("a", []string{}, []string{})
	ms.AddMapping("b", []string{"X"}, []string{})
	idx := indexMappers(ms)
	if idx["a"] == nil || idx["b"] == nil || idx["b"].In[0] != "X" {
		t.Fatalf("index broken: %#v", idx)
	}
}

const testDiff = `--- a/x.go
+++ b/x.go
@@ -1,3 +1,4 @@
 ctx
-old
+new
+new2
`

func TestResultPartsDiffRouting(t *testing.T) {
	withMappers(t, map[string]*tools.ParamMapper{
		"edit_file": {Name: "edit_file", In: []string{"Path"}, Out: []string{"Diff"}},
	})
	rows, blocks := resultParts("edit_file", `{"Diff":`+strconv.Quote(testDiff)+`}`)
	if len(rows) != 0 || len(blocks) != 1 {
		t.Fatalf("want 0 rows 1 block, got %d rows %d blocks", len(rows), len(blocks))
	}
	if !blocks[0].isDiff {
		t.Fatalf("want diff block, got %#v", blocks[0])
	}
}

func TestResultPartsLongTextRouting(t *testing.T) {
	withMappers(t, map[string]*tools.ParamMapper{
		"exec": {Name: "exec", In: []string{"Cmd"}, Out: []string{"Output"}},
	})
	rows, blocks := resultParts("exec", `{"Output":"total 24\ndrwxr-xr-x 5\n-rw-r--r-- 1"}`)
	if len(rows) != 0 || len(blocks) != 1 || blocks[0].isDiff {
		t.Fatalf("want 1 text block, got rows=%#v blocks=%#v", rows, blocks)
	}
}

func TestResultPartsScalarRow(t *testing.T) {
	withMappers(t, map[string]*tools.ParamMapper{
		"write_file": {Name: "write_file", In: []string{"Path"}, Out: []string{"BytesWritten"}},
	})
	rows, blocks := resultParts("write_file", `{"BytesWritten":1234}`)
	if len(rows) != 1 || rows[0].key != "BytesWritten" || rows[0].val != "1234" || len(blocks) != 0 {
		t.Fatalf("want scalar row, got rows=%#v blocks=%#v", rows, blocks)
	}
}

func TestResultPartsNonObjectResult(t *testing.T) {
	withMappers(t, map[string]*tools.ParamMapper{})
	// JSON 字符串整体路由为内容块
	_, blocks := resultParts("raw", `"line1\nline2\nline3"`)
	if len(blocks) != 1 || blocks[0].isDiff {
		t.Fatalf("want text block, got %#v", blocks)
	}
	// 非法 JSON 原文兜底
	rows, _ := resultParts("raw", `plain result`)
	if len(rows) != 1 || rows[0].key != "result" || rows[0].val != "plain result" {
		t.Fatalf("want result fallback row, got %#v", rows)
	}
}

func TestResultPartsEmpty(t *testing.T) {
	withMappers(t, map[string]*tools.ParamMapper{})
	for _, s := range []string{"", "{}", "()", "   "} {
		if rows, blocks := resultParts("x", s); len(rows) != 0 || len(blocks) != 0 {
			t.Fatalf("want nothing for %q, got %#v %#v", s, rows, blocks)
		}
	}
}

func TestDiffLineKind(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"+new", "add"},
		{"+++ b/x.go", "ctx"},
		{"-old", "del"},
		{"--- a/x.go", "ctx"},
		{"@@ -1,3 +1,4 @@", "ctx"},
		{" ctx", "ctx"},
		{"", "ctx"},
	}
	for _, c := range cases {
		if got := diffLineKind(c.line); got != c.want {
			t.Errorf("diffLineKind(%q) = %q, want %q", c.line, got, c.want)
		}
	}
}

func TestCleanBlock(t *testing.T) {
	in := "  \x1b[32mgreen\x1b[m\r\nline2\r\n"
	if got := cleanBlock(in); got != "green\nline2" {
		t.Fatalf("cleanBlock = %q, want green\\nline2", got)
	}
}

func TestToolViewAssembly(t *testing.T) {
	withMappers(t, map[string]*tools.ParamMapper{
		"edit_file": {Name: "edit_file", In: []string{"Path"}, Out: []string{"Diff"}},
	})
	args := []byte(`{"Path":"service/tui/render.go"}`)
	out := toolView("edit_file", args, `{"Diff":"--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new"}`)
	lines := strings.Split(out, "\n")
	if len(lines) != 7 {
		t.Fatalf("want 7 lines (header+param+5 diff), got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], "edit_file") {
		t.Errorf("header missing tool name: %q", lines[0])
	}
	if !strings.Contains(lines[1], "Path") || !strings.Contains(lines[1], "render.go") {
		t.Errorf("param line broken: %q", lines[1])
	}
	if !strings.Contains(lines[2], "--- a/x.go") {
		t.Errorf("diff first line missing: %q", lines[2])
	}
	// + 行染绿、- 行染红（文本包在 ANSI 内，用包含断言）
	if !strings.Contains(out, "+new") || !strings.Contains(out, "-old") {
		t.Errorf("diff content missing:\n%s", out)
	}
}

func TestToolViewKeyAlignment(t *testing.T) {
	withMappers(t, map[string]*tools.ParamMapper{
		"x": {Name: "x", In: []string{"K", "LongerKey"}, Out: []string{"R"}},
	})
	out := toolView("x", []byte(`{"K":"v1","LongerKey":"v2"}`), `{"R":"ok"}`)
	lines := strings.Split(out, "\n")
	// 两行的 ANSI 前缀相同（同为 muted 单色），value 起始列必须相等
	if i1, i2 := strings.Index(lines[1], "v1"), strings.Index(lines[2], "v2"); i1 != i2 {
		t.Errorf("value columns misaligned (%d vs %d): %q / %q", i1, i2, lines[1], lines[2])
	}
	if !strings.Contains(lines[2], "LongerKey") {
		t.Errorf("LongerKey row broken: %q", lines[2])
	}
	// 结果标量行带 ↪ 前缀
	if !strings.Contains(lines[3], "↪ R") {
		t.Errorf("result row missing arrow: %q", lines[3])
	}
}

func TestRenderBlockTruncation(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	lines := renderBlock(textBlock{text: b.String()})
	if len(lines) != 11 { // 10 行 + 尾注
		t.Fatalf("want 11 lines, got %d", len(lines))
	}
	last := lines[len(lines)-1]
	if !strings.Contains(last, "共 12 行") || !strings.Contains(last, "前 10 行") {
		t.Fatalf("truncation note wrong: %q", last)
	}
}

func TestToolViewEmptyArgsResult(t *testing.T) {
	withMappers(t, map[string]*tools.ParamMapper{})
	out := toolView("date_now", nil, "")
	lines := strings.Split(out, "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "date_now") {
		t.Fatalf("want header only, got:\n%s", out)
	}
}

func TestOrderedKeysNumericSort(t *testing.T) {
	withMappers(t, map[string]*tools.ParamMapper{})
	obj := map[string]any{"10": "a", "2": "b", "0": "c", "1": "d"}
	got := orderedKeys("unregistered", obj, false)
	want := []string{"0", "1", "2", "10"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("numeric sort broken: got %v, want %v", got, want)
	}
}

func TestExpandJSONList(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{`["a.go","b.go"]`, "a.go\nb.go", true},
		{`[{"A":1},{"B":2}]`, "{\"A\":1}\n{\"B\":2}", true},
		{`[]`, "", false},
		{`{"k":"v"}`, "", false},
		{`plain text`, "", false},
	}
	for _, c := range cases {
		got, ok := expandJSONList(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("expandJSONList(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestResultPartsListExpansion(t *testing.T) {
	withMappers(t, map[string]*tools.ParamMapper{
		"glob": {Name: "glob", In: []string{"Regex"}, Out: []string{"Matches"}},
	})
	rows, blocks := resultParts("glob", `{"Matches":"[\"a.go\",\"b.go\",\"c.go\"]"}`)
	if len(rows) != 0 || len(blocks) != 1 {
		t.Fatalf("want 0 rows 1 block, got rows=%#v blocks=%#v", rows, blocks)
	}
	if blocks[0].isDiff {
		t.Fatalf("list block misdetected as diff")
	}
	lines := strings.Split(cleanBlock(blocks[0].text), "\n")
	if len(lines) != 3 || lines[0] != "a.go" || lines[2] != "c.go" {
		t.Fatalf("list expansion broken: %#v", lines)
	}
}

func TestArgRowsSkipEmptyValues(t *testing.T) {
	withMappers(t, map[string]*tools.ParamMapper{
		"status": {Name: "status", In: []string{"Id", "WaitSeconds"}, Out: nil},
	})
	rows := argRows("status", []byte(`{"Id":"","WaitSeconds":0}`))
	if len(rows) != 1 || rows[0].key != "WaitSeconds" {
		t.Fatalf("want empty-string value skipped, got %#v", rows)
	}
	// 数值 0 不是空值，应保留
	if rows[0].val != "0" {
		t.Fatalf("numeric zero should be kept, got %q", rows[0].val)
	}
}

func TestToolViewSearchInFileFallback(t *testing.T) {
	// search_in_file 形态：Out 无登记（nil）→ 数字键自然排序，每条匹配一行
	withMappers(t, map[string]*tools.ParamMapper{
		"search_in_file": {Name: "search_in_file", In: []string{"Path", "Regex"}, Out: nil},
	})
	res := `{"10":"m10","2":"m2","0":"m0"}`
	out := toolView("search_in_file", []byte(`{"Path":"x.go","Regex":"foo"}`), res)
	lines := strings.Split(out, "\n")
	// header + 2 参数行 + 3 结果行
	if len(lines) != 6 {
		t.Fatalf("want 6 lines, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[5], "m10") {
		t.Errorf("numeric order broken, last row: %q", lines[5])
	}
	if !strings.Contains(lines[3], "m0") {
		t.Errorf("numeric order broken, first result row: %q", lines[3])
	}
}
