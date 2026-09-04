package functionTools

type Mappers interface {
	AddMapping(Name string, In []string, Out []string)
}

func InjectMapper(m Mappers) {
	m.AddMapping(dateToolName, []string{}, []string{"Datetime"})
	m.AddMapping(writeFileToolName, []string{"Path"}, []string{"BytesWritten"})
	m.AddMapping(readFileToolName, []string{"Path"}, []string{"ReadLength"})
	m.AddMapping(editFileToolName, []string{"Path"}, []string{"Diff"})
	m.AddMapping(searchInFileToolName, []string{"Path", "Regex"}, []string{})
	m.AddMapping(deleteFileToolName, []string{"Path"}, []string{"Deleted"})
	m.AddMapping(fileStatToolName, []string{"Path"}, []string{"Name", "Size", "IsDir", "Mode", "ModeTime"})
	m.AddMapping(diffToolName, []string{"PathA", "PathB"}, []string{"Diff"})
	m.AddMapping(pwdToolName, []string{}, []string{"PWD"})
	m.AddMapping(cdToolName, []string{}, []string{"CwdNow"})
	m.AddMapping(lsToolName, []string{"Path"}, []string{})
	m.AddMapping(mkdirToolName, []string{}, []string{"Created"})
	m.AddMapping(cpToolName, []string{}, []string{"Copied"})
	m.AddMapping(mvToolName, []string{}, []string{"Moved", "OldPath", "NewPath"})
	m.AddMapping(globToolName, []string{"Regex", "Root", "Depth"}, []string{})
	// 框架内置工具：todo_write 的入参是整份清单（JSON 数组，逐字段提取无意义），
	// 只展示工具返回的 nudge 文案。
	m.AddMapping(todoWriteToolName, []string{}, []string{"message"})
}
