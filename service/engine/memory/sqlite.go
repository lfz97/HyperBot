package memory

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
	"trpc.group/trpc-go/trpc-agent-go/memory"
	memorysqlite "trpc.group/trpc-go/trpc-agent-go/memory/sqlite"
)

// NewSQLiteMemoryService 创建 SQLite 记忆服务（纯手动模式）：不配自动提取器，
// 记忆的写入与修订完全由 agent 显式操作；读侧为 BeforeModel 的 [MEMORY] 预注入 + 主动检索。
func NewSQLiteMemoryService(dbPath string) (*memorysqlite.Service, error) {
	// WAL 保读写并发（每轮召回是长读，rollback 模式会挡写）；
	// _txlock=immediate 必须保留：框架延迟事务中途升级撞锁时 BUSY 不走 busy handler，
	// busy_timeout 救不了，只有把写锁提到事务开头才能变成可等待。参数由驱动每连接自动 PRAGMA。
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=15000&_txlock=immediate"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	// agentic（无 extractor）模式下 enabledTools 就是暴露闸门：Delete 默认关闭需显式打开，Clear 默认不暴露
	service, err := memorysqlite.NewService(
		db,
		memorysqlite.WithSoftDelete(true),
		memorysqlite.WithMemoryLimit(100000),
		memorysqlite.WithToolEnabled(memory.DeleteToolName, true),
	)
	if err != nil {
		return nil, fmt.Errorf("create sqlite memory service: %w", err)
	}
	return service, nil
}
