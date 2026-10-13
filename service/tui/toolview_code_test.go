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
	out := toolView("write_file", args, `{"BytesWritten":44}`, 120)
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
