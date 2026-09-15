package cronagent

const cronAgentToolSetName = "CronAgent"

const (
	createAgentToolName    = "create"
	startAgentToolName     = "start"
	stopAgentToolName      = "stop"
	removeAgentToolName    = "remove"
	getAgentStatusToolName = "status"
	getAllStatusToolName   = "allStatus"
	getAgentOutputToolName = "output"
	clearAllToolName       = "clearAll"
)

// Mappers 由 tools 包实现（*tools.Mappers 满足本接口）；cronagent 自定义同形接口，避免跨包依赖。
type Mappers interface {
	AddMapping(Name string, In []string, Out []string)
}

// InjectMapper 注册 cronagent 各工具的参数/结果提取字段。工具名为框架加前缀后的实际名，
// In 对应请求结构体的 json tag，Out 对应结果的 json 路径。
func InjectMapper(m Mappers) {
	prefix := cronAgentToolSetName + "_"
	m.AddMapping(prefix+createAgentToolName, []string{"Cronexpr", "Prompt"}, []string{"Id"})
	m.AddMapping(prefix+startAgentToolName, []string{"Id"}, []string{})
	m.AddMapping(prefix+stopAgentToolName, []string{"Id"}, []string{})
	m.AddMapping(prefix+removeAgentToolName, []string{"Id"}, []string{})
	m.AddMapping(prefix+getAgentStatusToolName, []string{"Id"}, []string{})
	m.AddMapping(prefix+getAllStatusToolName, []string{}, []string{})
	m.AddMapping(prefix+getAgentOutputToolName, []string{"Id", "Window"}, []string{})
	m.AddMapping(prefix+clearAllToolName, []string{}, []string{})
}
