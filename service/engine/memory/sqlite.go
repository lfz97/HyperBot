package memory

import (
	"HyperBot/service/engine/config"
	"HyperBot/service/engine/models"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"trpc.group/trpc-go/trpc-agent-go/memory"
	"trpc.group/trpc-go/trpc-agent-go/memory/extractor"
	memorysqlite "trpc.group/trpc-go/trpc-agent-go/memory/sqlite"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// NewSQLiteMemoryService 创建带自动记忆提取的 SQLite 记忆服务
// 后台 extractor 在每个 turn 结束后自动从对话中提取记忆
// agent 端暴露 memory_search/load/add/update 工具作为手动补充
func NewSQLiteMemoryService(m config.Model, dbPath string) (*memorysqlite.Service, error) {
	var extractorModel model.Model
	if m.APIType == "openai" {
		extractorModel = models.Openai(m)
	} else if m.APIType == "anthropic" {
		extractorModel = models.Anthropic(m)
	}

	// WAL：读写并发。每轮 BeforeModel 都会全表扫描召回记忆，rollback 模式下这个长读会挡住写、顶穿 busy_timeout。
	// _txlock=immediate 不可删：框架 rotateMemory 是"先 SELECT 再 UPDATE"的延迟事务，中途升级撞锁时
	// SQLite 直接返回 BUSY 且不走 busy handler，busy_timeout 救不了；只有让 BeginTx 发 BEGIN IMMEDIATE、
	// 把写锁提到事务开头，这个错误才变成可等待的。（WAL 已隐含 synchronous=NORMAL，无需重复指定）
	// 以上参数由 mattn/go-sqlite3 在每条新连接建立时自动 PRAGMA，框架不参与。
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=15000&_txlock=immediate"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	ext := extractor.NewExtractor(extractorModel)
	service, err := memorysqlite.NewService(
		db,
		memorysqlite.WithSoftDelete(true),
		memorysqlite.WithMemoryLimit(100000),
		memorysqlite.WithExtractor(ext),
		memorysqlite.WithToolEnabled(memory.LoadToolName, true), // auto 模式下 load 默认 disabled，必须显式 enable 才能被 exposed 生效
		memorysqlite.WithAutoMemoryExposedTools([]string{memory.AddToolName, memory.UpdateToolName, memory.LoadToolName, memory.DeleteToolName, memory.SearchToolName}...), // 为 agent 暴露除了clear以外的所有记忆操作工具
		memorysqlite.WithMemoryJobTimeout(600*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("create sqlite memory service: %w", err)
	}
	return service, nil
}
