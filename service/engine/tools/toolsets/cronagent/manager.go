package cronagent

import (
	"HyperBot/service/engine/agent"
	"HyperBot/service/engine/config"
	s "HyperBot/service/engine/session"
	functionTools "HyperBot/service/engine/tools/functions"
	"HyperBot/service/engine/tools/toolsets/localexec"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	ag "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

type manager struct {
	mu     sync.RWMutex
	sches  map[string]*schedule
	runner runner.Runner
	userid string
	path   string // cronagent.json 持久化文件路径
	// loadErr 保存启动时恢复存档的非致命错误。只在 NewManager 里、manager 被
	// 共享给其他 goroutine 之前写一次，之后只读，所以不需要加锁。
	loadErr error
}

// get 返回指定 id 的 schedule，不存在或已移除时返回 nil
func (m *manager) get(id string) *schedule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sches[id]
}

// newSchedule 是 Create 和 Load 的共同构造核心：不启动、不注册、不落盘。
// 三者的差异（id 是新生成还是沿用、paused 取默认还是取存档）留给调用方处理。
func (m *manager) newSchedule(id, cronexpr, prompt string, createdAt time.Time) *schedule {
	return &schedule{
		Prompt:    prompt,
		runner:    m.runner,
		Cronexpr:  cronexpr,
		Id:        id,
		resultBuf: bytes.NewBuffer(nil),
		createdAt: &createdAt,
		c:         cron.New(cron.WithParser(cronParser)),
		userid:    m.userid,
	}
}

func (m *manager) register(sche_p *schedule) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sches[sche_p.Id] = sche_p
}

func (m *manager) unregister(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sches[id] = nil
}

func (m *manager) Create(cronexpr string, prompt string) (string, error) {
	id := uuid.New().String()
	timeNow := time.Now()
	sche_p := m.newSchedule(id, cronexpr, prompt, timeNow)
	// Create 即启动，enableAt 与 createdAt 一致
	sche_p.enableAt = &timeNow
	// Add 会注册并启动调度，cron 表达式非法时既不能登记也不能落盘
	if err := sche_p.Add(); err != nil {
		return "", err
	}
	m.register(sche_p)
	// 落盘失败就回滚，不留一个跑着但重启后会消失的 agent
	if err := m.save(); err != nil {
		m.unregister(id)
		sche_p.Remove()
		return "", err
	}
	return id, nil
}

func (m *manager) Start(id string) error {
	sche_p := m.get(id)
	if sche_p == nil {
		return fmt.Errorf("agent id %s not found", id)
	}
	sche_p.Start()
	return m.save()
}

func (m *manager) Stop(id string) error {
	sche_p := m.get(id)
	if sche_p == nil {
		return fmt.Errorf("agent id %s not found", id)
	}
	sche_p.Stop()
	return m.save()
}

func (m *manager) Remove(id string) error {
	m.mu.Lock()
	sche_p := m.sches[id]
	if sche_p == nil {
		m.mu.Unlock()
		return fmt.Errorf("agent id %s not found", id)
	}
	m.sches[id] = nil
	m.mu.Unlock()
	sche_p.Remove()
	return m.save()
}

func (m *manager) Status(id string) (string, error) {
	sche_p := m.get(id)
	if sche_p == nil {
		return "", fmt.Errorf("agent id %s not found", id)
	}
	return jsonDump(sche_p.Status())
}

func (m *manager) StatusAll() (string, error) {
	// 先快照出 schedule 列表再释放 m.mu，避免持着 m.mu 去逐个抢 s.mu
	m.mu.RLock()
	sches := make([]*schedule, 0, len(m.sches))
	for _, sche_p := range m.sches {
		if sche_p == nil { // Remove 之后置 nil
			continue
		}
		sches = append(sches, sche_p)
	}
	m.mu.RUnlock()

	allstatus := make([]ScheduleStatus, 0, len(sches))
	for _, sche_p := range sches {
		allstatus = append(allstatus, sche_p.Status())
	}
	return jsonDump(allstatus)
}

func (m *manager) Output(id string, window int) (string, error) {
	sche_p := m.get(id)
	if sche_p == nil {
		return "", fmt.Errorf("agent id %s not found", id)
	}
	return sche_p.Output(window), nil
}

// Clear 停掉全部 schedule 的调度并清空 map，返回停掉的数量。
// 这是 Close 路径用的：刻意不 save，否则关闭程序就会抹掉全部存档。
func (m *manager) Clear() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, sche_p := range m.sches {
		if sche_p == nil {
			continue
		}
		sche_p.Remove()
		n++
	}
	m.sches = map[string]*schedule{}
	return n
}

// ClearAll 是面向用户的显式清空：停掉全部 schedule，并把空集合落盘。
// 与 Clear 的区别就在落盘——不落盘的话重启后任务会全部回来，那不叫清空。
func (m *manager) ClearAll() (int, error) {
	n := m.Clear()
	return n, m.save()
}

// snapshot 导出当前所有存活的 schedule，跳过 Remove 之后置 nil 的元素
func (m *manager) snapshot() []persistedSchedule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	recs := make([]persistedSchedule, 0, len(m.sches))
	for _, sche_p := range m.sches {
		if sche_p == nil {
			continue
		}
		recs = append(recs, toPersisted(sche_p))
	}
	return recs
}

func toPersisted(sche_p *schedule) persistedSchedule {
	sche_p.mu.Lock()
	defer sche_p.mu.Unlock()
	rec := persistedSchedule{
		Id:       sche_p.Id,
		Cronexpr: sche_p.Cronexpr,
		Prompt:   sche_p.Prompt,
		Paused:   sche_p.paused.Load(),
	}
	if sche_p.createdAt != nil {
		rec.CreatedAt = *sche_p.createdAt
	}
	return rec
}

func (m *manager) save() error {
	return writePersisted(m.path, m.snapshot())
}

// cronParser 是表达式解析的唯一来源：调度器和 Load 的预校验都必须用它，
// 否则会出现"校验通过但 Add 失败"的半加载状态。
// 用 SecondOptional 而非 Second，这样模型最常产出的 5 字段标准 Unix cron
// （秒位默认 0）和显式的 6 字段写法都能解析。
var cronParser = cron.NewParser(
	cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// Load 从持久化文件恢复 schedule。文件不存在时静默返回（首次运行）。
// 先整体校验再启动：Add() 会真的拉起 cron goroutine，一旦启动就无法靠返回
// error 回滚，所以表达式非法或 id 重复这类问题必须在启动之前全部挡掉。
// 任何失败都会把存档挪到 .fix<时间戳>，原路径腾空，启动照常继续。
func (m *manager) Load() error {
	recs, err := readPersisted(m.path)
	if err != nil {
		return m.quarantine(err)
	}
	if err := m.validate(recs); err != nil {
		return m.quarantine(err)
	}
	for _, r := range recs {
		sche_p := m.newSchedule(r.Id, r.Cronexpr, r.Prompt, r.CreatedAt)
		sche_p.paused.Store(r.Paused)
		if !r.Paused {
			// 停机期间错过的触发不补跑，只从下一次到点开始
			now := time.Now()
			sche_p.enableAt = &now
		}
		if err := sche_p.Add(); err != nil {
			// validate 用的是同一个 parser，走到这里说明表达式在两次解析之间变了，
			// 属于不可回滚的半加载状态，只能整体挪走重来
			m.Clear()
			return m.quarantine(err)
		}
		m.register(sche_p)
	}
	return nil
}

func (m *manager) validate(recs []persistedSchedule) error {
	seen := make(map[string]struct{}, len(recs))
	for _, r := range recs {
		if _, err := cronParser.Parse(r.Cronexpr); err != nil {
			return fmt.Errorf("cron agent %s 的表达式 %q 非法: %w", r.Id, r.Cronexpr, err)
		}
		if _, dup := seen[r.Id]; dup {
			return fmt.Errorf("cron agent id %s 重复", r.Id)
		}
		seen[r.Id] = struct{}{}
		if m.get(r.Id) != nil {
			return fmt.Errorf("cron agent id %s 与已在运行的任务冲突", r.Id)
		}
	}
	return nil
}

// quarantine 把用不了的存档挪到 .fix<时间戳>，并把新路径写进错误信息。
// 挪动本身失败时原文件仍在，但下一次 save 会覆盖它，所以要在信息里说明。
func (m *manager) quarantine(cause error) error {
	dst := quarantineFile(m.path)
	if dst == "" {
		return fmt.Errorf("加载 cron agent 存档失败，且无法挪走原文件（下次保存会覆盖它）: %w", cause)
	}
	return fmt.Errorf("加载 cron agent 存档失败，原文件已挪至 %s: %w", dst, cause)
}

func NewManager(agentname string, cfg *config.Config, systemprompt string, SkillFolderPath string, configFolderPath string) (*manager, error) {
	var builtinTools []tool.Tool
	builtinTools = append(builtinTools, functionTools.GetFileOperationsTools()...)
	builtinTools = append(builtinTools, functionTools.GetFileSystemTools()...)
	builtinTools = append(builtinTools, functionTools.GetDateTools()...)
	builtinTools = append(builtinTools, functionTools.GetTodoTools()...)

	SkillRepo, err := skill.NewFSRepository(SkillFolderPath)
	if err != nil {
		return nil, err
	}
	r := runner.NewRunnerWithAgentFactory(
		agentname,
		agentname,
		func(ctx context.Context, ro ag.RunOptions) (ag.Agent, error) {
			var Agent_p *llmagent.LLMAgent
			toolsets := []tool.ToolSet{}
			tools := []tool.Tool{}
			toolsets = append(toolsets, localexec.LocalExec())
			tools = append(tools, builtinTools...)
			opts := []llmagent.Option{
				llmagent.WithGenerationConfig(model.GenerationConfig{
					MaxTokens: &(*cfg).Model.MaxTokens, // 最大生成 token 数，来自配置 maxtokens 字段
					Stream:    (*cfg).Model.Stream,
				}),
				llmagent.WithTools(tools),
				llmagent.WithGlobalInstruction(systemprompt), //系统提示词
				llmagent.WithToolSets(toolsets),
				llmagent.WithRefreshToolSetsOnRun(true),
				llmagent.WithSkillsLoadedContentInToolResults(true),
				//仅注入知识，不注入执行工具的能力，统一通过localexec执行
				llmagent.WithSkills(SkillRepo),
				llmagent.WithSkillToolProfile(
					llmagent.SkillToolProfileKnowledgeOnly,
				),
				llmagent.WithAddSessionSummary(true),                                           //启用上下文压缩注入
				llmagent.WithSessionSummaryInjectionMode(llmagent.SessionSummaryInjectionUser), //摘要注入到user message，不与system prompt中的SOP规则竞争优先级
				llmagent.WithSyncSummaryIntraRun(true),                                         //在同一次对话中同步更新摘要
				llmagent.WithEnableContextCompaction(true),                                     // 启用 tool result 压缩（Pass 1+2）
				llmagent.WithContextCompactionOversizedToolResultMaxTokens(8192),               // Pass 2: 超大 tool result 首尾保留截断
				llmagent.WithEnableOnDemandSession(true),                                       // 按需加载被压缩的原始数据（session_load）
				llmagent.WithEnableParallelTools(true),                                         //启用并行工具调用，提升工具调用效率
				agent.SetBeforeModelStatusCallback(nil),                                        //追加beforeModel状态栏
			}
			// APIType 校验只做一次。ConfigBaseAgent 内部也按同一字段选模型，但它对
			// 未知类型是静默不设模型（agent 照样建得出来、跑起来才失败），所以这里
			// 先挡住，给出可读的配置错误。
			apiType := (*cfg).Model.APIType
			if apiType != "openai" && apiType != "anthropic" {
				return nil, errors.New("不支持的API类型，请检查配置文件中的 Model.APIType 字段")
			}
			Agent_p = agent.ConfigBaseAgent(
				agentname,
				(*cfg).Model,
				opts,
			)
			return Agent_p, nil
		},
		runner.WithSessionService(s.NewMemorySessionService((*cfg).Model, nil)),
	)
	m := &manager{
		sches:  map[string]*schedule{},
		runner: r,
		userid: (*cfg).User.UserID,
		path:   filepath.Join(configFolderPath, cronAgentFileName),
	}
	if err := m.Load(); err != nil {
		// 存档已被挪到 .fix<时间戳>，空集合是安全状态，不该阻止启动
		m.loadErr = err
	}
	return m, nil
}

func jsonDump[T any](v T) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(&v); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}
