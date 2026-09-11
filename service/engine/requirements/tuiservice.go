package requirements

// 需要一个tui服务注入如下方法。用于tui和engine解耦
type TuiService interface {
	AddHelpItems(items []map[string]string)
	ClearAppFuncTrigger()
	PrintToMsgView(content string, clear bool)
	ListenUserInput() chan string
	SetAgentRunning(running bool)
	ShowNotice(msg string)
	ShowStartupBanner(infoLines []string)
	SetTodoText(text string)
	SetAppFuncTriggerWithEsc(f func())
	ShowErrorInMsgViewAndExit(errmsg string)
	ShowMsgAndExitNoTrigger(msg string)
	ShowSuccessInMsgViewAndExit(sussessmsg string)
	RenderMarkdown(in string) (string, error)
	ResetHelpItems()
}
