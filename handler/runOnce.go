package handler

import (
	"context"
	"fmt"
	"sort"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

// AgentRunOnce 处理单轮对话，返回历史消息
func AgentRunOnce(Ctx context.Context, r runner.Runner, stream bool, sessionID string, userID string, requestID string, msg []model.Message) ([]model.Message, error) {

	eventChan, err := runner.RunWithMessages(Ctx, r, userID, sessionID, msg, agent.WithRequestID(requestID))
	if err != nil {
		return []model.Message{}, fmt.Errorf("调用runner时发生错误: %v", err)

	}

	//初始化历史消息slice，把用户输入作为第一条消息
	history := []model.Message{}
	history = append(history, msg...)

	MsgTmpMap := map[int]*model.Message{} //定义一个临时map用来存储消息，key为Index，value为消息指针
	Index := 0                            //指向消息在map中的位置
	var Role model.Role                   //记录消息对应的角色
	startReasoning := false
	for event := range eventChan {

		if event.Error != nil {
			err = fmt.Errorf("获取Event时发生错误: %v", event.Error)
			break
		}
		select {
		case <-Ctx.Done():
			fmt.Printf(colorRed + "会话已取消，停止接收消息...\n" + colorReset)
			//将MsgTmpMap中的消息按照顺序追加到history中
			sortMessagesToHistory(&history, &MsgTmpMap)
			return history, nil

		default:
		}
		if len((*(*event).Response).Choices) > 0 {

			Choice := (*(*event).Response).Choices[0]
			/*------------------打印响应---------------------------------------------------------------------------*/
			printMessage(Choice, &startReasoning, stream)
			/*------------------此处汇聚消息---------------------------------------------------------------------------*/
			gatherMessage(Choice, &MsgTmpMap, &Index, &Role, stream)
		}
		// event.IsRunnerCompletion()判断是否完成输出
		if event.IsRunnerCompletion() {
			break
		}

	}

	//将MsgTmpMap中的消息按照顺序追加到history中
	sortMessagesToHistory(&history, &MsgTmpMap)
	if err != nil {
		return history, err
	} else {
		return history, nil
	}

}

func sortMessagesToHistory(history *[]model.Message, MsgTmpMap *map[int]*model.Message) {
	sortedKeys := []int{}
	for k, _ := range *MsgTmpMap {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Ints(sortedKeys)
	for _, key := range sortedKeys {
		*history = append(*history, *(*MsgTmpMap)[key])
	}
}
