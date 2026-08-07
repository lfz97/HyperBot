package agent

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/mackerelio/go-osstat/memory"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// 注入模型调用前callback，在消息末尾追加当前状态栏（时间、工作目录、内存）。
// 注意：追加在末尾而非前置 —— 自动前缀缓存要求请求头部保持稳定，
// 状态栏每次调用内容变化，放头部会破坏整个前缀缓存（实测：尾部99%命中 vs 头部0）。
// 使用本功能必须关闭框架的 system 前置重排（openai.WithOptimizeForCache），否则
// 尾部状态栏会被框架挪回头部、缓存收益失效；关闭位置：service/engine/models/openai.go。
// 状态栏不进 session（仅存在于当次请求副本），不污染摘要/上下文压缩。
func setBeforeModelStatusCallback() llmagent.Option {

	modelCallbacks := model.NewCallbacks().RegisterBeforeModel(
		func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
			//获取时间
			datenow := time.Now().Format("2006-01-02 15:04:05")
			//获取工作目录
			cwd, err := os.Getwd()
			if err != nil {
				cwd = "UNKNOWN"
			}
			//获取总内存、当前内存
			var memTotalStr string
			var memNowStr string
			memoryInfo, err := memory.Get()
			if err == nil {
				memTotalStr = strconv.FormatUint(memoryInfo.Total/1024/1024, 10)
				memNowStr = strconv.FormatUint(memoryInfo.Used/1024/1024, 10)
			} else {
				memTotalStr = "UNKNOWN"
				memNowStr = "UNKNOWN"
			}
			status := fmt.Sprintf(`[STATUS] TIMENOW: %s , CWD: %s , MEMORY USAGE: %s/%s MB`, datenow, cwd, memNowStr, memTotalStr)

			args.Request.Messages = append(args.Request.Messages, model.NewSystemMessage(status)) //在末尾追加状态栏
			return nil, nil
		},
	)
	return llmagent.WithModelCallbacks(modelCallbacks)
}
