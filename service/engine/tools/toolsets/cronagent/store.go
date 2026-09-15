package cronagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

const cronAgentFileName = "cronagent.json"

// persistVersion 是文件格式版本，结构变更时递增，load 时校验
const persistVersion = 1

type persistedSchedule struct {
	Id        string    `json:"id"`
	Cronexpr  string    `json:"cronExpr"`
	Prompt    string    `json:"prompt"`
	Paused    bool      `json:"paused"`
	CreatedAt time.Time `json:"createdAt"`
}

type persistedFile struct {
	Version   int                 `json:"version"`
	Schedules []persistedSchedule `json:"schedules"`
}

// readPersisted 读取持久化文件。文件不存在不算错误，返回空列表（首次运行）。
// 解析失败或版本不符时返回错误，由调用方决定是否把文件挪走——本函数只做读取。
func readPersisted(path string) ([]persistedSchedule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	var f persistedFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
	}
	if f.Version != persistVersion {
		return nil, fmt.Errorf("%s 格式版本为 %d，当前只支持 %d", path, f.Version, persistVersion)
	}
	return f.Schedules, nil
}

// writePersisted 原子替换：先写 .tmp 再 rename。崩溃只会留下无害的 .tmp，
// 读方永远看到完整的旧文件或完整的新文件，不会看到撕裂的中间态。
func writePersisted(path string, sches []persistedSchedule) error {
	f := persistedFile{Version: persistVersion, Schedules: sches}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 cron agent 配置失败: %w", err)
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("替换 %s 失败: %w", path, err)
	}
	return nil
}

// quarantineFile 把无法使用的存档挪到 <path>.fix<时间戳>，腾出原路径。
// 时间戳必带：第二次失败如果覆盖掉上一份 .fix，丢的可能是用户最初的完整任务列表。
// 挪动失败返回空串，由调用方决定措辞。
func quarantineFile(path string) string {
	dst := fmt.Sprintf("%s.fix%s", path, time.Now().Format("20060102-150405"))
	if err := os.Rename(path, dst); err != nil {
		return ""
	}
	return dst
}
