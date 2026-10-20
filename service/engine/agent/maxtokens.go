package agent

import (
	"context"
	"encoding/json"
	"log"

	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

const (
	// 摘要请求自身的输出预留：WithMaxSummaryWords(5000 词) 约 7500~10000 token，按上限取，
	// 保证满窗后摘要请求仍发得出去。
	clampSummaryReserveTokens = 10000
	// 输入估算误差缓冲：tiktoken 对非 OpenAI 模型以 cl100k 逼近（中文 ±30%）+ 消息结构开销。
	clampBufferTokens = 2000
	// 钳制下限：低于此值不再下调，由 provider 截断（finish_reason=length），
	// 下一跳由阈值压缩（Pass 1/2、摘要）接管。
	clampMinViableTokens = 1024
)

// TokenClampConfig 动态 max_tokens 钳制参数；传 nil 表示不启用钳制。
type TokenClampConfig struct {
	Counter       model.TokenCounter // tiktoken 计数器，与 summarizer 共用同一实例
	ContextWindow int                // 跟随模型配置，不能写死
	CfgMaxTokens  int                // yaml maxtokens 静态上限
}

// clampMaxTokens 将本次请求的 max_tokens 钳制为 min(配置上限, 窗口-输入-工具schema-摘要预留-缓冲)。
// 推理模型的思维链计入 max_tokens（实测 max_tokens 全被 reasoning_tokens 消耗、content 为空），
// 因此钳制的是"思维链+正文"总预算。计数失败时 fail-open：不动 max_tokens，交给 provider 校验兜底。
// 必须分配新指针：Request.MaxTokens 与 yaml 配置共享同一个 *int，直接解引用会污染全局配置。
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
	if ceiling < clampMinViableTokens {
		// 输入已逼近窗口（正常由压缩 Pass 1/2 与摘要提前消化）：保持下限，交给 provider 截断。
		log.Printf("[MAXTOKENS] input %d near window %d, clamp to floor %d",
			inputTokens, cfg.ContextWindow, clampMinViableTokens)
		ceiling = clampMinViableTokens
	} else if cfg.CfgMaxTokens > 0 && ceiling > cfg.CfgMaxTokens {
		ceiling = cfg.CfgMaxTokens
	}
	mt := ceiling
	req.MaxTokens = &mt
}

// countToolSchemaTokens 统计工具 schema（名称+描述+参数 JSON）的 token 数。
// schema 不在 Messages 里但占窗口、参与 provider 的 input+max_tokens 校验，不计入会导致钳制偏松。
// 单条工具失败静默跳过，不阻塞钳制。
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
