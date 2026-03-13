package handler

import (
	"HyperBot/myutils"
	"context"
	"fmt"
	"os"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// 交互式对话
func AgentRunIteratively(sigChan chan os.Signal, Ctx context.Context, AgentRunner AgentRunner, sessionID string, userID string, requestID string, MsgContext EndInfo) *EndInfo {

	//定义要返回的结束信息，初始值为正常结束
	EndInfo := EndInfo{
		Code:           ExitCodeNormal,
		Reason:         "对话正常结束",
		RecoverMessage: []model.Message{},
	}
	// MsgBuffer用于按需继承历史消息
	MsgBuffer := []model.Message{}

	//定义一个可取消的context。启用一个独立goroutine当捕获到退出信号时，取消这个上下文，从而停止接收输入和消息
	ctx, cancel := context.WithCancel(Ctx)
	go func() {
		select {
		case sig := <-sigChan:
			fmt.Printf(colorRed+"\n捕获到信号: %v，退出本次会话...\n"+colorReset, sig)
			cancel()
		case <-ctx.Done(): //当对话正常结束时，ctx.Done()会被触发，此时直接返回，释放goroutine资源
			return
		}
	}()

	defer AgentRunner.Runner.Close()
	if MsgContext.Code == ExitCodeNormal || MsgContext.Code == ExitCodeNew {
		fmt.Println(colorBlue + "\n新对话已开始" + colorReset)
	} else if MsgContext.Code == ExitCodeError {
		fmt.Println(colorYellow + "\n对话出现错误:" + MsgContext.Reason + colorReset)
		fmt.Println(colorBlue + "\n尝试继续对话" + colorReset)

	} else if MsgContext.Code == ExitCodeInt {
		fmt.Println(colorYellow + "\n对话被中断:" + MsgContext.Reason + colorReset)
		fmt.Println(colorBlue + "\n尝试继续对话" + colorReset)
	} else if MsgContext.Code == ExitCodeExit {
		fmt.Println(colorYellow + "\n对话已结束:" + MsgContext.Reason + colorReset)
		cancel() //取消上下文，停止接收输入和消息
		return &EndInfo
	}

	for {

		//如果是正常或新对话，不继承历史消息的交互式运行
		if MsgContext.Code == ExitCodeNormal || MsgContext.Code == ExitCodeNew {
			userPrompt, err := myutils.StdinInput(colorBlue + "\nUser(欲退出请输入" + colorGreen + "`/exit`,新对话请输入`/new`):" + colorReset)
			if err != nil {
				//读取输入错误一般是在stdin阻塞读的时候用户按了Ctrl+C导致的，此时将结束状态设定为中断，并保存历史消息直接返回
				fmt.Printf(colorRed+"读取输入错误: %v\n"+colorReset, err)
				EndInfo.Code = ExitCodeInt
				EndInfo.Reason = fmt.Sprintf("读取输入错误: %v", err)
				EndInfo.RecoverMessage = MsgContext.RecoverMessage
				return &EndInfo
			}
			if userPrompt == "/exit" {
				fmt.Println(colorBlue + "对话已结束" + colorReset)
				EndInfo.Code = ExitCodeExit
				EndInfo.Reason = "用户主动结束对话"
				cancel() //取消上下文，停止接收输入和消息
				return &EndInfo

			} else if userPrompt == "/new" {
				EndInfo.Code = ExitCodeNew
				EndInfo.Reason = "用户主动开始新对话"
				cancel() //取消上下文，停止接收输入和消息
				return &EndInfo
			} else if userPrompt == "" {
				continue
			}
			MsgBuffer = append(MsgBuffer, model.Message{
				Role:    model.RoleUser,
				Content: userPrompt,
			})

			//如果是因为错误中断的对话，则继承历史消息，并设定默认提示词
		} else if MsgContext.Code == ExitCodeError {
			MsgBuffer = MsgContext.RecoverMessage
			if MsgBuffer[len(MsgBuffer)-1].Role == model.RoleUser {
				if MsgBuffer[len(MsgBuffer)-1].Content != "继续" {
					MsgBuffer = append(MsgBuffer, model.Message{
						Role:    model.RoleUser,
						Content: "继续",
					})
				}
			} else if MsgBuffer[len(MsgBuffer)-1].Role != model.RoleUser {
				MsgBuffer = append(MsgBuffer, model.Message{
					Role:    model.RoleUser,
					Content: "继续",
				})
			}
			//如果是因为中断信号导致的对话中断，则继承历史消息的交互式运行
		} else if MsgContext.Code == ExitCodeInt {
			userPrompt, err := myutils.StdinInput(colorBlue + "\nUser(欲退出请输入" + colorGreen + "`/exit`,新对话请输入`/new`):" + colorReset)
			if err != nil {
				//读取输入错误一般是在stdin阻塞读的时候用户按了Ctrl+C导致的，此时将结束状态设定为中断，并保存历史消息直接返回
				fmt.Printf(colorRed+"读取输入错误: %v\n"+colorReset, err)

				EndInfo.Code = ExitCodeInt
				EndInfo.Reason = fmt.Sprintf("读取输入错误: %v", err)
				EndInfo.RecoverMessage = MsgContext.RecoverMessage
				return &EndInfo
			}
			if userPrompt == "/exit" {
				fmt.Println(colorBlue + "对话已结束" + colorReset)
				EndInfo.Code = ExitCodeExit
				EndInfo.Reason = "用户主动结束对话"
				cancel() //取消上下文，停止接收输入和消息
				return &EndInfo

			} else if userPrompt == "/new" {
				EndInfo.Code = ExitCodeNew
				EndInfo.Reason = "用户主动开始新对话"
				cancel() //取消上下文，停止接收输入和消息
				return &EndInfo
			} else if userPrompt == "" {
				continue
			}
			MsgBuffer = MsgContext.RecoverMessage
			MsgBuffer = append(MsgBuffer, model.Message{
				Role:    model.RoleUser,
				Content: userPrompt,
			})
		}

		// AgentRunOnce返回的消息包含本次对话输入输出的所有消息
		h, e := AgentRunOnce(ctx, AgentRunner.Runner, AgentRunner.Stream, sessionID, userID, requestID, MsgBuffer)
		if e != nil {
			fmt.Printf(colorRed+"对话过程中发生错误: %v\n"+colorReset, e)
			EndInfo.Code = ExitCodeError
			EndInfo.Reason = fmt.Sprintf("对话过程中发生错误: %v", e)
			if len(h) != 0 {
				EndInfo.RecoverMessage = h //将本轮对话的历史消息保存到EndInfo中，以便下一轮对话恢复
			}
			cancel()
			return &EndInfo
		} else {
			//如果AgentRunOnce成功，重置MsgBuffer，并将MsgContext设为正常结束，否则会一直走ExitCodeError的逻辑无限循环
			MsgBuffer = h
			MsgContext.RecoverMessage = h //将本轮对话的历史消息保存到MsgContext中，以便下一轮对话继承
			MsgContext.Code = ExitCodeNormal
		}

		//每轮对话结束后检查上下文是否被取消，被取消说明用户手动Ctrl+C了，此时应追加历史消息便于继续对话
		select {
		case <-ctx.Done():
			fmt.Printf(colorRed + "\n会话已取消，停止接收输入...\n" + colorReset)
			EndInfo.Code = ExitCodeInt
			EndInfo.Reason = "会话已取消，停止接收输入"
			if len(h) != 0 {
				EndInfo.RecoverMessage = h
			}
			cancel()
			return &EndInfo
		default:

		}

	}

}
