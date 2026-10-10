package tui

import (
	"encoding/json"
)

// EngineView 是 TUI 对引擎的全部依赖（消费方定义的接口）。
// 上下游零 import：跨界只有 stdlib 类型与 JSON 文本，消息类型放在 JSON 的
// type/style/kind 字段上；引擎侧的 *Engine 天然满足本接口。
// 控制权全部在 TUI：状态由 TUI 按帧拉取，输入/取消由 TUI 主动调用。
type EngineView interface {
	Version() uint64
	Records() []string // 消息日志全量（每条一行 JSON，type 字段承载消息类型）
	RunStateJSON() string
	TodoText() string
	NoticeJSON() string
	StartupInfo() ([]string, bool)
	HelpItemsJSON() string
	SubmitInput(line string) bool
	Interrupt() bool
}

// ── 跨界 JSON 形状（与引擎侧 uistate.go 的 wire schema 对齐）──────────
//
// demo 的 model.ThirdpartMessage 同位物：跨 goroutine 传递的消息类型
// 与 JSON 契约集中在本文件，事件循环（tui.go）与渲染（render.go）只消费。

// wireFatal / wireRunState 运行状态快照。
type wireFatal struct {
	Text    string `json:"text"`
	Style   string `json:"style"` // plain | error | success | exit
	WaitKey bool   `json:"waitKey"`
}

type wireRunState struct {
	Running bool       `json:"running"`
	Fatal   *wireFatal `json:"fatal"`
}

// wireNotice 通知栏槽位快照。
type wireNotice struct {
	Kind  string `json:"kind"` // none | new_conversation | cancelled | success | warning
	Text  string `json:"text"`
	SetAt string `json:"setAt"` // RFC3339
}

// wireHelpItem 帮助页一行。
type wireHelpItem struct {
	Cmd  string `json:"cmd"`
	Desc string `json:"desc"`
}

// wireRecord 消息日志记录：type 承载消息类型。
//   - delta / message：msg 为框架 model.Message 原样（框架自带 json 签签，两端同型收发）
//   - user / slash / warn / error / summary：text 为语义原文
type wireRecord struct {
	Type string          `json:"type"`
	Msg  json.RawMessage `json:"msg,omitempty"`
	Text string          `json:"text,omitempty"`
}

// wireToolCall 待结果的工具调用缓冲条目（按 ToolCall.ID 索引）。
type wireToolCall struct {
	name string
	args string
}

// ── 事件循环消息 ─────────────────────────────────────────

// frameMsg 一帧渲染结果（全部为已渲染的最终形态文本）。
// 由 pullCmd（订阅式 cmd）在框架的 goroutine 上产出、return 交给框架送进
// Update——不经过 program.Send，走标准的 Elm 消息通道。
type frameMsg struct {
	view    string // 消息区全文（含横幅/终态，glamour 已渲染、lipgloss 已上色）
	todo    string // 清单栏渲染结果（空串=无清单）
	notice  string // 底部通知渲染结果（含 TTL 兜底）
	running bool   // agent 是否在运行（spinner 开关依据）
	fatal   *wireFatal
}
