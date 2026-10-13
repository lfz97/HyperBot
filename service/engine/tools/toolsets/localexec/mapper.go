package localexec

// 框架以工具集名为前缀给工具名加前缀（LocalExec_<tool>）。
const localExecToolSetName = "LocalExec"

const (
	runToolName       = "run"
	statusToolName    = "status"
	outputToolName    = "output"
	interveneToolName = "intervene"
	killToolName      = "kill"
)

// Mappers 由 tools 包实现（*tools.Mappers 满足本接口）；localexec 自定义同形接口，避免跨包依赖。
type Mappers interface {
	AddMapping(Name string, In []string, Out []string)
}

// InjectMapper 注册 localexec 各工具的参数/结果提取字段。工具名为框架加前缀后的实际名。
func InjectMapper(m Mappers) {
	prefix := localExecToolSetName + "_"
	// run：标量字段在前、Output 块殿后（渲染层块独立于标量行，旧的
	// "Id 放前防截断"考量已不适用）
	m.AddMapping(prefix+runToolName, []string{"Process", "Args"}, []string{"Status", "ExitCode", "Id", "Output"})
	m.AddMapping(prefix+statusToolName, []string{"Id"}, []string{"Status", "ExitCode", "Id", "Pid", "StatusAll"})
	// output：工具的本体就是取输出，Out 必须含 Output；落盘超阈值时
	// Note/OutputFile 告知 agent 完整内容的位置
	m.AddMapping(prefix+outputToolName, []string{"Id", "Stream"}, []string{"Id", "Output", "Note", "OutputFile"})
	m.AddMapping(prefix+interveneToolName, []string{"Id", "Signal"}, []string{"Msg"})
	m.AddMapping(prefix+killToolName, []string{"Id"}, []string{"Status"})
}
