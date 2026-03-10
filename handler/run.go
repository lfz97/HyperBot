package handler

import (
	"context"
	"fmt"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpcagent/myutils"
)

const (
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorReset  = "\033[0m"
)

// AgentRunOnce 处理单轮对话，返回历史消息
func AgentRunOnce(Ctx context.Context, r runner.Runner, sessionID string, userID string, requestID string, msg string) []model.Message {

	eventChan, err := r.Run(Ctx, userID, sessionID, model.NewUserMessage(msg), agent.WithRequestID(requestID))
	if err != nil {
		panic(err)

	}

	//初始化历史消息slice，把用户输入作为第一条消息
	history := []model.Message{
		model.Message{
			Role:    model.RoleUser,
			Content: msg,
		},
	}

	MsgTmpMap := map[int]*model.Message{} //定义一个临时map用来存储消息，key为Index，value为消息指针
	Index := 0                            //指向消息在map中的位置
	var Role model.Role                   //记录消息对应的角色

	startReasoning := false
	for event := range eventChan {

		if event.Error != nil {
			fmt.Printf("错误: %s\n", event.Error.Message)
			continue
		}

		if len((*(*event).Response).Choices) > 0 {

			Choice := (*(*event).Response).Choices[0]
			/*------------------处理流式的响应*---------------------------------------------------------------------------*/

			//处理思考信息
			if Choice.Delta.ReasoningContent != "" && !startReasoning {

				fmt.Printf("%s%s%s", colorGreen, "\n开始推理...\n", colorReset)
				startReasoning = true

			} else if Choice.Delta.ReasoningContent != "" && startReasoning {

				fmt.Printf("%s%s%s", colorYellow, Choice.Delta.ReasoningContent, colorReset)

			} else if Choice.Delta.ReasoningContent == "" && startReasoning {
				startReasoning = false

				fmt.Printf("%s%s%s", colorGreen, "\n推理结束，开始输出结果...\n", colorReset)

			}

			//处理正文

			if Choice.Delta.Content != "" {
				fmt.Printf("%s", Choice.Delta.Content)
			}

			/*------------------处理非流式的响应*---------------------------------------------------------------------------*/
			//处理思考信息
			if Choice.Message.ReasoningContent != "" {

				fmt.Printf("%s%s%s", colorGreen, "\n开始推理...\n", colorReset)
				fmt.Printf("%s%s%s", colorYellow, Choice.Message.ReasoningContent, colorReset)
				fmt.Printf("%s%s%s", colorGreen, "\n推理结束，开始输出结果...\n", colorReset)

			}

			//处理正文
			if Choice.Message.Content != "" {
				fmt.Printf("%s", Choice.Message.Content)

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

			/*------------------此处汇聚消息---------------------------------------------------------------------------*/
			if Choice.Delta.Role != "" {

				//当Role与Choice.Delta.Role不同时，说明角色切换了，需要新建一个消息
				if Choice.Delta.Role != Role {
					Role = Choice.Delta.Role
					Index += 1
					MsgTmpMap[Index] = &model.Message{
						Role:    Choice.Delta.Role,
						Content: Choice.Delta.Content,
					}
					if Choice.Message.ToolID != "" {
						(*MsgTmpMap[Index]).ToolID = Choice.Delta.ToolID
					}
					if len(Choice.Delta.ToolCalls) != 0 {
						(*MsgTmpMap[Index]).ToolCalls = append((*MsgTmpMap[Index]).ToolCalls, Choice.Delta.ToolCalls...)
					}

				} else if Choice.Delta.Role == Role {
					(*MsgTmpMap[Index]).Content += Choice.Delta.Content
					if len(Choice.Delta.ToolCalls) != 0 {
						(*MsgTmpMap[Index]).ToolCalls = append((*MsgTmpMap[Index]).ToolCalls, Choice.Delta.ToolCalls...)
					}
				}
			} else if Choice.Message.Role != "" {

				//当Role与Choice.Delta.Role不同时，说明角色切换了，需要新建一个消息
				if Choice.Message.Role != Role {
					Role = Choice.Message.Role
					Index += 1
					MsgTmpMap[Index] = &model.Message{
						Role:    Choice.Message.Role,
						Content: Choice.Message.Content,
					}
					if Choice.Message.ToolID != "" {
						(*MsgTmpMap[Index]).ToolID = Choice.Message.ToolID
					}
					if len(Choice.Message.ToolCalls) != 0 {
						(*MsgTmpMap[Index]).ToolCalls = append((*MsgTmpMap[Index]).ToolCalls, Choice.Message.ToolCalls...)
					}
				} else if Choice.Message.Role == Role {
					(*MsgTmpMap[Index]).Content += Choice.Message.Content
					if len(Choice.Message.ToolCalls) != 0 {
						(*MsgTmpMap[Index]).ToolCalls = append((*MsgTmpMap[Index]).ToolCalls, Choice.Message.ToolCalls...)
					}
				}
			}

		}
		// event.IsRunnerCompletion()判断是否完成输出
		if event.IsRunnerCompletion() {
			break
		}

	}

	//将MsgTmpMap中的消息按照顺序追加到history中
	for _, msg_p := range MsgTmpMap {
		history = append(history, *msg_p)
	}
	return history
}

type EndReason struct {
	Code   int
	Reason string
}

func AgentRunIteratively(Ctx context.Context, r runner.Runner, sessionID string, userID string, requestID string) (*EndReason, []model.Message) {
	historyAll := []model.Message{}
	fmt.Println(colorBlue + "\n新对话已开始" + colorReset)
	EndReason := EndReason{}
	for {
		userPrompt, err := myutils.StdinInput(colorBlue + "\nUser(欲退出请输入" + colorGreen + "`/exit`,新对话请输入`/new`):" + colorReset)
		if err != nil {
			fmt.Printf(colorRed+"读取输入错误: %v\n"+colorReset, err)
			continue
		}
		if userPrompt == "/exit" {
			fmt.Println(colorBlue + "对话已结束" + colorReset)
			EndReason.Code = 0
			EndReason.Reason = "用户主动结束对话"
			break

		} else if userPrompt == "/new" {
			fmt.Println(colorBlue + "新对话已开始" + colorReset)
			EndReason.Code = 1
			EndReason.Reason = "用户主动开始新对话"
			break
		}
		//因为runner内部自动追加了历史消息，所以这里直接覆盖即可
		historyAll = append(historyAll, AgentRunOnce(Ctx, r, sessionID, userID, requestID, userPrompt)...)

	}
	return &EndReason, historyAll
}
