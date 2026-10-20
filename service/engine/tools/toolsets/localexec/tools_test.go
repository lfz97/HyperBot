package localexec

// 框架把工具结果 JSON 序列化后发给模型，序列化对非法 UTF-8/控制字符有最高
// 6 倍放大，所以 deliver 的内联判定必须基于序列化后的大小。

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// inlineJSONSize 返回 deliver 结果经 JSON 序列化后的大小，即模型实际收到的字节数。
func inlineJSONSize(t *testing.T, res map[string]string) int {
	t.Helper()
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	return len(b)
}

// 原始字节数未超限、但序列化后超限的输出必须落盘而不是内联。
func TestDeliverEscapedOverflowSpills(t *testing.T) {
	// 11KB 的 0xB4（非法 UTF-8 字节）：原始 11KB 远未超限，序列化后 6 倍放大
	// 超过 64KB。取 8KB 做对照，序列化后 ~48KB，应正常内联。
	for _, tc := range []struct {
		kb    int
		spill bool
	}{
		{8, false},
		{11, true},
	} {
		data := make([]byte, tc.kb<<10)
		for i := range data {
			data[i] = 0xB4
		}
		res := map[string]string{}
		if err := deliver(res, data, "test-invalid"); err != nil {
			t.Fatalf("deliver: %v", err)
		}
		if got := res["OutputFile"] != ""; got != tc.spill {
			t.Fatalf("%dKB invalid bytes: spilled = %v, want %v (inline json %d bytes)",
				tc.kb, got, tc.spill, inlineJSONSize(t, res))
		}
		if size := inlineJSONSize(t, res); size > inlineMaxBytes {
			t.Fatalf("%dKB invalid bytes: inline result is %d bytes (json), over the %d-byte limit",
				tc.kb, size, inlineMaxBytes)
		}
	}
}

// 二进制输出里大量的 0x00 同样按序列化后大小判定：原始 32KB，序列化 6 倍后落盘。
func TestDeliverControlCharsSpill(t *testing.T) {
	data := make([]byte, 32<<10)
	res := map[string]string{}
	if err := deliver(res, data, "test-control"); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if res["OutputFile"] == "" {
		t.Fatalf("expected spill, got inline result of %d bytes (json)", inlineJSONSize(t, res))
	}
	// 落盘文件保留原始字节，TotalBytes 报原始大小。
	if res["TotalBytes"] != strconv.Itoa(len(data)) {
		t.Fatalf("TotalBytes = %q, want %d", res["TotalBytes"], len(data))
	}
}

// 合法 UTF-8 且序列化后不超限的输出应原样内联，内容不做任何转换。
func TestDeliverValidUTF8Inlines(t *testing.T) {
	data := []byte(strings.Repeat("hello\n", 100))
	res := map[string]string{}
	if err := deliver(res, data, "test-inline"); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if res["OutputFile"] != "" {
		t.Fatalf("unexpected spill: %s", res["OutputFile"])
	}
	if res["Output"] != string(data) {
		t.Fatal("Output should be the original text")
	}
	// 少量非法字节同样原样内联，交给框架序列化时替换成 \ufffd。
	bad := []byte("ok \xB4\xD6 tail")
	res2 := map[string]string{}
	if err := deliver(res2, bad, "test-bad"); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if res2["Output"] != string(bad) {
		t.Fatalf("Output = %q, want %q", res2["Output"], string(bad))
	}
}

// 巨大的输出（远超原始上限）同样落盘。
func TestDeliverOverRawLimitSpills(t *testing.T) {
	big := make([]byte, inlineMaxBytes+1)
	res := map[string]string{}
	if err := deliver(res, big, "test-big"); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if res["OutputFile"] == "" {
		t.Fatal("expected spill for output over the raw byte limit")
	}
}
