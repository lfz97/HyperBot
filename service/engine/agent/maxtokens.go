package agent

import (
	"context"
	"encoding/json"
	"log"

	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

const (
	// clampSummaryReserveTokens 摘要调用自身的输出预留。WithMaxSummaryWords(5000 词)
	// 约 7500~10000 token，按上限取。钳制最坏落点为 窗口-此值，保证摘要请求
	// （input=全量上下文）始终有输出空间，消除"满窗后摘要请求发不出去"的死锁。
	clampSummaryReserveTokens = 10_000
	// clampBufferTokens 输入估算误差缓冲：tiktoken 对非 OpenAI 模型以 cl100k
	// 逼近（中文 ±30%）、消息结构/分隔符开销、状态栏等尾部注入的波动。
	clampBufferTokens = 2_000
	// clampMinViableTokens 钳制下限：低于此值输出已无意义，保持下限由
	// provider 截断（finish_reason=length），消息照常入库，下一跳由
	// 阈值压缩（Pass 1/2、摘要）接管，而非放弃输出。
	clampMinViableTokens = 1024
)

// TokenClampConfig 动态 max_tokens 钳制参数；传 nil 表示不启用钳制。
type TokenClampConfig struct {
	Counter       model.TokenCounter // 输入 token 计数器（tiktoken，与 summarizer 共用同一实例）
	ContextWindow int                // 当前模型上下文窗口，必须跟随模型配置，不能写死
	CfgMaxTokens  int                // 配置的静态 maxtokens 上限（yaml maxtokens 字段）
}

// clampMaxTokens 按以下公式钳制本次请求的 max_tokens：
//
//	max_tokens = min(配置上限, 窗口 - 输入 - 工具schema - 摘要预留 - 缓冲)
//
// 输入用 tiktoken 逐消息精确计数（与 summarizer 同一计数器）。推理模型的思维链
// 计入 max_tokens（deepseek-v4-pro @千帆实测：max_tokens=1000 全被 reasoning_tokens
// 消耗、content 为空），因此钳制的是"思考链+正文"的总预算。
//
// 计数失败时 fail-open：不动 max_tokens，仅记日志——宁可放行给 provider 的
// input+max_tokens 校验兜底，不因计数故障阻塞对话。
//
// 必须分配新指针：Request 内嵌 GenerationConfig 的 MaxTokens 与 yaml 配置共享
// 同一个 *int（init.go: MaxTokens: &cfg.MaxTokens），直接解引用修改会污染全局配置。
func clampMaxTokens(ctx context.Context, req *model.Request, cfg *TokenClampConfig) {
	if cfg == nil || cfg.Counter == nil || cfg.ContextWindow <= 0 {
		return
	}
	inputTokens := 0
	if len(req.Messages) > 0 {
		n, err := cfg.Counter.CountTokensRange(ctx, req.Messages, 0, len(req.Messages))
		if err != nil {
			log.Printf("[MAXTOKENS] message token count failed, skip clamp: %v", err)
			return
		}
		inputTokens = n
	}
	inputTokens += countToolSchemaTokens(ctx, cfg.Counter, req.Tools)

	ceiling := cfg.ContextWindow - inputTokens - clampSummaryReserveTokens - clampBufferTokens
	switch {
	case ceiling < clampMinViableTokens:
		// 输入已逼近窗口（正常情况下压缩 Pass 1/2 与摘要应在更低水位消化掉）：
		// 保持下限，由 provider 截断，不放大也不报错。
		log.Printf("[MAXTOKENS] input %d near window %d, clamp to floor %d",
			inputTokens, cfg.ContextWindow, clampMinViableTokens)
		ceiling = clampMinViableTokens
	case cfg.CfgMaxTokens > 0 && ceiling > cfg.CfgMaxTokens:
		ceiling = cfg.CfgMaxTokens
	}
	mt := ceiling
	req.MaxTokens = &mt
}

// countToolSchemaTokens 统计工具 schema（名称+描述+参数 JSON）的 token 数。
// schema 不在 Request.Messages 里，但同样占用上下文，且参与 provider 的
// input+max_tokens ≤ 窗口校验——不计入会导致高水位钳制偏松、实际请求被
// provider 以 400 拒绝。单条工具失败静默跳过，不阻塞钳制。
func countToolSchemaTokens(ctx context.Context, counter model.TokenCounter, tools map[string]tool.Tool) int {
	total := 0
	for _, t := range tools {
		if t == nil {
			continue
		}
		decl := t.Declaration()
		if decl == nil {
			continue
		}
		b, err := json.Marshal(decl)
		if err != nil {
			continue
		}
		n, err := counter.CountTokens(ctx, model.Message{Content: string(b)})
		if err != nil {
			continue
		}
		total += n + 4 // 工具名本身与分隔开销
	}
	return total
}
