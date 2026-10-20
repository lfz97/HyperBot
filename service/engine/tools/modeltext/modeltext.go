// Package modeltext 提供返回给模型的文本的公共度量与整形。
//
// 工具结果最终由框架 JSON 序列化后发给模型，序列化会放大文本：0x00 等控制字符
// 每字节展开成 6 字节的 `\u00XX`、非法 UTF-8 每字节展开成 6 字节的 `\ufffd`、
// 换行等膨胀 1 字节。所以「这段文本发给模型占多大上下文」必须按序列化后的大小
// 度量——按原始字节数判定内联上限的话，64KB 的二进制/GBK 输出可以膨胀到
// ~384KB，上限形同虚设。
//
// 所有要限制内联体量的输出点（localexec 的超长输出落盘、ReadFile 的分页窗口、
// SearchInFile 的匹配预算……）共用这里的度量与上限。
package modeltext

import (
	"encoding/json"
	"unicode/utf8"
)

// MaxInlineBytes 是单次内联返回给模型的体量上限（按 Size 计）。
// 各输出点按自己的方式消费这个预算：localexec 超限落盘、ReadFile 收缩窗口、
// SearchInFile 截断匹配列表。
const MaxInlineBytes = 64 << 10 // 64 KB

// Size 返回 s 经 JSON 字符串序列化后的大小（含两端引号），即它在模型上下文里
// 实际占据的字节数。框架用 encoding/json 序列化工具结果，这里的度量与之逐字节
// 一致；string 的序列化不会返回错误，忽略之。
func Size(s string) int {
	b, _ := json.Marshal(s)
	return len(b)
}

// TrimPartialRune 去掉末尾被字节窗口劈开的半个 UTF-8 字符。
// 被去掉的部分由调用方在下次读取时重新取回，不丢数据。
func TrimPartialRune(b []byte) []byte {
	// 从末尾往前找最近的 rune 起点；UTF-8 单字符最长 4 字节，回退 4 次足够。
	for i := len(b) - 1; i >= 0 && i >= len(b)-4; i-- {
		if !utf8.RuneStart(b[i]) {
			continue // 10xxxxxx，是延续字节，继续往前找
		}
		// b[i:] 是一个 rune 的开头；不合法说明这个 rune 被截断了，整段丢弃
		if !utf8.Valid(b[i:]) {
			return b[:i]
		}
		return b
	}
	return b
}
