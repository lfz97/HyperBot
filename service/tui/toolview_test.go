package tui

import (
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
