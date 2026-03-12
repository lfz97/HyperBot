package handler

import (
	"fmt"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func printMessage(Choice model.Choice, startReasoning *bool, stream bool) {

	if stream {
		//------------------处理流式的响应---------------------------------------------------------------------------
		if Choice.Delta.ReasoningContent != "" && !(*startReasoning) {

			fmt.Printf("%s%s%s", colorGreen, "\n开始推理...\n", colorReset)
			*startReasoning = true

		} else if Choice.Delta.ReasoningContent != "" && (*startReasoning) {

			fmt.Printf("%s%s%s", colorYellow, Choice.Delta.ReasoningContent, colorReset)

		} else if Choice.Delta.ReasoningContent == "" && (*startReasoning) {
			*startReasoning = false

			fmt.Printf("%s%s%s", colorGreen, "\n推理结束，开始输出结果...\n", colorReset)

		}
		if Choice.Delta.Content != "" {
			fmt.Printf("%s", Choice.Delta.Content)
		}

	} else {
		//------------------处理非流式的响应---------------------------------------------------------------------------
		//处理思考信息
		if Choice.Message.ReasoningContent != "" {

			fmt.Printf("%s%s%s", colorGreen, "\n开始推理...\n", colorReset)
			fmt.Printf("%s%s%s", colorYellow, Choice.Message.ReasoningContent, colorReset)
			fmt.Printf("%s%s%s", colorGreen, "\n推理结束，开始输出结果...\n", colorReset)

		}
		if Choice.Message.Content != "" {
			fmt.Printf("%s", Choice.Message.Content)

		}
	}

	/*------------------此处统一处理工具信息*---------------------------------------------------------------------------*/

	//流式的
	if len(Choice.Delta.ToolCalls) != 0 {
		for _, toolCall := range Choice.Delta.ToolCalls {
			fmt.Printf("%sToolCall: %s%s\n", colorBlue, toolCall.Function.Name, colorReset)
			fmt.Printf("%sToolCall Arguments: %s%s\n", colorBlue, string(toolCall.Function.Arguments), colorReset)
		}
	}

	//非流式的
	if len(Choice.Message.ToolCalls) != 0 {
		for _, toolCall := range Choice.Message.ToolCalls {
			fmt.Printf("%sToolCall: %s%s\n", colorBlue, toolCall.Function.Name, colorReset)
			fmt.Printf("%sToolCall Arguments: %s%s\n", colorBlue, string(toolCall.Function.Arguments), colorReset)
		}
	}
}

// 把Choice中的消息按照角色进行汇聚，存储到MsgTmpMap map[int]*model.Message中
func gatherMessage(Choice model.Choice, MsgTmpMap *map[int]*model.Message, Index *int, Role *model.Role, stream bool) {

	if stream {
		if Choice.Delta.Role != "" {

			//当Role与Choice.Delta.Role不同时，说明角色切换了，需要新建一个消息
			if Choice.Delta.Role != *Role {
				*Role = Choice.Delta.Role
				*Index += 1
				(*MsgTmpMap)[*Index] = &model.Message{
					Role: Choice.Delta.Role,
				}
				if Choice.Delta.Content == "" && Choice.Delta.ReasoningContent != "" {
					(*MsgTmpMap)[*Index].Content = Choice.Delta.ReasoningContent
				} else if Choice.Delta.Content != "" && Choice.Delta.ReasoningContent == "" {
					(*MsgTmpMap)[*Index].Content = Choice.Delta.Content
				} else if Choice.Delta.Content != "" && Choice.Delta.ReasoningContent != "" {
					(*MsgTmpMap)[*Index].Content = Choice.Delta.ReasoningContent + Choice.Delta.Content
				}
				//delta 和 message中都可能包含工具信息，都要处理
				{
					if Choice.Delta.ToolID != "" {
						(*MsgTmpMap)[*Index].ToolID = Choice.Delta.ToolID
					}
					if len(Choice.Delta.ToolCalls) != 0 {
						(*MsgTmpMap)[*Index].ToolCalls = append((*MsgTmpMap)[*Index].ToolCalls, Choice.Delta.ToolCalls...)
					}
					if Choice.Message.ToolID != "" {
						(*MsgTmpMap)[*Index].ToolID = Choice.Message.ToolID
					}
					if len(Choice.Message.ToolCalls) != 0 {
						(*MsgTmpMap)[*Index].ToolCalls = append((*MsgTmpMap)[*Index].ToolCalls, Choice.Message.ToolCalls...)
					}
				}

			} else if Choice.Delta.Role == *Role {

				if Choice.Delta.ReasoningContent != "" && Choice.Delta.Content == "" {
					(*MsgTmpMap)[*Index].Content += Choice.Delta.ReasoningContent
				} else if Choice.Delta.ReasoningContent == "" && Choice.Delta.Content != "" {
					(*MsgTmpMap)[*Index].Content += Choice.Delta.Content
				} else if Choice.Delta.ReasoningContent != "" && Choice.Delta.Content != "" {
					(*MsgTmpMap)[*Index].Content += Choice.Delta.ReasoningContent + Choice.Delta.Content
				}
				//delta 和 message中都可能包含工具信息，都要处理
				{
					if len(Choice.Delta.ToolCalls) != 0 {
						(*MsgTmpMap)[*Index].ToolCalls = append((*MsgTmpMap)[*Index].ToolCalls, Choice.Delta.ToolCalls...)
					}
					if len(Choice.Message.ToolCalls) != 0 {
						(*MsgTmpMap)[*Index].ToolCalls = append((*MsgTmpMap)[*Index].ToolCalls, Choice.Message.ToolCalls...)
					}
				}

			}
		}
	} else {
		if Choice.Message.Role != "" {

			//当Role与Choice.Delta.Role不同时，说明角色切换了，需要新建一个消息
			if Choice.Message.Role != *Role {
				*Role = Choice.Message.Role
				*Index += 1
				(*MsgTmpMap)[*Index] = &model.Message{
					Role: Choice.Message.Role,
				}
				if Choice.Message.Content == "" && Choice.Message.ReasoningContent != "" {
					(*MsgTmpMap)[*Index].Content = Choice.Message.ReasoningContent
				} else if Choice.Message.Content != "" && Choice.Message.ReasoningContent == "" {
					(*MsgTmpMap)[*Index].Content = Choice.Message.Content
				} else if Choice.Message.Content != "" && Choice.Message.ReasoningContent != "" {
					(*MsgTmpMap)[*Index].Content = Choice.Message.ReasoningContent + Choice.Message.Content
				}
				//delta 和 message中都可能包含工具信息，都要处理
				{
					if Choice.Delta.ToolID != "" {
						(*MsgTmpMap)[*Index].ToolID = Choice.Delta.ToolID
					}
					if len(Choice.Delta.ToolCalls) != 0 {
						(*MsgTmpMap)[*Index].ToolCalls = append((*MsgTmpMap)[*Index].ToolCalls, Choice.Delta.ToolCalls...)
					}
					if Choice.Message.ToolID != "" {
						(*MsgTmpMap)[*Index].ToolID = Choice.Message.ToolID
					}
					if len(Choice.Message.ToolCalls) != 0 {
						(*MsgTmpMap)[*Index].ToolCalls = append((*MsgTmpMap)[*Index].ToolCalls, Choice.Message.ToolCalls...)
					}
				}
			} else if Choice.Message.Role == *Role {

				if Choice.Message.ReasoningContent != "" && Choice.Message.Content == "" {
					(*MsgTmpMap)[*Index].Content += Choice.Message.ReasoningContent
				} else if Choice.Message.ReasoningContent == "" && Choice.Message.Content != "" {
					(*MsgTmpMap)[*Index].Content += Choice.Message.Content
				} else if Choice.Message.ReasoningContent != "" && Choice.Message.Content != "" {
					(*MsgTmpMap)[*Index].Content += Choice.Message.ReasoningContent + Choice.Message.Content
				}
				//delta 和 message中都可能包含工具信息，都要处理
				{
					if len(Choice.Delta.ToolCalls) != 0 {
						(*MsgTmpMap)[*Index].ToolCalls = append((*MsgTmpMap)[*Index].ToolCalls, Choice.Delta.ToolCalls...)
					}
					if len(Choice.Message.ToolCalls) != 0 {
						(*MsgTmpMap)[*Index].ToolCalls = append((*MsgTmpMap)[*Index].ToolCalls, Choice.Message.ToolCalls...)
					}
				}
			}
		}
	}

}
