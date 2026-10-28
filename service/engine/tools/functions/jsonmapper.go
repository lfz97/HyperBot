package functionTools

type Mappers interface {
	AddMapping(Name string, In []string, Out []string)
}

func InjectMapper(m Mappers) {
	m.AddMapping(writeFileToolName, []string{"Path"}, []string{"BytesWritten"})
	m.AddMapping(readFileToolName, []string{"Path"}, []string{"ReadLength"})
	m.AddMapping(editFileToolName, []string{"Path"}, []string{"Diff"})
	// search_in_file 的结果是数字键（"0"~"N" 的逐条匹配），无法逐字段登记，
	// Out 传 nil 走兜底：数字键自然排序、每条匹配一行
	m.AddMapping(grepToolName, []string{"Path", "Regex"}, nil)
	m.AddMapping(deleteFileToolName, []string{"Path"}, []string{"Deleted"})
	m.AddMapping(fileStatToolName, []string{"Path"}, []string{"Name", "Size", "IsDir", "Mode", "ModeTime"})
	m.AddMapping(diffToolName, []string{"PathA", "PathB"}, []string{"Diff"})
	// ls/glob 的值是"JSON 套 JSON"（数组字符串），渲染层会展开成每元素一行
	m.AddMapping(lsToolName, []string{"Path"}, []string{"Files"})
	m.AddMapping(mkdirToolName, []string{}, []string{"Created"})
	m.AddMapping(cpToolName, []string{}, []string{"Copied"})
	m.AddMapping(mvToolName, []string{}, []string{"Moved", "OldPath", "NewPath"})
	m.AddMapping(globToolName, []string{"Regex", "Root", "Depth"}, []string{"Matches"})
	// 框架内置工具：todo_write 的入参是整份清单（JSON 数组，逐字段提取无意义），
	// 只展示工具返回的 nudge 文案。
	m.AddMapping(todoWriteToolName, []string{}, []string{"message"})
}
