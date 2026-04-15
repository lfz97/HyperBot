package tui

import (
	"HyperBot/bootstrap"
	"HyperBot/handler"
	"HyperBot/tui/global_object"
	"github.com/gdamore/tcell/v2"
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

// 定义颜色，遵循w3c的颜色规范，使用十六进制颜色值，确保界面风格统一且美观
var (
	bg          tcell.Color = tcell.GetColor("#1e1e1e") // 整体背景色
	SidebarBg   tcell.Color = tcell.GetColor("#252526") // 侧边栏背景色
	borderColor tcell.Color = tcell.GetColor("#3c3c3c") // 边框颜色
	StatusBarBg tcell.Color = tcell.GetColor("#2d2d2d") // 标题栏背景色
	inputAreaBg tcell.Color = tcell.GetColor("#383737") // 输入区背景色
)

// 创建一个view，专门用来显示分割线的文本视图
func createSeparator() *tview.TextView {
	line := "─" // 水平线字符，也可以用 "═"
	sep := tview.NewTextView().
		SetDynamicColors(true).
		SetText("[cyan]" + strings.Repeat(line, 300) + "[-]").
		SetTextAlign(tview.AlignCenter)
	return sep
}

func CreateConfigPage(pages *tview.Pages) tview.Primitive {
	//创建两个视图
	global_object.StatusView_p = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(false).
		SetText(banner)

	global_object.LogView_p = tview.NewTextView().
		SetDynamicColors(true). // 启用颜色
		SetScrollable(true).    // 可滚动
		SetWrap(false)          // 长行不换行

	//创建一个垂直布局，把两个视图放进去
	ConfigPageFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(global_object.StatusView_p, 14, 0, false). // 状态视图占1行
		AddItem(createSeparator(), 1, 0, false).
		AddItem(global_object.LogView_p, 0, 1, false) // 日志视图占剩余空间

	go func() {
		//初始化AgentRunner
		runner := bootstrap.Init("HyperBot")
		//如果Init都成功了，创建Agent页面
		AgentPage := createAgentPage(runner)
		global_object.App_p.QueueUpdateDraw(func() {
			//添加并切换到Agent页面
			pages.AddPage("AgentPage", AgentPage, true, true)
			pages.SwitchToPage("AgentPage")
		})
	}()
	return ConfigPageFlex
}

func createAgentPage(runner handler.AgentRunner) tview.Primitive {

	//设置标题状态栏
	global_object.StatusBar_p = tview.NewTextView()
	global_object.StatusBar_p.SetDynamicColors(true).SetWrap(false).SetText("[green]HyperBot[-]")
	global_object.StatusBar_p.SetTextAlign(tview.AlignCenter)
	global_object.StatusBar_p.SetBackgroundColor(StatusBarBg)

	//设置Agent消息显示区
	global_object.AgentMessageView_p = tview.NewTextView().
		SetDynamicColors(true). // 启用颜色
		SetScrollable(true).    // 可滚动
		SetWrap(true)

	global_object.AgentMessageView_p.SetBackgroundColor(bg)      // 设置背景颜色
	global_object.AgentMessageView_p.SetBorder(true)             // 设置边框
	global_object.AgentMessageView_p.SetBorderColor(borderColor) // 设置边框颜色

	//设置底部输入区
	global_object.InputArea_p = tview.NewTextArea().
		SetLabel(`⇒ `).
		SetWrap(true)
	global_object.InputArea_p.SetBackgroundColor(inputAreaBg)
	global_object.InputArea_p.SetTextStyle(tcell.StyleDefault.
		Background(inputAreaBg).      // 输入区背景色
		Foreground(tcell.ColorWhite)) // 文字颜色

	//设置左侧命令提示区
	global_object.Sidebar_p = tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(true)
	global_object.Sidebar_p.SetBackgroundColor(SidebarBg)
	global_object.Sidebar_p.SetBorder(true)
	global_object.Sidebar_p.SetBorderColor(borderColor)

	//设置布局
	//设置中间的sidebar+Agent消息区布局
	MiddleFlex_p := tview.NewFlex()
	MiddleFlex_p.AddItem(global_object.Sidebar_p, 20, 0, false)         // 左侧的命令提示区占20列
	MiddleFlex_p.AddItem(global_object.AgentMessageView_p, 0, 1, false) // 消息视图占剩余空间

	//设置整体布局
	MainFlex := tview.NewFlex().SetDirection(tview.FlexRow)
	MainFlex.SetBackgroundColor(bg)
	MainFlex.AddItem(global_object.StatusBar_p, 1, 0, false) // 顶部的状态栏占2行
	MainFlex.AddItem(MiddleFlex_p, 0, 1, false)              // 中间的sidebar+Agent消息区占剩余空间
	MainFlex.AddItem(global_object.InputArea_p, 1, 0, true)  // 底部的输入区占2行

	go func() {
		bootstrap.AgentStart(runner)
	}()

	return MainFlex
}

// drawSplitLineVertical 是一个自定义的绘制函数，用于在指定位置绘制一条垂直分割线
func drawSplitLineVertical(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
	for cy := y; cy < y+height; cy++ {
		// 浅灰色：0x888888 是中等灰，0x666666 更深，0xaaaaaa 更浅
		screen.SetContent(x, cy, '│', nil, tcell.StyleDefault.Foreground(tcell.NewHexColor(0x888888)))
	}
	return x, y, width, height
}

// drawSplitLineHorizontal 是一个自定义的绘制函数，用于在指定位置绘制一条水平分割线
func drawSplitLineHorizontal(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
	for cx := x; cx < x+width; cx++ {
		screen.SetContent(cx, y, '─', nil, tcell.StyleDefault.Foreground(tcell.NewHexColor(0x888888)))
	}
	return x, y, width, height
}

func TuiInit() {
	// 1. 创建应用实例和页面容器
	global_object.App_p = tview.NewApplication()
	pages := tview.NewPages()
	pages.AddPage("config", CreateConfigPage(pages), true, true) // 初始显示配置页

	// 4. 设置应用根组件并启动
	global_object.App_p.SetRoot(pages, true) // true = 全屏模式
	global_object.App_p.EnableMouse(true)    //允许接收鼠标事件

}
