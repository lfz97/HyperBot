package memory

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
	"trpc.group/trpc-go/trpc-agent-go/memory"
	memorysqlite "trpc.group/trpc-go/trpc-agent-go/memory/sqlite"
)

// NewSQLiteMemoryService 创建 SQLite 记忆服务（纯手动模式）
// 不配自动提取器：记忆的写入与修订完全由 agent 在会话中显式操作，
// 读侧仍是 BeforeModel 的 [MEMORY] 预注入 + memory_search/load 主动检索
func NewSQLiteMemoryService(dbPath string) (*memorysqlite.Service, error) {
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

	// agentic（无 extractor）模式下 enabledTools 就是暴露闸门：默认集合为 add/update/search/load，
	// Delete 需显式打开；Clear 默认就不在集合内（危险操作）。为 agent 暴露除 clear 外的全部记忆工具
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
