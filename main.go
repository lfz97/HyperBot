package main

import (
	"HyperBot/tui"
	"github.com/rivo/tview"
)

func main() {
	// 1. 创建应用实例和页面容器
	app := tview.NewApplication()
	pages := tview.NewPages()
	pages.AddPage("config", tui.CreateConfigPage(app, pages), true, true) // 初始显示配置页

	// 4. 设置应用根组件并启动
	app.SetRoot(pages, true) // true = 全屏模式
	app.EnableMouse(true)    //允许接收鼠标事件

	if err := app.Run(); err != nil {
		panic(err)
	}
}
