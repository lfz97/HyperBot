package functionTools

import (
	"context"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// 工具逻辑函数
func getWeather(ctx context.Context, req struct {
	Date string `json:"Date" jsonschema:"description=日期"`
}) (map[string]any, error) {
	result := map[string]any{
		"Weather":     "晴",
		"Temperature": "25°C",
	}

	return result, nil
}

// 包装并返回工具
func CreateGetWeatherTool() tool.Tool {
	GetWeatherTool := function.NewFunctionTool(
		getWeather,
		function.WithName("getweather"),
		function.WithDescription("获取天气状态"),
	)
	return GetWeatherTool
}
