package tui

import (
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
