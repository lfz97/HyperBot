package bootstrap

import (
	"HyperBot/handler"
	"context"
	"github.com/google/uuid"
	"os"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

type RunningStatus string

const END RunningStatus = "end"
const RUN RunningStatus = "run"

func Boot(sigChan chan os.Signal) {

	//用于保留中断或退出的对话历史，以便恢复对话
	MsgContext := handler.EndInfo{
		Code:           handler.ExitCodeNormal,
		Reason:         "对话正常结束",
		RecoverMessage: []model.Message{},
	}

	for {

		sessionID, userID, requestID, AgentName := RandomStartID()
		AgentRunner := Init(AgentName)

		Endinfo_p := handler.AgentRunIteratively(sigChan, context.Background(), AgentRunner, sessionID, userID, requestID, MsgContext)
		exitcode := (*Endinfo_p).Code
		if exitcode == handler.ExitCodeExit {
			//用户主动结束对话，退出程序
			return
		} else if exitcode == handler.ExitCodeNew {
			//用户开始新的一轮对话
			MsgContext.Code = handler.ExitCodeNew
			MsgContext.Reason = (*Endinfo_p).Reason
			MsgContext.RecoverMessage = []model.Message{} //清空恢复消息，防止干扰下一轮对话
			continue
		} else if exitcode == handler.ExitCodeError {
			//对话过程中发生错误，尝试通过history恢复对话
			MsgContext.Code = handler.ExitCodeError
			MsgContext.Reason = (*Endinfo_p).Reason
			MsgContext.RecoverMessage = (*Endinfo_p).RecoverMessage
			continue
		} else if exitcode == handler.ExitCodeInt {
			//追加历史消息，保留对话内容
			MsgContext.Code = handler.ExitCodeInt
			MsgContext.Reason = (*Endinfo_p).Reason
			MsgContext.RecoverMessage = (*Endinfo_p).RecoverMessage
			continue
		} else if exitcode == handler.ExitCodeNormal {
			MsgContext = handler.EndInfo{
				Code:           handler.ExitCodeNormal,
				Reason:         "对话正常结束",
				RecoverMessage: []model.Message{},
			} //正常一轮对话结束,重置MsgContext
		}

	}

}

func RandomStartID() (string, string, string, string) {
	sessionID := uuid.New().String()
	userID := uuid.New().String()
	requestID := uuid.New().String()
	AgentName := uuid.New().String()
	return sessionID, userID, requestID, AgentName
}
