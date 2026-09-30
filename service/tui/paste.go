package tui

import (
	"bytes"
	"os"
	"time"

	gotui "github.com/grindlemire/go-tui"
)

// bracketed paste 过滤（设计参照 pi agent 的 terminal.go/stdin-buffer.go）。
//
// go-tui v0.22.1 没有 bracketed paste（DECSET 2004）支持：终端粘贴的 \r
// 与手敲的 Enter 在字节层不可区分。这里在 fd 层补齐，不碰库：
//   - 用 NewAppWithReader 注入"读管道"的库原生 stdinReader；
//   - 过滤协程独占真实 stdin：普通字节（按键/鼠标/kitty 响应）原样进管道；
//     粘贴段（\x1b[200~…\x1b[201~）剥标记、\r\n/\r→\n、\t→4 空格、丢弃其余
//     控制字符后进管道；
//   - 库把管道字节当正常输入解析：可见字符→KeyRune 插入、\n→Ctrl+J 插行。
//     粘贴不可能产生 Enter/Tab/Esc 事件，所以无需输入节奏启发式。
//
// 时序契约：过滤协程必须在 NewAppWithReader 返回之后启动——构造期的 kitty
// 协商会同步读一次真实 stdin。?2004h 的启用在协程启动前写入。
// 退出时 Run 写 ?2004l；挂起/恢复（Ctrl+Z）期间库可能重新协商 kitty、
// 与本协程竞争 stdin，属已知边界（协商失败只降级输入协议，不致命）。

const (
	pasteStart = "\x1b[200~"
	pasteEnd   = "\x1b[201~"
	tabSpaces  = "    "
	// partialMarkerTimeout 疑似被截断标记的等待上限：数据末尾若是标记前缀
	// （比如单独的 Esc 正是 "\x1b[200~" 的前缀），暂存等待拼齐；超过该时限
	// 没有后续数据就当真实按键放行——对 Esc 序列造成至多 25ms 的延迟。
	partialMarkerTimeout = 25 * time.Millisecond
)

// pasteFilter 输入过滤器：独占真实 stdin 的协程 + 转发给库 reader 的包装。
type pasteFilter struct {
	inner gotui.EventReader // 库的 stdinReader：读管道（过滤后的字节流）
	pipeR *os.File          // 管道读端——必须保活：库只存 fd 整数，os.File 被 GC
	// 回收时 finalizer 会 close 该 fd，管道读端就静默失效（症状时灵时不灵）
	pipeW *os.File      // 管道写端
	done  chan struct{} // 过滤协程退出信号（Close 后协程随进程结束）
}

// newPasteFilter 建立管道与库 reader（不启动协程、不动终端模式）。
func newPasteFilter() (*pasteFilter, error) {
	pipeR, pipeW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	inner, err := gotui.NewEventReader(pipeR)
	if err != nil {
		pipeR.Close()
		pipeW.Close()
		return nil, err
	}
	return &pasteFilter{inner: inner, pipeR: pipeR, pipeW: pipeW, done: make(chan struct{})}, nil
}

// start 启用 bracketed paste 并启动过滤协程（此后独占真实 stdin）。
func (p *pasteFilter) start() {
	os.Stdout.WriteString("\x1b[?2004h")
	go p.filterLoop()
}

// stop 关闭 bracketed paste（Run 收尾时调用，复原终端模式）。
func (p *pasteFilter) stop() {
	os.Stdout.WriteString("\x1b[?2004l")
}

// ── EventReader 接口：逐项转发库 reader ───────────

func (p *pasteFilter) PollEvent(timeout time.Duration) (gotui.Event, bool) {
	return p.inner.PollEvent(timeout)
}

func (p *pasteFilter) Close() error {
	err := p.inner.Close()
	p.pipeW.Close() // 过滤协程可能在真实 stdin 上阻塞，随进程退出回收
	return err
}

// EnableInterrupt/Interrupt 转发：Stop/resize 需要从其他 goroutine 唤醒
// 事件循环（库对 reader 做接口断言，不实现则退化为超时轮询）。
func (p *pasteFilter) EnableInterrupt() error {
	if r, ok := p.inner.(gotui.InterruptibleReader); ok {
		return r.EnableInterrupt()
	}
	return nil
}

func (p *pasteFilter) Interrupt() error {
	if r, ok := p.inner.(gotui.InterruptibleReader); ok {
		return r.Interrupt()
	}
	return nil
}

// ── 过滤协程 ─────────────────────────────────────

// filterLoop 事件驱动：读协程投递真实 stdin 数据；carry 里存有疑似截断的
// 标记前缀时挂一个超时——超时仍无后续数据就按真实按键放行（Esc 歧义）。
func (p *pasteFilter) filterLoop() {
	defer close(p.done)
	defer p.pipeW.Close()
	ch := make(chan []byte)
	go p.readStdin(ch)
	buf := make([]byte, 0, 8192)
	var carry []byte // 末尾疑似被截断的标记前缀（标记可能跨 read 分裂）
	inPaste := false
	for {
		var timeout <-chan time.Time
		if len(carry) > 0 {
			timeout = time.After(partialMarkerTimeout)
		}
		var data []byte
		select {
		case d, ok := <-ch:
			if !ok {
				return
			}
			data = d
		case <-timeout:
			// 超时：carry 是真实按键（如单独的 Esc），原样放行
			p.pipeW.Write(carry)
			carry = nil
			continue
		}
		if len(carry) > 0 {
			data = append(carry, data...)
			carry = nil
		}
		buf = buf[:0]
		for len(data) > 0 {
			if inPaste {
				if idx := bytes.Index(data, []byte(pasteEnd)); idx >= 0 {
					buf = appendPaste(buf, data[:idx])
					data = data[idx+len(pasteEnd):]
					inPaste = false
				} else {
					keep := splitPartialMarker(data, pasteEnd)
					buf = appendPaste(buf, data[:keep])
					carry = append(carry[:0], data[keep:]...)
					data = nil
				}
			} else if idx := bytes.Index(data, []byte(pasteStart)); idx >= 0 {
				buf = append(buf, data[:idx]...)
				data = data[idx+len(pasteStart):]
				inPaste = true
			} else {
				keep := splitPartialMarker(data, pasteStart)
				buf = append(buf, data[:keep]...)
				carry = append(carry[:0], data[keep:]...)
				data = nil
			}
		}
		if len(buf) > 0 {
			if _, err := p.pipeW.Write(buf); err != nil {
				return
			}
		}
	}
}

// readStdin 阻塞读真实 stdin，把每块数据的副本投递给 filterLoop。
func (p *pasteFilter) readStdin(ch chan []byte) {
	defer close(ch)
	readBuf := make([]byte, 4096)
	for {
		n, err := os.Stdin.Read(readBuf)
		if err != nil {
			return
		}
		data := make([]byte, n)
		copy(data, readBuf[:n])
		ch <- data
	}
}

// appendPaste 规范粘贴内容：\r\n/\r 合并为单个 \n（经库解析为 Ctrl+J 插行）、
// \t→4 空格（放行会切焦点）、其余控制字符丢弃（ESC 会变中断、其余乱码）、
// 可见字符与 UTF-8 多字节原样保留。
func appendPaste(dst, src []byte) []byte {
	for i := 0; i < len(src); i++ {
		switch c := src[i]; {
		case c == '\r':
			if i+1 < len(src) && src[i+1] == '\n' {
				i++
			}
			dst = append(dst, '\n')
		case c == '\n':
			dst = append(dst, '\n')
		case c == '\t':
			dst = append(dst, tabSpaces...)
		case c < 0x20 || c == 0x7f:
			// 丢弃控制字符
		default:
			dst = append(dst, c)
		}
	}
	return dst
}

// splitPartialMarker 返回 data 前段的安全下发长度：末尾若是 marker 的可能
// 前缀（标记跨 read 截断），暂缓下发等待下一块拼齐。
func splitPartialMarker(data []byte, marker string) int {
	maxK := len(marker) - 1
	if maxK > len(data) {
		maxK = len(data)
	}
	for k := maxK; k > 0; k-- {
		if string(data[len(data)-k:]) == marker[:k] {
			return len(data) - k
		}
	}
	return len(data)
}
