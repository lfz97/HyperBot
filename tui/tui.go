package tui

import (
	"HyperBot/bootstrap"
	"HyperBot/handler"
	"github.com/rivo/tview"
	"strings"
)

const banner string = `$$   $$ |$$\                                         $$$$$$$\             $$\
  $$ |  $$ |                                        $$  __$$\            $$ |
  $$ |  $$ |$$\   $$\  $$$$$$\   $$$$$$\   $$$$$$\  $$ |  $$ | $$$$$$\ $$$$$$\
  $$$$$$$$ |$$ |  $$ |$$  __$$\ $$  __$$\ $$  __$$\ $$$$$$$\ |$$  __$$\\_$$  _|
  $$  __$$ |$$ |  $$ |$$ /  $$ |$$$$$$$$ |$$ |  \__|$$  __$$\ $$ /  $$ | $$ |
  $$ |  $$ |$$ |  $$ |$$ |  $$ |$$   ____|$$ |      $$ |  $$ |$$ |  $$ | $$ |$$\
  $$ |  $$ |\$$$$$$$ |$$$$$$$  |\$$$$$$$\ $$ |      $$$$$$$  |\$$$$$$  | \$$$$  |
  \__|  \__| \____$$ |$$  ____/  \_______|\__|      \_______/  \______/   \____/
            $$\   $$ |$$ |
            \$$$$$$  |$$ |
             \______/ \__|`

// 创建一个view，专门用来显示分割线的文本视图
func createSeparator() *tview.TextView {
	line := "─" // 水平线字符，也可以用 "═"
	sep := tview.NewTextView().
		SetDynamicColors(true).
		SetText("[cyan]" + strings.Repeat(line, 300) + "[-]").
		SetTextAlign(tview.AlignCenter)
	return sep
}

func CreateConfigPage(app *tview.Application, pages *tview.Pages) tview.Primitive {
	//创建两个视图
	StatusView_p := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(false).
		SetText(banner)

	LogView_p := tview.NewTextView().
		SetDynamicColors(true). // 启用颜色
		SetScrollable(true).    // 可滚动
		SetWrap(false)          // 长行不换行

	//创建一个垂直布局，把两个视图放进去
	ConfigPageFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(StatusView_p, 14, 0, false). // 状态视图占1行
		AddItem(createSeparator(), 1, 0, false).
		AddItem(LogView_p, 0, 1, false) // 日志视图占剩余空间

	go func(app_p *tview.Application, view_p *tview.TextView) {
		//初始化AgentRunner
		runner := bootstrap.Init("HyperBot", app_p, view_p)
		//如果Init都成功了，创建Agent页面
		AgentPage := createAgentPage(app_p, runner)
		app_p.QueueUpdateDraw(func() {
			//添加并切换到Agent页面
			pages.AddPage("AgentPage", AgentPage, true, true)
			pages.SwitchToPage("AgentPage")
		})
	}(app, LogView_p)
	return ConfigPageFlex
}

func createAgentPage(app *tview.Application, runner handler.AgentRunner) tview.Primitive {
	StatusBar := tview.NewTextView().
		SetDynamicColors(true).
		SetText("[green]HyperBot[-]").
		SetTextAlign(tview.AlignCenter)
	AgentMessageView_p := tview.NewTextView().
		SetDynamicColors(true). // 启用颜色
		SetScrollable(true).    // 可滚动
		SetWrap(true)

	InputArea := tview.NewTextArea().
		SetLabel("> ").
		SetWrap(true)
	AgentPageFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(StatusBar, 1, 0, false).          // 状态栏固定1行
		AddItem(AgentMessageView_p, 0, 1, false). // 消息视图占剩余空间
		AddItem(InputArea, 3, 0, true)            // 输入框固定3行，初始时获得焦点

	go func(app_p *tview.Application, AgentMessageView_p *tview.TextView, InputArea_p *tview.TextArea) {

		bootstrap.AgentStart(app_p, AgentMessageView_p, InputArea_p, runner)
	}(app, AgentMessageView_p, InputArea)

	return AgentPageFlex
}
