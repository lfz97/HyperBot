package functionTools

import (
	"context"
	"fmt"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// 工具逻辑函数
func calculator(ctx context.Context, req struct {
	Operation string  `json:"operation" jsonschema:"description=运算类型，例如 add/multiply"`
	A         float64 `json:"a" jsonschema:"description=第一个操作数"`
	B         float64 `json:"b" jsonschema:"description=第二个操作数"`
}) (map[string]interface{}, error) {
	switch req.Operation {
	case "add":
		return map[string]interface{}{"result": req.A + req.B}, nil
	case "multiply":
		return map[string]interface{}{"result": req.A * req.B}, nil
	default:
		return nil, fmt.Errorf("unsupported operation: %s", req.Operation)
	}
}

// 包装并返回工具
func CreateCalculatorTool() tool.Tool {
	calculatorTool := function.NewFunctionTool(
		calculator,
		function.WithName("calculator"),
		function.WithDescription("执行数学运算"),
	)
	return calculatorTool
}
