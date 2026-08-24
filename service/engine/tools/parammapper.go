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
	return mappers_p
}
