package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

// pty 冒烟测试：在 80x24 伪终端里跑原型，按真实按键序列驱动。
//
// 断言采用「ANSI 剥离 + 全量文本累积」，而不是重建屏幕：bubbletea/ultraviolet
// 是差分重绘，用 CUP/CHA/VPA/EL/ECH 等序列只重写变化的行列，要精确还原屏幕
// 就得实现一个完备的 VT 模拟器（最小分支不值得养这个复杂度）。累积法把渲染
// 过的所有可见文本连起来做子串断言，足以验证：内容可达、无 tview 标签残留、
// 输入/中断/退出链路通。覆盖：启动横幅、回显、流式 markdown、esc 中断、
// 贴底跟随、waitForKey 退出。

// stripANSI 剥掉终端字节流里的转义序列，只留可见文本（\r 丢弃，其余保留）。
func stripANSI(data []byte) string {
	var b strings.Builder
	i := 0
	for i < len(data) {
		c := data[i]
		switch {
		case c == 0x1b && i+1 < len(data) && data[i+1] == '[': // CSI：扫到 0x40-0x7e 的 final byte
			j := i + 2
			for j < len(data) && (data[j] < 0x40 || data[j] > 0x7e) {
				j++
			}
			i = j + 1
		case c == 0x1b && i+1 < len(data) && (data[i+1] == ']' || data[i+1] == 'P'): // OSC/DCS：到 BEL 或 ST
			j := i + 2
			for j < len(data) {
				if data[j] == 0x07 {
					j++
					break
				}
				if data[j] == 0x1b && j+1 < len(data) && data[j+1] == '\\' {
					j += 2
					break
				}
				j++
			}
			i = j
		case c == 0x1b: // 其余两字节转义（ESC 7、ESC M 等）
			i += 2
		case c == '\r':
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// ── 测试驱动 ─────────────────────────────

func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "tuiproto")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build 失败: %v", err)
	}
	return bin
}

// waitForScreen 轮询累积文本，直到 sub 出现（曾渲染过即算）。
func waitForScreen(t *testing.T, plain *strings.Builder, raw *bytes.Buffer, timeout time.Duration, sub string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(plain.String(), sub) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("等待 %q 出现超时；累积文本尾部：%s\n原始字节尾部：%q", sub, tailOfPlain(plain), tailOf(raw))
}

func tailOfPlain(plain *strings.Builder) string {
	s := plain.String()
	if len(s) > 600 {
		s = s[len(s)-600:]
	}
	return s
}

func tailOf(buf *bytes.Buffer) string {
	b := buf.Bytes()
	if len(b) > 800 {
		b = b[len(b)-800:]
	}
	return string(b)
}

func TestTUIProtoSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过 pty 冒烟测试")
	}
	bin := buildBinary(t)

	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("启动 pty 失败: %v", err)
	}
	var raw bytes.Buffer   // 原始字节流（诊断用）
	var plain strings.Builder // ANSI 剥离后的累积文本（断言用）
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 65536)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				raw.Write(buf[:n])
				plain.WriteString(stripANSI(buf[:n]))
			}
			if err != nil {
				close(done)
				return
			}
		}
	}()
	defer func() {
		if cmd.Process != nil {
			// 先杀进程再关主端：Fatalf 退出路径下，若只 Close 而子进程还握着
			// slave fd，master 的 Read 不会立刻 EOF，上面的 <-done 会卡死
			_ = cmd.Process.Kill()
		}
		_ = ptmx.Close()
		<-done
		_ = cmd.Wait()
	}()

	type key = []byte
	send := func(ks ...key) {
		for _, k := range ks {
			if _, err := ptmx.Write(k); err != nil {
				t.Fatalf("写入 %q 失败: %v", k, err)
			}
			time.Sleep(30 * time.Millisecond)
		}
	}
	contains := func(sub string) bool { return strings.Contains(plain.String(), sub) }

	// 1. 启动横幅可读
	waitForScreen(t, &plain, &raw, 6*time.Second, "bubbletea 最小原型")

	// 2. 输入 + 回显（输入框可达、submit 通）
	send([]byte("hello\r"))
	waitForScreen(t, &plain, &raw, 5*time.Second, "▶ hello")

	// 3. 流式 turn 完成（最小版按源码显示 markdown）
	waitForScreen(t, &plain, &raw, 12*time.Second, "示例警告")
	if !contains("| 列 A | 列 B |") {
		t.Fatalf("markdown 源码未显示：流式内容缺失")
	}

	// 4. 无 tview 标签字面量（乱码根因）
	for _, tag := range []string{"[red]", "[green]", "[yellow]", "[gray", "[-:-:-]", "[white:"} {
		if contains(tag) {
			t.Fatalf("输出含 tview 标签 %q（乱码未除）", tag)
		}
	}

	// 5. /long 长输出 → 贴底跟随
	send([]byte("/long\r"))
	waitForScreen(t, &plain, &raw, 15*time.Second, "line-080")

	// 6. esc 中断运行中的流式
	send([]byte("slow\r"))
	time.Sleep(250 * time.Millisecond)
	send([]byte{0x1b})
	waitForScreen(t, &plain, &raw, 3*time.Second, "session cancelled")

	// 7. waitForKey 退出路径：置 exiting 后任意键（含输入框里的可打印字符）退出
	send([]byte("/boom\r"))
	waitForScreen(t, &plain, &raw, 5*time.Second, "模拟致命错误")
	send([]byte("x"))
	exited2 := make(chan error, 1)
	go func() { exited2 <- cmd.Wait() }()
	select {
	case err := <-exited2:
		if err != nil {
			t.Fatalf("任意键退出路径异常: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("按任意键后进程未退出")
	}
}
