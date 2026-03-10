package bootstrap

import (
	"context"
	"github.com/google/uuid"
	"os"
	"os/signal"
	"sync"
	"trpcagent/handler"
)

func Boot() {
	//定义一个切片用来存储所有工具的退出命令
	wgbootloop_p := &sync.WaitGroup{}
	wgbootloop_p.Add(1)
	go func(wgbootloop_p *sync.WaitGroup) {
		defer wgbootloop_p.Done()
		wgsession_p := &sync.WaitGroup{}
		status := "new"
		for {
			if status == "done" {
				break
			}
			wgsession_p.Add(1)
			go func(wgsession_p *sync.WaitGroup) {
				defer wgsession_p.Done()

				// 启动信号监听 goroutine
				signalChan := make(chan os.Signal, 1)
				signal.Notify(signalChan)

				sessionID := uuid.New().String()
				userID := uuid.New().String()
				requestID := uuid.New().String()
				AgentName := uuid.New().String()
				runner := Init(AgentName)
				EndReason_p, _ := handler.AgentRunIteratively(context.Background(), runner, sessionID, userID, requestID)
				if EndReason_p.Code == 0 {
					//用户主动结束对话，退出程序
					status = "done"
					return
				} else if EndReason_p.Code == 1 {
					//正常完成对话，重新开始新的一轮对话
					return
				}

			}(wgsession_p)
			wgsession_p.Wait()
		}

	}(wgbootloop_p)
	wgbootloop_p.Wait()

}
