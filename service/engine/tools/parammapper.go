package tools

import (
	functionTools "HyperBot/service/engine/tools/functions"
	localexec "HyperBot/service/engine/tools/toolsets/localexec"
)

type ParamMapper struct {
	Name string
	In   []string
	Out  []string
}
type Mappers []*ParamMapper

func (m *Mappers) AddMapping(Name string, In []string, Out []string) {
	mapping := ParamMapper{
		Name: Name,
		In:   In,
		Out:  Out,
	}
	*m = append(*m, &mapping)
}
func GetParamMapper() *Mappers {
	mappers_p := &Mappers{}
	functionTools.InjectMapper(mappers_p)
	localexec.InjectMapper(mappers_p)
	injectTaskRunMapper(mappers_p)
	return mappers_p
}

// injectTaskRunMapper 注册框架 taskrun 工具的参数/结果提取字段。
// 工具名与字段是框架 tool/taskrun 包的公开契约（裸名、无前缀——它们经
// WithTools 挂载而非 ToolSet）。task 文本可能很长，In 只取首段渲染由渲染层
// 截断，这里不做预处理。
func injectTaskRunMapper(m *Mappers) {
	m.AddMapping("start_task_run", []string{"task", "mode"}, []string{"id", "status", "summary"})
	// list 的 runs / transcript 的 events 是"JSON 套 JSON"数组（与 ls/glob 同形），
	// 渲染层按元素展开
	m.AddMapping("list_task_runs", []string{}, []string{"runs"})
	m.AddMapping("get_task_run", []string{"id"}, []string{"id", "status", "summary", "result"})
	m.AddMapping("cancel_task_run", []string{"id"}, []string{"id", "status"})
	m.AddMapping("wait_task_run", []string{"id", "timeout_seconds"}, []string{"id", "status", "summary"})
	m.AddMapping("read_task_run_transcript", []string{"id", "limit"}, []string{"id", "status", "child_session_id"})
}
