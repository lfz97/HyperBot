package requirements

import (
	"charm.land/glamour/v2"
	"context"
)

// 需要一个tui服务注入如下方法。用于tui和engine解耦
type TuiService interface {
	AddHelpItems(items []map[string]string)
	ClearAppFuncTrigger()
	PrintToMsgView(content string, clear bool)
	ReadInputAreaPromptWithEnter()
	SetAppFuncTriggerWithEsc(f func())
	ShowErrorInMsgViewAndExit(errmsg string)
	ShowMsgAndExitNoTrigger(msg string)
	ShowSuccessInMsgView(sussessmsg string)
	ShowSuccessInMsgViewAndExit(sussessmsg string)
	StatusBarScrollingTip(ctx context.Context, tip string, TColor string)
	StatusBarUserTip(s string)
	NewGlamourRenderer() *glamour.TermRenderer
	ResetHelpItems()
	InputChannel() chan string
}
