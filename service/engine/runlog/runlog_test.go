package runlog

import (
	"testing"
	"time"
)

func TestAppendAndRecords(t *testing.T) {
	s := NewStore()
	if s.Version() != 0 {
		t.Fatalf("初始 Version 应为 0，得到 %d", s.Version())
	}

	s.Append(KindUser, "hello")
	s.Append(KindContentDelta, "wo")
	s.AppendTool("read_file", "(a.txt)", "(ok)")

	if s.Version() != 3 {
		t.Fatalf("三次写入后 Version 应为 3，得到 %d", s.Version())
	}

	all := s.Records(0)
	if len(all) != 3 {
		t.Fatalf("Records(0) 应返回 3 条，得到 %d", len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i].Seq <= all[i-1].Seq {
			t.Fatalf("Records 必须按 Seq 升序：%v", all)
		}
	}
	if all[0].Kind != KindUser || all[0].Text != "hello" {
		t.Fatalf("第 1 条记录不符：%+v", all[0])
	}
	if all[2].Tool == nil || all[2].Tool.Name != "read_file" {
		t.Fatalf("第 3 条应为工具记录：%+v", all[2])
	}

	if got := s.Records(all[0].Seq); len(got) != 2 {
		t.Fatalf("增量拉取应返回 2 条，得到 %d", len(got))
	}
	if got := s.Records(all[2].Seq); len(got) != 0 {
		t.Fatalf("全部消费后增量拉取应为空，得到 %d", len(got))
	}

	// 返回切片是拷贝：外部改动不得影响 store 内部状态
	all[0].Text = "tampered"
	if s.Records(0)[0].Text != "hello" {
		t.Fatal("Records 返回的必须是快照，外部修改不应穿透")
	}
}

func TestSubmitInput(t *testing.T) {
	s := NewStore()

	// 引擎忙（无人接收）时提交失败——旧版 unbuffered chan + default 的行为
	if s.SubmitInput("busy") {
		t.Fatal("无接收方时 SubmitInput 应返回 false")
	}

	got := make(chan string, 1)
	go func() { got <- (<-s.InputChan()) }()
	// 接收方必须先真正阻塞在 channel 上，SubmitInput 的 select 才能看到它；
	// Go 没有用户态的"接收已注册"同步点，用短暂 sleep 让调度进入稳定状态
	time.Sleep(100 * time.Millisecond)
	if !s.SubmitInput("hello") {
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

func TestRunStateAndFatal(t *testing.T) {
	s := NewStore()
	if s.RunState().Running {
		t.Fatal("初始不应处于运行态")
	}
	if s.RunState().Fatal != nil {
		t.Fatal("初始不应有终态")
	}

	s.SetRunning(true)
	if !s.RunState().Running {
		t.Fatal("SetRunning(true) 后应处于运行态")
	}

	s.SetFatal(FatalError, "加载配置文件错误: x", true)
	rs := s.RunState()
	if rs.Fatal == nil || rs.Fatal.Style != FatalError || rs.Fatal.Text != "加载配置文件错误: x" || !rs.Fatal.WaitKey {
		t.Fatalf("终态快照不符：%+v", rs.Fatal)
	}
}

func TestNoticeAndStaticData(t *testing.T) {
	s := NewStore()

	if kind, _, setAt := s.Notice(); kind != NoticeNone || !setAt.IsZero() {
		t.Fatalf("初始通知槽位应为零值，得到 %v@%v", kind, setAt)
	}
	s.SetNotice(NoticeWarning, "broken")
	kind, text, setAt := s.Notice()
	if kind != NoticeWarning || text != "broken" || time.Since(setAt) > time.Minute {
		t.Fatalf("通知槽位快照不符：%v %q %v", kind, text, setAt)
	}

	if _, ok := s.StartupInfo(); ok {
		t.Fatal("StartupInfo 未就绪时应返回 false")
	}
	s.SetStartupInfo([]string{"model m"})
	lines, ok := s.StartupInfo()
	if !ok || len(lines) != 1 {
		t.Fatalf("StartupInfo 就绪后应可读，得到 %v %v", lines, ok)
	}

	s.SetSkillHelpItems([]HelpItem{{Cmd: "/foo", Desc: "bar"}})
	items := s.SkillHelpItems()
	if len(items) != 1 || items[0].Cmd != "/foo" {
		t.Fatalf("技能帮助项不符：%v", items)
	}
	// 返回的是拷贝：外部 append 不得穿透
	items = append(items, HelpItem{Cmd: "/evil"})
	if len(s.SkillHelpItems()) != 1 {
		t.Fatal("SkillHelpItems 返回的必须是快照")
	}
}

func TestCancelHook(t *testing.T) {
	s := NewStore()
	s.Cancel() // 未注册时必须是 no-op，不能 panic

	fired := false
	s.SetActiveCancel(func() { fired = true })
	s.Cancel()
	if !fired {
		t.Fatal("注册后 Cancel 应触发取消函数")
	}

	s.SetActiveCancel(nil)
	fired = false
	s.Cancel()
	if fired {
		t.Fatal("注销后 Cancel 不应再触发")
	}
}

func TestSetTodoText(t *testing.T) {
	s := NewStore()
	s.SetTodoText("[TODO] x")
	if s.TodoText() != "[TODO] x" {
		t.Fatalf("清单文本不符：%q", s.TodoText())
	}
	s.SetTodoText("") // 空串必须能写回（清单栏塌陷依据）
	if s.TodoText() != "" {
		t.Fatalf("空串应可写回：%q", s.TodoText())
	}
}
