package boot

import (
	"HyperBot/service/engine"
	"HyperBot/service/tui"
)

func Boot() {
	// 引擎轻量构造后交给 TUI。这里用 go 是为了不阻塞 TUI 启动：Init 的错误
	// 路径自带 fatal 兜底（见 Init 的 defer），终态消息要靠已在运行的 TUI 渲染；
	// Init 成功返回即完全就绪（会话/横幅/boot 态），无需 post-Init 收尾。
	e := engine.GetEngineService("HyperBot")
	t := tui.NewTui(e)
	go e.Init()
	t.Run()

}
