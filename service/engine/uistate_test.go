package engine

import (
	"encoding/json"
	"testing"
	"time"
)

// 本文件锁定 JSON 契约的状态语义：版本号单调、增量拉取为快照、
// 提交输入的 best-effort 行为、终态/通知/帮助项的 JSON 形状。

func TestAppendAndRecords(t *testing.T) {
	e := GetEngineService("test")
	if e.Version() != 0 {
		t.Fatalf("初始 Version 应为 0，得到 %d", e.Version())
	}

	e.appendRecord(`{"type":"user","text":"hello"}`)
	e.appendTyped(RecWarn, "3 秒后重试")
	e.AppendSummary("已生成摘要")

	if e.Version() != 3 {
		t.Fatalf("三次写入后 Version 应为 3，得到 %d", e.Version())
	}

	all := e.Records(0)
	if len(all) != 3 {
		t.Fatalf("Records(0) 应返回 3 条，得到 %d", len(all))
	}
	if all[0] != `{"type":"user","text":"hello"}` {
		t.Fatalf("第 1 条记录不符：%s", all[0])
	}

	var rec struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(all[1]), &rec); err != nil {
		t.Fatalf("记录必须是合法 JSON：%v", err)
	}
	if rec.Type != RecWarn || rec.Text != "3 秒后重试" {
		t.Fatalf("appendTyped 形状不符：%s", all[1])
	}
	if err := json.Unmarshal([]byte(all[2]), &rec); err != nil {
		t.Fatalf("记录必须是合法 JSON：%v", err)
	}
	if rec.Type != RecSummary || rec.Text != "已生成摘要" {
		t.Fatalf("摘要记录形状不符：%s", all[2])
	}

	// 增量拉取：Seq 语义 = 顺序计数
	if got := e.Records(1); len(got) != 2 {
		t.Fatalf("增量拉取应返回 2 条，得到 %d", len(got))
	}
	if got := e.Records(3); len(got) != 0 {
		t.Fatalf("全部消费后增量拉取应为空，得到 %d", len(got))
	}
}

func TestSubmitInput(t *testing.T) {
	e := GetEngineService("test")

	// 引擎忙（无人接收）时提交失败——旧版 unbuffered chan + default 的行为
	if e.SubmitInput("busy") {
		t.Fatal("无接收方时 SubmitInput 应返回 false")
	}

	got := make(chan string, 1)
	go func() { got <- (<-e.inputCh) }()
	// 接收方必须先真正阻塞在 channel 上，SubmitInput 的 select 才能看到它；
	// Go 没有用户态的"接收已注册"同步点，用短暂 sleep 让调度进入稳定状态
	time.Sleep(100 * time.Millisecond)
	if !e.SubmitInput("hello") {
		t.Fatal("有接收方时 SubmitInput 应返回 true")
	}
	select {
	case v := <-got:
		if v != "hello" {
			t.Fatalf("提交的输入应原样送达，得到 %q", v)
		}
	case <-time.After(time.Second):
		t.Fatal("输入未被引擎侧收到")
	}
}

func TestRunStateJSON(t *testing.T) {
	e := GetEngineService("test")

	var idle struct {
		Running bool `json:"running"`
		Fatal   *struct {
			Text    string `json:"text"`
			Style   string `json:"style"`
			WaitKey bool   `json:"waitKey"`
		} `json:"fatal"`
	}
	if err := json.Unmarshal([]byte(e.RunStateJSON()), &idle); err != nil {
		t.Fatalf("RunStateJSON 必须是合法 JSON：%v", err)
	}
	if idle.Running || idle.Fatal != nil {
		t.Fatalf("初始快照应为运行中/无终态：%s", e.RunStateJSON())
	}

	e.setRunning(true)
	e.setFatal(FatalError, "加载配置文件错误: x", true)
	if err := json.Unmarshal([]byte(e.RunStateJSON()), &idle); err != nil {
		t.Fatalf("RunStateJSON 必须是合法 JSON：%v", err)
	}
	if !idle.Running || idle.Fatal == nil {
		t.Fatalf("终态快照缺失：%s", e.RunStateJSON())
	}
	if idle.Fatal.Style != FatalError || idle.Fatal.Text != "加载配置文件错误: x" || !idle.Fatal.WaitKey {
		t.Fatalf("终态字段不符：%+v", idle.Fatal)
	}
}

func TestNoticeAndHelpItemsJSON(t *testing.T) {
	e := GetEngineService("test")

	var notice struct {
		Kind  string `json:"kind"`
		Text  string `json:"text"`
		SetAt string `json:"setAt"`
	}
	if err := json.Unmarshal([]byte(e.NoticeJSON()), &notice); err != nil {
		t.Fatalf("NoticeJSON 必须是合法 JSON：%v", err)
	}
	if notice.Kind != NoticeNone {
		t.Fatalf("初始通知槽位应为 none：%s", e.NoticeJSON())
	}

	e.setNotice(NoticeWarning, "broken")
	if err := json.Unmarshal([]byte(e.NoticeJSON()), &notice); err != nil {
		t.Fatalf("NoticeJSON 必须是合法 JSON：%v", err)
	}
	if notice.Kind != NoticeWarning || notice.Text != "broken" {
		t.Fatalf("通知槽位不符：%s", e.NoticeJSON())
	}
	if _, err := time.Parse(time.RFC3339, notice.SetAt); err != nil {
		t.Fatalf("setAt 应为 RFC3339 时间戳：%q", notice.SetAt)
	}

	if e.HelpItemsJSON() != "[]" {
		t.Fatalf("初始技能帮助项应为空数组：%s", e.HelpItemsJSON())
	}
	e.setSkillHelpItems([]HelpItem{{Cmd: "/foo", Desc: "bar"}})
	if e.HelpItemsJSON() != `[{"cmd":"/foo","desc":"bar"}]` {
		t.Fatalf("帮助项 JSON 形状不符：%s", e.HelpItemsJSON())
	}
}

func TestCancelHook(t *testing.T) {
	e := GetEngineService("test")
	e.Cancel() // 未注册时必须是 no-op，不能 panic

	fired := false
	e.setActiveCancel(func() { fired = true })
	e.Cancel()
	if !fired {
		t.Fatal("注册后 Cancel 应触发取消函数")
	}

	e.setActiveCancel(nil)
	fired = false
	e.Cancel()
	if fired {
		t.Fatal("注销后 Cancel 不应再触发")
	}
}

func TestSetTodoText(t *testing.T) {
	e := GetEngineService("test")
	e.SetTodoText("[TODO] x")
	if e.TodoText() != "[TODO] x" {
		t.Fatalf("清单文本不符：%q", e.TodoText())
	}
	e.SetTodoText("") // 空串必须能写回（清单栏塌陷依据）
	if e.TodoText() != "" {
		t.Fatalf("空串应可写回：%q", e.TodoText())
	}
}
