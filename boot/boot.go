package boot

import (
	"HyperBot/service/engine"
	"HyperBot/service/engine/runlog"
	"HyperBot/service/tui"
)

func Boot() {
	// runlog.Store 是两端唯一交汇点：TUI 按帧拉取渲染，引擎只写状态、从不回调。
	// 引擎 goroutine 何时就绪都不影响 TUI 先行启动——横幅、通知、错误终态
	// 都是引擎写入状态后由 TUI 在后续帧里拉到的。
	st := runlog.NewStore()
	t := tui.NewTui(st)
	go func() {
		e := engine.GetEngineService("HyperBot", st)
		e.AgentStart()
	}()
	t.Run()

}
