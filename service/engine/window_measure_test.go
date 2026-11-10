package engine

// 实验5：测量 B 方案（Cancel by requestID）的"注册前窗口"实际时长。
//
// 窗口定义：SubmitInput 发生 → runner.Run() 被调用 → registerRun 落账。
// 此区间内 Cancel(id) 查不到 run，Esc 丢失（"sliver"）。窗口主要由
// Run() 前置链路构成：getOrCreateSession(SQLite) → agent 工厂
// （refresh + initMainAgent）→ resolveMessages → registerRun。
//
// 测三个量做分解：
//  1) refresh() 独立耗时（冷/热）           —— 工厂前半
//  2) initMainAgent() 独立耗时              —— 工厂后半
//  3) Run()→RunStatus==true 真实窗口（冷/热）—— Cancel 失效区
//
// 生产同构：真实配置副本（含 MCP/skills）、SQLite session、真实模型网关。
// 隔离：只读复制真实 .hyperbot 的 yaml+skills，memory/session 全在 t.TempDir()。

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

const realHyperbotDir = "/home/lfz/hyperbot/.hyperbot"

// buildMeasureEngine 按生产路径装配引擎（隔离副本）。
func buildMeasureEngine(t *testing.T) *Engine {
	if _, err := os.Stat(realHyperbotDir); err != nil {
		t.Skipf("真实配置目录不存在: %v", err)
	}
	root := t.TempDir()
	cfgDir := filepath.Join(root, ".hyperbot")
	skillsDst := filepath.Join(cfgDir, "skills")
	for _, d := range []string{cfgDir, skillsDst, filepath.Join(root, "cwd")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	// 只读复制：yaml + skills（memory.db 不带，临时目录新建）
	if err := copyMeasureFile(
		filepath.Join(realHyperbotDir, "hyperbot.yaml"),
		filepath.Join(cfgDir, "hyperbot.yaml"),
	); err != nil {
		t.Fatalf("copy config: %v", err)
	}
	if err := copyMeasureDir(filepath.Join(realHyperbotDir, "skills"), skillsDst); err != nil {
		t.Fatalf("copy skills: %v", err)
	}

	e := &Engine{
		Agentname:          "HyperBot",
		CWD:                filepath.Join(root, "cwd"),
		ExeDir:             root,
		ConfigFolderPath:   cfgDir,
		HyperBotConfigPath: filepath.Join(cfgDir, "hyperbot.yaml"),
		SkillFolderPath:    skillsDst,
	}
	if err := e.flush(); err != nil { // 生产装配全链路（含 newRunner）
		t.Fatalf("flush: %v", err)
	}
	e.newSessionID() // 对齐生产 Run()：SubmitInput 链路读 SessionId，零值会被 session key 校验拒绝
	t.Cleanup(func() { e.Shutdown() })
	return e
}

// windowResult 一轮窗口测量的结果。
type windowResult struct {
	regDur time.Duration // Run() 发起 → RunStatus(id)==true（注册完成 = Cancel 从此有效）
	retDur time.Duration // Run() 发起 → Run() 返回（含 persist+agent 启动，参考值）
}

// measureWindow 一次真实 Run 的注册窗口测量：并发轮询 RunStatus，
// 注册确认的瞬间立刻 Cancel（既阻止模型真跑，也验证 Cancel 链路本身）。
func measureWindow(t *testing.T, e *Engine, mr runner.ManagedRunner, reqID, sessionID string) windowResult {
	t.Helper()
	registered := make(chan time.Duration, 1)
	t0 := time.Now()

	go func() {
		for {
			if _, ok := mr.RunStatus(reqID); ok {
				registered <- time.Since(t0)
				return // 只测注册时刻，取消挪到 Run 返回后（避免抢在 persist 前生效）
			}
			if time.Since(t0) > 90*time.Second {
				registered <- time.Since(t0)
				return
			}
			time.Sleep(500 * time.Microsecond)
		}
	}()

	ch, err := e.AgentRunner_p.Runner.Run(
		context.Background(),
		e.Config_p.User.UserID,
		sessionID,
		model.NewUserMessage("measurement probe"),
		agent.WithRequestID(reqID),
	)
	regDur := <-registered
	mr.Cancel(reqID) // Run 已返回（注册已完成），取消落在模型长调用期间
	if err != nil {
		// 理论上不该发生（取消在 Run 返回之后）；出现说明有别的路径问题，记录
		t.Logf("!! Run 返回错误（regDur=%v）: %v", regDur, err)
		return windowResult{regDur: regDur, retDur: time.Since(t0)}
	}
	for range ch { // 等 Cancel 传播、事件流收尾
	}
	return windowResult{regDur: regDur, retDur: time.Since(t0)}
}

func TestMeasureRegistrationWindow(t *testing.T) {
	e := buildMeasureEngine(t)
	mr, ok := e.AgentRunner_p.Runner.(runner.ManagedRunner)
	if !ok {
		t.Fatal("runner 未实现 ManagedRunner")
	}

	// ---- 1) refresh 独立耗时：冷 1 + 热 6 ----
	t0 := time.Now()
	if err := e.refresh(); err != nil {
		t.Fatalf("refresh(冷): %v", err)
	}
	coldRefresh := time.Since(t0)
	warmRefresh := []time.Duration{}
	for i := 0; i < 6; i++ {
		t0 = time.Now()
		if err := e.refresh(); err != nil {
			t.Fatalf("refresh(热%d): %v", i, err)
		}
		warmRefresh = append(warmRefresh, time.Since(t0))
	}

	// ---- 2) initMainAgent 独立耗时：热 6 ----
	agentDur := []time.Duration{}
	for i := 0; i < 6; i++ {
		t0 = time.Now()
		if _, err := e.initMainAgent(); err != nil {
			t.Fatalf("initMainAgent(%d): %v", i, err)
		}
		agentDur = append(agentDur, time.Since(t0))
	}

	// ---- 3) 注册窗口：冷 1 轮 + 热 6 轮（同 session，模拟真实连轮对话）----
	const sessionID = "measure-session-001"
	coldWindow := measureWindow(t, e, mr, "measure-cold", sessionID)
	warmWindows := []windowResult{}
	for i := 0; i < 6; i++ {
		warmWindows = append(warmWindows, measureWindow(
			t, e, mr, fmt.Sprintf("measure-warm-%d", i), sessionID))
	}

	// ---- 汇报 ----
	rd := measureStats(warmRefresh)
	ad := measureStats(agentDur)
	regD := measureStats(warmMeasureReg(warmWindows))
	retD := measureStats(warmMeasureRet(warmWindows))

	t.Log("\n========== 注册前窗口测量（真实装配）==========")
	t.Logf("refresh()        冷=%v   热 p50=%v max=%v", coldRefresh, rd.p50, rd.max)
	t.Logf("initMainAgent()  热 p50=%v max=%v", ad.p50, ad.max)
	t.Logf("注册窗口(冷)     reg=%v  run返回=%v", coldWindow.regDur, coldWindow.retDur)
	t.Logf("注册窗口(热)     reg p50=%v max=%v   run返回 p50=%v max=%v",
		regD.p50, regD.max, retD.p50, retD.max)
	t.Logf("分解(热): refresh p50=%v + initAgent p50=%v + 框架/session开销 ≈ %v",
		rd.p50, ad.p50, regD.p50-rd.p50-ad.p50)
	t.Log("判读: reg p50/max 即 Esc 丢失区时长（Cancel 查不到 run 的窗口）")
}

// TestEscInterruptEndToEnd 新中断链路的端到端验证（真实装配 + 真实模型网关）：
// SubmitInput 直通 → run 注册 → Interrupt（runner.Cancel）→ 事件流携带
// RunOutcome 收尾 → NoticeCancelled + 门复位。取消传播全在框架内（engine
// 不再持有 ctx 桥接），本测试锁定"取消 → 通知 → 收尾"的完整闭环。
func TestEscInterruptEndToEnd(t *testing.T) {
	e := buildMeasureEngine(t)

	if !e.SubmitInput("measurement probe") {
		t.Fatal("空闲时必须接受提交")
	}

	// 等 run 注册（注册前窗口 <3ms，见 TestMeasureRegistrationWindow）
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		id := e.curRequestID
		e.mu.Unlock()
		if id != "" {
			if _, ok := e.AgentRunner_p.Runner.RunStatus(id); ok {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	if time.Now().After(deadline) {
		t.Fatal("run 未在 60s 内注册")
	}

	if !e.Interrupt() {
		t.Fatal("运行期 Interrupt 必须返回 true")
	}

	// turn 必须经取消收尾（模型长调用被 Cancel 终止，事件流关闭）
	deadline = time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && e.isRunning() {
		time.Sleep(10 * time.Millisecond)
	}
	if e.isRunning() {
		t.Fatal("turn 未在 30s 内被取消收尾（running 仍为 true）")
	}
	if got := noticeKind(t, e); got != NoticeCancelled {
		t.Fatalf("notice=%q, want %q（RunOutcome → 会话已取消）", got, NoticeCancelled)
	}
}

// ---------- 统计与工具 ----------

type measureStat struct{ p50, max time.Duration }

func measureStats(ds []time.Duration) measureStat {
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return measureStat{p50: s[len(s)/2], max: s[len(s)-1]}
}

func warmMeasureReg(ws []windowResult) []time.Duration {
	var out []time.Duration
	for _, w := range ws {
		out = append(out, w.regDur)
	}
	return out
}

func warmMeasureRet(ws []windowResult) []time.Duration {
	var out []time.Duration
	for _, w := range ws {
		out = append(out, w.retDur)
	}
	return out
}

func copyMeasureFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func copyMeasureDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyMeasureFile(path, target)
	})
}
