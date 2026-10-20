// Package modeltext 把「模型朝向」的工具输出整理成适合进入上下文的文本。
//
// 背景（2026-10-20 事故）：localexec deliver() 的 64KB inline 阈值量的是原始
// 字节，但真正进入 LLM 上下文的是框架 json.Marshal 转义后的文本：二进制输出
// 里的非法 UTF-8 字节与控制字节被逐字节转义成 \ufffd / \u00XX（各 6 字符），
// 实测 25,840 字节的 cat 二进制输出膨胀到 128,007 字符（~5×），64KB 阈值对
// 二进制输出形同虚设，只能靠框架 context compaction 兜底截断。
//
// 职责边界：本包只做「模型通道」的无损化整理——非法字节折叠、控制字节剔除、
// ANSI 序列剥离。复用依赖树里已有的 charmbracelet/x/ansi（bubbletea/glamour
// 全家桶带入），与 TUI 渲染通道的 cleanBlock（service/tui/toolview.go）同一
// 思路：渲染通道剥色码防串色，模型通道剥色码防转义膨胀。权威 raw 副本由
// 调用方（deliver 的 spill）完整落盘，净化丢了什么用 cmp 对比即可审计。
//
// 刻意不做任何「聪明」处理：不压缩空白、不合并行、不转码——那才是真失真。
package modeltext

import (
	"bytes"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Sanitize 把任意字节序列整理成适合进入模型上下文的文本：
//   - 非法 UTF-8 → U+FFFD：与 json.Marshal 现状同语义，但在转义前完成，
//     且连续非法字节折叠为一个替换符，不再逐字节 6 字符膨胀
//   - ANSI 序列剥离：复用 ansi.Strip，色码不再以 \u001b 形态进上下文
//   - \r\n 归一为 \n：PTY 的 ONLCR 产物是纯传输噪声（与 TUI cleanBlock 同款处理）
//   - 控制字节剔除（\n \r \t 保留）：经过 Strip 后残留的 ESC 是残缺序列，同样剔除
//
// 长度语义：净化后字符数 ≈ 原始字节数（非法字节 1:1 折叠、合法多字节 UTF-8
// 还会缩减），调用方按原始字节判 inline 阈值重新是安全的。输入已是干净文本
// 时原样返回（零拷贝），日志类输出走这条快路。
func Sanitize(b []byte) []byte {
	if cleanEnough(b) {
		return b
	}
	cleaned := bytes.ToValidUTF8(b, []byte("\uFFFD"))
	cleaned = []byte(ansi.Strip(string(cleaned)))
	cleaned = bytes.ReplaceAll(cleaned, []byte("\r\n"), []byte("\n"))
	out := make([]byte, 0, len(cleaned))
	for _, c := range cleaned {
		switch {
		case c == '\n' || c == '\r' || c == '\t':
			out = append(out, c)
		case c < 0x20 || c == 0x7f:
			// 控制字节：转义前剔除。ESC 已被 Strip 消费，残留的是残缺序列残骸。
		default:
			out = append(out, c)
		}
	}
	return out
}

// IsBinaryLike 采样头部判断输出是否「二进制主导」：控制字节与非法 UTF-8
// 字节占比超阈值。用于 deliver() 的二进制快速通道——这类输出内联给模型只会
// 变成转义墙，落盘 + 诊断更合适。
//
// 阈值 30% 的依据：文本/日志 <1%，重度彩色日志的 ESC 占比 ~10%（计入坏字节，
// 防二进制里的零散 ESC 漏判），ELF/图片头 >50%，中间没有自然分布。
// 采样 8KB：对 25KB～GB 级输出足够区分，也避免为尾部少数字节扫描整个缓冲；
// 截断处的残缺多字节序列最多多贡献 1~3 个坏字节，对占比无影响。
func IsBinaryLike(b []byte) bool {
	const (
		sampleMax   = 8 << 10 // 8KB 采样
		badPermille = 300
	)
	if len(b) > sampleMax {
		b = b[:sampleMax]
	}
	if len(b) == 0 {
		return false
	}
	bad := 0
	for i := 0; i < len(b); {
		c := b[i]
		if c < utf8.RuneSelf {
			if (c < 0x20 && c != '\n' && c != '\r' && c != '\t') || c == 0x7f {
				bad++
			}
			i++
			continue
		}
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size == 1 {
			bad++ // 非法 UTF-8 字节
		}
		i += size
	}
	return bad*1000 >= len(b)*badPermille
}

// cleanEnough 快速路径判定：合法 UTF-8 且不含会被处理的对象（控制字节、
// DEL、\r）时，Sanitize 原样返回输入。注意两点：
//   - ESC 不在放行集里——含色码的输出走慢路径被 Strip
//   - \r 不放行——\r\n 归一发生在慢路径，快速路径放行会让纯 CRLF 文本
//     直通绕过归一（首版测试抓到的 bug）
func cleanEnough(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	for _, c := range b {
		if (c < 0x20 && c != '\n' && c != '\t') || c == 0x7f {
			return false
		}
	}
	return true
}
