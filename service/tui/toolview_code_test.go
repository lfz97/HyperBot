package tui

import (
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/x/ansi"
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
