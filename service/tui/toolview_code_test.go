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
		"WriteFile": {Name: "WriteFile", In: []string{"Path", "Content"}, Out: []string{"BytesWritten"}},
	})
	args := []byte(`{"Path":"x.go","Content":"func main() {\n\treturn 42\n}\n"}`)
	out := toolView("WriteFile", args, `{"BytesWritten":44}`, 120, "")
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
	// 4 行内容（---/+++/@@ 头行不渲染）
	if len(lines) != 4 {
		t.Fatalf("want 4 lines, got %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	flat := ansi.Strip(strings.Join(lines, "\n"))
	// 原始 diff 头不出现
	for _, noise := range []string{"--- a/x.go", "+++", "@@"} {
		if strings.Contains(flat, noise) {
			t.Errorf("raw diff header leaked: %q\n%s", noise, flat)
		}
	}
	// 单列行号：ctx = 新 10；-old = 旧 11；+new = 新 11；+new2 = 新 12
	for _, want := range []string{"10", "11", "12"} {
		if !strings.Contains(flat, want) {
			t.Errorf("line number %q missing:\n%s", want, flat)
		}
	}
	// 标记列：行号 + 空格 + +/- + 空格 + 内容
	if !strings.Contains(flat, "11 - old") || !strings.Contains(flat, "11 + new") {
		t.Errorf("diff content missing:\n%s", flat)
	}
	if strings.Count(strings.Join(lines, "\n"), "\x1b[38;2;") == 0 {
		t.Errorf("no 24-bit color in diff block")
	}
}

func TestRenderDiffBlockHunkGap(t *testing.T) {
	// 两个 hunk：中间插入弱化 … 行示意行号跳变
	diff := "@@ -1,2 +1,2 @@\n a\n-b\n@@ -10,2 +10,2 @@\n c\n+d\n"
	lines := renderDiffBlock(textBlock{text: diff, isDiff: true}, "x.go", 120)
	flat := ansi.Strip(strings.Join(lines, "\n"))
	if strings.Count(flat, "…") != 1 {
		t.Errorf("want exactly one hunk gap:\n%s", flat)
	}
	if !strings.Contains(flat, "- b") || !strings.Contains(flat, "+ d") {
		t.Errorf("hunk content missing:\n%s", flat)
	}
}

func TestRenderDiffBlockTruncationCountsContent(t *testing.T) {
	// 3 头行 + 12 内容行：截断计数只算内容行
	var b strings.Builder
	b.WriteString("--- a/x.go\n+++ b/x.go\n@@ -1,12 +1,12 @@\n")
	for i := 0; i < 12; i++ {
		b.WriteString(" ctx\n")
	}
	lines := renderDiffBlock(textBlock{text: b.String(), isDiff: true}, "x.go", 120)
	if len(lines) != 11 { // 10 内容行 + 尾注
		t.Fatalf("want 11 lines, got %d", len(lines))
	}
	last := ansi.Strip(lines[len(lines)-1])
	if !strings.Contains(last, "共 12 行") {
		t.Errorf("truncation note should count content lines only: %q", last)
	}
}

func TestToolViewWriteFileResultContent(t *testing.T) {
	withMappers(t, map[string]*tools.ParamMapper{
		"WriteFile": {Name: "WriteFile", In: []string{"Path", "Content"}, Out: []string{"BytesWritten"}},
	})
	// 响应携带 content（用户改造后的 WriteFile）：渲染为代码块，
	// 结果行不重复展示 content 截断值
	args := []byte(`{"Path":"x.py","Content":"print(1)\nprint(2)\n"}`)
	result := `{"BytesWritten":20,"content":"print(1)\nprint(2)\n"}`
	out := toolView("WriteFile", args, result, 120, `{"result":{"BytesWritten":20,"content":"print(1)\nprint(2)\n"}}`)
	flat := ansi.Strip(out)
	if !strings.Contains(flat, "print(1)") || !strings.Contains(flat, "print(2)") {
		t.Errorf("result content not rendered as code block:\n%s", flat)
	}
	if !strings.Contains(flat, "BytesWritten") || !strings.Contains(flat, "20") {
		t.Errorf("BytesWritten row missing:\n%s", flat)
	}
	// content 只出现一次（代码块），不出现第二次（值行）
	if n := strings.Count(flat, "print(1)"); n != 1 {
		t.Errorf("content duplicated (%d occurrences)", n)
	}
}

func TestToolViewBodySectionLines(t *testing.T) {
	withMappers(t, map[string]*tools.ParamMapper{
		"CD": {Name: "CD", In: []string{"Path"}, Out: []string{"CwdNow"}},
	})
	out := toolView("CD", []byte(`{"Path":"/tmp"}`), `{"CwdNow":"/home/x"}`, 120, "")
	lines := strings.Split(out, "\n")
	// 头部行无竖线
	if strings.HasPrefix(ansi.Strip(lines[0]), "│") {
		t.Errorf("header should not carry section line: %q", lines[0])
	}
	if len(lines) != 4 { // header + Path + 段间空行 + CwdNow
		t.Fatalf("want 4 lines, got %d:\n%s", len(lines), out)
	}
	// 参数区行带暗灰线（#605F6B = 96;95;107）
	if !strings.HasPrefix(lines[1], "\x1b[38;2;96;95;107m│") {
		t.Errorf("param line missing dark-gray line: %q", lines[1])
	}
	// 段间空行不带竖线（两段竖线在此断开）
	if strings.Contains(lines[2], "│") {
		t.Errorf("section separator should not carry line: %q", lines[2])
	}
	// 结果区行带淡绿线（#12C78F = 18;199;143）
	if !strings.HasPrefix(lines[3], "\x1b[38;2;18;199;143m│") {
		t.Errorf("result line missing soft-green line: %q", lines[3])
	}
}
