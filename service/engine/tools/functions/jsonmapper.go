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
}
