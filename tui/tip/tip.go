package tip

import (
	"HyperBot/utils/pretty"
	"fmt"
)

// SidebarUserInputTip 返回侧边栏的用户输入提示信息
func SidebarUserInputTip() string {

	coloredtip := fmt.Sprintf(`• %s: 开始新对话
• %s: 结束对话并退出程序
• %s: 提交输入内容`,
		pretty.TColoredText(pretty.TColorCyan, "/new"),
		pretty.TColoredText(pretty.TColorCyan, "/exit"),
		pretty.TColoredText(pretty.TColorGreen, "Ctrl+Enter"))
	return coloredtip
}
