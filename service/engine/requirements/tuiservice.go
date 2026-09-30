package requirements

import "HyperBot/utils/pretty"

// 需要一个tui服务注入如下方法。用于tui和engine解耦
type TuiService interface {
	AddHelpItems(items []map[string]string)
	ClearAppFuncTrigger()
	PrintToMsgView(content []pretty.Span, clear bool)
	MarkdownDelta(content string)
	MarkdownDone()
	ListenUserInput() chan string
	SetAgentRunning(running bool)
	ShowNotice(msg pretty.Span)
	ShowStartupBanner(infoLines []string)
	SetTodoText(text string)
	SetAppFuncTriggerWithEsc(f func())
	ShowErrorInMsgViewAndExit(errmsg []pretty.Span)
	ShowMsgAndExitNoTrigger(msg []pretty.Span)
	ShowSuccessInMsgViewAndExit(sussessmsg string)
	ResetHelpItems()
}
