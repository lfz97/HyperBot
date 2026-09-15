package cronagent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

type schedule struct {
	Prompt   string
	runner   runner.Runner
	Cronexpr string
	Id       string
	Cronid   cron.EntryID
	userid   string
	c        *cron.Cron

	// 以下字段由 mu 保护：cron 触发的 goroutine 写，Status/Output/Stop 读
	mu            sync.Mutex
	resultBuf     *bytes.Buffer
	err           error
	createdAt     *time.Time
	enableAt      *time.Time
	disableAt     *time.Time
	lastTriggerAt *time.Time
	cancel        context.CancelFunc
	eventChan     <-chan *event.Event

	paused atomic.Bool
	// executing 表示"此刻有一轮正在跑"，与 paused（调度是否启用）是两回事：
	// paused=false 且 executing=false 就是正常待命，等下一次触发
	executing atomic.Bool
}

// resetLocked 清空上一轮的运行状态，调用方必须持有 s.mu
func (s *schedule) resetLocked() {
	s.err = nil
	s.eventChan = nil
	s.resultBuf = bytes.NewBuffer(nil)
}

func (s *schedule) setErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func (s *schedule) Add() error {
	cronid, err := s.c.AddFunc(s.Cronexpr, func() {
		if s.paused.Load() {
			return
		}
		// 上一轮还没结束就跳过本次触发：否则 resetLocked 会换掉正在被写入的
		// buffer，两轮输出混在一起
		if !s.executing.CompareAndSwap(false, true) {
			return
		}
		defer s.executing.Store(false)

		// 在调 runner 之前记录，这样它反映的是 cron 真正触发的时刻，
		// 而不是模型回复的时刻——两者差几秒是正常的 LLM 延迟
		triggerAt := time.Now()
		s.mu.Lock()
		s.resetLocked()
		s.lastTriggerAt = &triggerAt
		s.mu.Unlock()

		ctx, cancel := context.WithCancel(context.Background())
		s.mu.Lock()
		s.cancel = cancel
		s.mu.Unlock()

		eventChan, runErr := s.runner.Run(
			ctx,
			s.userid,
			uuid.New().String(),
			model.NewUserMessage(s.Prompt),
			agent.WithToolCallArgumentsJSONRepairEnabled(true),
		)
		if runErr != nil {
			cancel()
			s.setErr(runErr)
			return
		}
		s.mu.Lock()
		s.eventChan = eventChan
		s.mu.Unlock()

		// 同步消费完这一轮再返回，executing 才能覆盖整个执行周期
		for ev := range eventChan {
			if ev.Response == nil {
				continue
			}
			if ev.Error != nil {
				if ev.IsTerminalError() {
					s.setErr(errors.New(ev.Error.Message))
					break
				}
				continue
			}
			if len(ev.Choices) > 0 {
				choice := ev.Choices[0]
				s.mu.Lock()
				if choice.Message.Content != "" {
					s.resultBuf.WriteString(choice.Message.Content)
				}
				for _, toolCall := range choice.Message.ToolCalls {
					fmt.Fprintf(s.resultBuf, "calling tool: %s\n", toolCall.Function.Name)
				}
				s.mu.Unlock()
			}
			if ev.IsFinalResponse() {
				break
			}
		}
		cancel()
	})
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.Cronid = cronid
	s.mu.Unlock()
	s.c.Start()
	return nil
}

// Start 恢复调度。幂等：已在运行时只刷新 enableAt。
func (s *schedule) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.enableAt = &now
	s.paused.Store(false)
}

// Stop 暂停调度并取消正在执行的那一轮。幂等。
func (s *schedule) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.disableAt = &now
	s.paused.Store(true)
	// cron 还没触发过时 cancel 为 nil
	if s.cancel != nil {
		s.cancel()
	}
}

// Remove 摘除 cron 条目、停掉调度器并取消在跑的那一轮。不要求先 Stop。
func (s *schedule) Remove() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.c.Remove(s.Cronid)
	s.c.Stop()
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *schedule) Status() ScheduleStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ScheduleStatus{
		Prompt:        s.Prompt,
		Cronexpr:      s.Cronexpr,
		Id:            s.Id,
		Err:           errString(s.err),
		CreatedAt:     formatTime(s.createdAt),
		EnableAt:      formatTime(s.enableAt),
		DisableAt:     formatTime(s.disableAt),
		LastTriggerAt: formatTime(s.lastTriggerAt),
		Cronid:        int(s.Cronid),
		Paused:        s.paused.Load(),
		Executing:     s.executing.Load(),
	}
}

// Output 返回末尾 window 个字符；window <= 0 或超过总长度时返回全量。
func (s *schedule) Output(window int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := []rune(s.resultBuf.String())
	if window > 0 && window < len(all) {
		return string(all[len(all)-window:])
	}
	return string(all)
}

func formatTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02 15:04:05")
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
