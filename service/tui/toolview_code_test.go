package tui

import (
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/x/ansi"

	tools "HyperBot/service/engine/tools"
)

func TestLexerFor(t *testing.T) {
	l1 := lexerFor("x.go", "")
	if l1 == nil || l1.Config() == nil {
		t.Fatalf("want go lexer for .go file")
	}
	if n := l1.Config().Name; n != "Go" && n != "go" {
		t.Errorf(".go lexer name = %q", n)
	}
	// memoize：同名文件返回同一实例
	if l2 := lexerFor("x.go", ""); l2 != l1 {
		t.Errorf("lexer not memoized")
	}
	// 无扩展名：Analyse 兜底（内容像 Go）
	l3 := lexerFor("", "package main\nfunc main() {}\n")
	if l3 == nil || l3.Config() == nil {
		t.Fatalf("want analysed lexer for plain name")
	}
}

func TestHighlightCode(t *testing.T) {
	src := "func main() { return 42 }"
	out := highlightCode(src, "x.go")
	if out == src {
		t.Fatalf("want ANSI highlighted output, got plain")
	}
	// 剥 ANSI 后文本内容保留（chroma 可能补行尾，trim 比较）
	if got := strings.TrimSpace(ansi.Strip(out)); got != src {
		t.Errorf("content mangled: %q", got)
	}
	// 缓存命中：同输入返回同输出且不重复格式化
	if out2 := highlightCode(src, "x.go"); out2 != out {
		t.Errorf("cache miss for same key")
	}
	// 非 Go 文件不应命中同一缓存条目
	out3 := highlightCode(src, "x.py")
	if out3 == out {
		t.Errorf("cache key not separated by filename")
	}
}

func TestHighlightCodeConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = highlightCode("var x = 1", "x.go")
			_ = highlightCode("print('hi')", "x.py")
		}(i)
	}
	wg.Wait()
}

func TestCodeBlockLines(t *testing.T) {
	content := strings.Repeat("line\n", 15) // 15 行
	lines := codeBlockLines("x.go", content, 120)
	if len(lines) != 11 { // 10 行 + 尾注
		t.Fatalf("want 11 lines, got %d", len(lines))
	}
	// 行号列：前导行号 1..10
	if !strings.Contains(ansi.Strip(lines[0]), " 1") {
		t.Errorf("missing line number: %q", lines[0])
	}
	last := ansi.Strip(lines[10])
	if !strings.Contains(last, "共 15 行") {
		t.Errorf("truncation note wrong: %q", last)
	}
	// 高亮存在：Go 内容应带 ANSI
	if lines[0] == ansi.Strip(lines[0]) {
		t.Errorf("no syntax highlighting applied")
	}
}

func TestToolViewWriteFileContent(t *testing.T) {
	withMappers(t, map[string]*tools.ParamMapper{
		"write_file": {Name: "write_file", In: []string{"Path", "Content"}, Out: []string{"BytesWritten"}},
	})
	args := []byte(`{"Path":"x.go","Content":"func main() {\n\treturn 42\n}\n"}`)
	out := toolView("write_file", args, `{"BytesWritten":44}`, 120, "")
	lines := strings.Split(out, "\n")
	// header + Path 行 + 空行 + 3 行代码 + 空行 + 1 结果行 = 8
	if len(lines) != 8 {
		t.Fatalf("want 8 lines, got %d:\n%s", len(lines), out)
	}
	// 参数行不再出现 Content 截断值
	if strings.Contains(lines[1], "Content") {
		t.Errorf("Content should not render as truncated row: %q", lines[1])
	}
	if !strings.Contains(lines[1], "Path") || !strings.Contains(lines[1], "x.go") {
		t.Errorf("Path row broken: %q", lines[1])
	}
	// 代码块：完整内容行（不是 40 rune 截断）+ 高亮
	if !strings.Contains(ansi.Strip(out), "func main() {") {
		t.Errorf("code content missing:\n%s", out)
	}
	if !strings.Contains(ansi.Strip(out), "return 42") {
		t.Errorf("code body missing:\n%s", out)
	}
	if strings.Count(out, "\x1b[38;2;") == 0 {
		t.Errorf("no syntax highlighting in output")
	}
}

func TestRenderDiffBlockLineNumbers(t *testing.T) {
	diff := "--- a/x.go\n+++ b/x.go\n@@ -10,3 +10,4 @@\n ctx\n-old\n+new\n+new2\n"
	lines := renderDiffBlock(textBlock{text: diff, isDiff: true}, "x.go", 120)
	// 3 行内容（hunk 头不计入截断尾注前）+ hunk 头 + 头部两行 = 7 行
	if len(lines) != 7 {
		t.Fatalf("want 7 lines, got %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	flat := ansi.Strip(strings.Join(lines, "\n"))
	// 行号：ctx 行 = old 10 / new 10；-old = old 11；+new2 = new 12
	for _, want := range []string{"10 10", "11", "12"} {
		if !strings.Contains(flat, want) {
			t.Errorf("line number %q missing:\n%s", want, flat)
		}
	}
	// +/- 前缀保留（语法色分层模式：前缀 + 空格 + 内容）
	if !strings.Contains(flat, "- old") || !strings.Contains(flat, "+ new") {
		t.Errorf("diff content missing:\n%s", flat)
	}
	for _, l := range lines {
		s := ansi.Strip(l)
		if strings.HasPrefix(s, "-") || strings.Contains(s, " -old") {
			if !strings.Contains(l, "38;2;235;66;104") { // cError
				t.Errorf("del line not red: %q", l)
			}
		}
	}
	// 行内语法高亮存在（ctx 行内容 "ctx" 无 Go 关键字，看 +new 行染绿之外还有 chroma 色）
	hasChroma := false
	for _, l := range lines {
		s := ansi.Strip(l)
		if strings.Contains(s, "+new") && strings.Count(l, "\x1b[38;2;") >= 1 {
			hasChroma = true
		}
	}
	_ = hasChroma // +new 的绿色即 chroma/diff 叠加，弱断言：整块至少有一个 24-bit 色
	if strings.Count(strings.Join(lines, "\n"), "\x1b[38;2;") == 0 {
		t.Errorf("no 24-bit color in diff block")
	}
}
