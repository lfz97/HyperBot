package bootstrap

import (
	"context"
	"github.com/google/uuid"
	"os"
	"sync"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpcagent/handler"
)

type RunningStatus string

const END RunningStatus = "end"
const RUN RunningStatus = "run"

func Boot(sigChan chan os.Signal) {

	wgbootloop_p := &sync.WaitGroup{}
	wgbootloop_p.Add(1)
	go func(wgbootloop_p *sync.WaitGroup) {
		defer wgbootloop_p.Done()

		status := RUN
		//用于保留中断或退出的对话历史，以便恢复对话
		MsgContext := handler.EndInfo{
			Code:           handler.ExitCodeNormal,
			Reason:         "对话正常结束",
			RecoverMessage: []model.Message{},
		}

		for {
			if status == END {
				break
			}
			wgsession_p := &sync.WaitGroup{}
			wgsession_p.Add(1)
			go func(wgsession_p *sync.WaitGroup) {

				defer wgsession_p.Done()

				sessionID := uuid.New().String()
				userID := uuid.New().String()
				requestID := uuid.New().String()
				AgentName := uuid.New().String()
				runner := Init(AgentName)
				Endinfo_p := handler.AgentRunIteratively(sigChan, context.Background(), runner, sessionID, userID, requestID, MsgContext)
				if (*Endinfo_p).Code == handler.ExitCodeExit {
					//用户主动结束对话，退出程序
					status = END
					return
				} else if (*Endinfo_p).Code == handler.ExitCodeNew {
					//正常完成对话，重新开始新的一轮对话
					MsgContext.Code = handler.ExitCodeNew
					MsgContext.Reason = (*Endinfo_p).Reason
					MsgContext.RecoverMessage = []model.Message{} //清空恢复消息，防止干扰下一轮对话
					return
				} else if (*Endinfo_p).Code == handler.ExitCodeError {
					//对话过程中发生错误，尝试通过history恢复对话
					MsgContext.Code = handler.ExitCodeError
					MsgContext.Reason = (*Endinfo_p).Reason
					MsgContext.RecoverMessage = (*Endinfo_p).RecoverMessage
					return
				} else if (*Endinfo_p).Code == handler.ExitCodeInt {
					//追加历史消息，保留对话内容
					MsgContext.Code = handler.ExitCodeInt
					MsgContext.Reason = (*Endinfo_p).Reason
					MsgContext.RecoverMessage = (*Endinfo_p).RecoverMessage
					return
				}
				MsgContext.RecoverMessage = []model.Message{} //正常一轮对话结束，清空恢复消息

			}(wgsession_p)
			wgsession_p.Wait()
		}

	}(wgbootloop_p)
	wgbootloop_p.Wait()

}
