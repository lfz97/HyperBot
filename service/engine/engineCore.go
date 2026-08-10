package engine

import (
	"HyperBot/service/engine/requirements"
	"HyperBot/utils/pretty"
	"context"
	"github.com/google/uuid"
)

func GetEngineService(name string, tui requirements.TuiService) *Engine {
	e := &Engine{
		tui: tui,
	}
	(*e).Agentname = name
	(*e).preCheckLoad()
	(*e).newRunner()
	return e
}

func (e *Engine) AgentStart() {
	MsgContext := turnResult{
		Code:       New,
		Reason:     "新对话",
		OutputPart: "",
	}
	e.randomStartID()
	for {
		EndTurn_p := e.agentRunIteratively(context.Background(), MsgContext)
		if (*EndTurn_p).Code == Exit { //用户主动结束对话，退出程序
			//关闭AgentRunner，释放资源
			(*(*e).AgentRunner_p).Runner.Close()
			(*e).tui.ShowMsgAndExitNoTrigger(pretty.TExit("对话已结束，感谢使用！后会有期！"))

		} else if (*EndTurn_p).Code == New { //用户开始新对话，重置global.SessionID,  global.RequestID，更新MsgContext为新对话的初始状态
			e.randomStartID()
			MsgContext = turnResult{
				Code:       New,
				Reason:     "新对话",
				OutputPart: "",
			}

		} else { //其他情况，继续使用当前的global.SessionID, global.UserID, global.RequestID，更新MsgContext为当前对话的结束状态，供下一轮对话使用
			MsgContext = *EndTurn_p
			continue
		}

	}
}

func (e *Engine) randomStartID() {
	(*(*e).AgentRunner_p).SessionId = uuid.New().String()
	(*(*e).AgentRunner_p).RequestId = uuid.New().String()

}
