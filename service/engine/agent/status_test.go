package agent

import (
	"strings"
	"testing"

	agentmemory "trpc.group/trpc-go/trpc-agent-go/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func TestCutAtSentence(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"短文本原样", "短句。", "短句。"},
		{"上限内取最后一个句末标点", strings.Repeat("前", 150) + "句子结束。后面还有内容继续填充。" + strings.Repeat("后", 60), strings.Repeat("前", 150) + "句子结束。后面还有内容继续填充。"},
		{"版本号和小数点不是句末", strings.Repeat("前", 150) + "。版本 v1.11.0 发布之后废弃了旧路径" + strings.Repeat("后", 60), strings.Repeat("前", 150) + "。"},
	}
	for _, c := range cases {
		if got := cutAtSentence(c.in, memoryEntryMaxRunes); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestCutAtSentenceRelaxedThenHard(t *testing.T) {
	noBoundary := strings.Repeat("字", 200) + strings.Repeat("中", 40) + "。结尾"
	got := cutAtSentence(noBoundary, memoryEntryMaxRunes)
	if want := strings.Repeat("字", 200) + strings.Repeat("中", 40) + "。"; got != want {
		t.Errorf("relaxed: got %d runes want %d runes", len([]rune(got)), len([]rune(want)))
	}
	allNoBoundary := strings.Repeat("字", 500)
	if got := cutAtSentence(allNoBoundary, memoryEntryMaxRunes); len([]rune(got)) != 200 {
		t.Errorf("hard cut: got %d runes want 200", len([]rune(got)))
	}
}

func TestBuildMemoryQueriesUsesLatestReasoningTail(t *testing.T) {
	userPrompt := "完成当前任务" + strings.Repeat("并保留完整约束", 30)
	messages := []model.Message{
		{Role: model.RoleAssistant, ReasoningContent: "上一轮推理不能进入查询。"},
		{Role: model.RoleUser, Content: userPrompt},
		{Role: model.RoleAssistant, ReasoningContent: "较早的推理也不能进入查询。"},
		{
			Role:             model.RoleAssistant,
			Content:          "这个对外正文不能进入查询。",
			ReasoningContent: "第一句。第二句。第三句。第四句。第五句。第六句。",
		},
	}

	userQuery, reasoningQuery := buildMemoryQueries(messages)
	if userQuery != userPrompt {
		t.Errorf("user query was truncated or changed: %q", userQuery)
	}
	want := "第二句。 第三句。 第四句。 第五句。 第六句。"
	if reasoningQuery != want {
		t.Errorf("reasoning query mismatch: got %q want %q", reasoningQuery, want)
	}
}

func TestBuildMemoryQueriesDoesNotFallbackToContentOrEarlierReasoning(t *testing.T) {
	messages := []model.Message{
		{Role: model.RoleUser, Content: "排查当前故障"},
		{Role: model.RoleAssistant, ReasoningContent: "较早推理不能兜底。"},
		{Role: model.RoleAssistant, Content: "最新正文也不能兜底。"},
	}

	userQuery, reasoningQuery := buildMemoryQueries(messages)
	if userQuery != "排查当前故障" || reasoningQuery != "" {
		t.Fatalf("expected user query only, got user=%q reasoning=%q", userQuery, reasoningQuery)
	}
}

func TestBuildMemoryQueriesDoesNotCrossTurn(t *testing.T) {
	messages := []model.Message{
		{Role: model.RoleUser, Content: "上一轮任务"},
		{Role: model.RoleAssistant, ReasoningContent: "上一轮推理不能进入新一轮。"},
		{Role: model.RoleUser, Content: "新一轮任务"},
	}

	userQuery, reasoningQuery := buildMemoryQueries(messages)
	if userQuery != "新一轮任务" || reasoningQuery != "" {
		t.Fatalf("expected new-turn user query only, got user=%q reasoning=%q", userQuery, reasoningQuery)
	}
}

func newEntry(id, text string, score float64) *agentmemory.Entry {
	return &agentmemory.Entry{ID: id, Memory: &agentmemory.Memory{Memory: text}, Score: score}
}

func TestRenderMemoryEntriesLabelsChannelsAndNormalizesSeparately(t *testing.T) {
	block, logLine := renderMemoryEntries(
		[]*agentmemory.Entry{
			newEntry("a", "用户任务相似经历。", 0.8),
			newEntry("b", "用户任务相关事实。", 0.4),
		},
		[]*agentmemory.Entry{
			newEntry("c", "执行推理相似经历。", 0.2),
			newEntry("d", "执行推理相关事实。", 0.1),
		},
		true,
	)

	if !strings.HasPrefix(block, "[MEMORY]\n[USER]\n") {
		t.Errorf("expected plain memory and user headers, got %q", block)
	}
	if !strings.Contains(block, "\n[REASONING]\n") {
		t.Errorf("reasoning header missing: %q", block)
	}
	if strings.Contains(block, "v4") || strings.Contains(block, "v5") {
		t.Errorf("version leaked into memory block: %q", block)
	}
	if got := strings.Count(block, "可能与当前相关（100%）"); got != 2 {
		t.Errorf("each channel should normalize its top entry to 100%%, got %d in %q", got, block)
	}
	if got := strings.Count(block, "可能与当前相关（50%）"); got != 2 {
		t.Errorf("each channel should normalize its second entry to 50%%, got %d in %q", got, block)
	}
	if !strings.Contains(logLine, "user=2") || !strings.Contains(logLine, "reasoning=2") {
		t.Errorf("channel counts missing from log: %q", logLine)
	}
}

func TestRenderMemoryEntriesDeduplicatesAcrossChannels(t *testing.T) {
	block, _ := renderMemoryEntries(
		[]*agentmemory.Entry{newEntry("same", "用户侧优先保留。", 0.8)},
		[]*agentmemory.Entry{newEntry("same", "推理侧重复条目。", 0.9)},
		true,
	)

	if !strings.Contains(block, "用户侧优先保留") || strings.Contains(block, "推理侧重复条目") {
		t.Errorf("cross-channel deduplication mismatch: %q", block)
	}
}

func TestRenderMemoryEntriesOmitsReasoningWhenNotQueried(t *testing.T) {
	block, _ := renderMemoryEntries(nil, nil, false)
	if block != "[MEMORY]\n[USER] 0 hits" {
		t.Errorf("unexpected user-only zero-hits block: %q", block)
	}
}

func TestRenderMemoryEntriesShowsReasoningZeroHits(t *testing.T) {
	block, _ := renderMemoryEntries(nil, nil, true)
	want := "[MEMORY]\n[USER] 0 hits\n[REASONING] 0 hits"
	if block != want {
		t.Errorf("unexpected dual-channel zero-hits block: %q", block)
	}
}
