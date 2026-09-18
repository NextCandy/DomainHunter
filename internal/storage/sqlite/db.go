// Package sqlite 提供 DomainHunter 的 SQLite 存储实现。
//
// 数据文件默认是 data/domainhunter.db。
package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "github.com/glebarez/sqlite"
)

// DefaultDir 默认数据目录
const DefaultDir = "data"

// DefaultFile 是全新安装时创建的文件名。
const DefaultFile = "domainhunter.db"

// ResolveFile 决定使用哪个数据库文件。
//
// DOMAINHUNTER_DB_FILE 非空时优先使用指定路径，否则使用 data/domainhunter.db。
func ResolveFile(dir string) string {
	if override := strings.TrimSpace(os.Getenv("DOMAINHUNTER_DB_FILE")); override != "" {
		return override
	}
	return DefaultFile
}

// DB 封装底层连接与路径信息
type DB struct {
	*sql.DB
	dir  string
	path string

	migrateOnce sync.Once
	migrateErr  error
}

// Open 打开（必要时创建）数据库。dir 为空时使用 DefaultDir。
func Open(dir string) (*DB, error) {
	if strings.TrimSpace(dir) == "" {
		dir = DefaultDir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}

	path := filepath.Join(dir, ResolveFile(dir))
	// busy_timeout 让并发写入等待而不是立刻报 database is locked。
	dsn := path + "?_pragma=busy_timeout(15000)&_pragma=foreign_keys(0)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"

	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	// WAL 模式下读不阻塞写、写不阻塞读，因此放开连接数：单连接会让前台请求
	// 排在调度器的每一次写入后面（实测 /health 从 5ms 的裸查询被拖到 2.5s）。
	// SQLite 仍是单写者，写冲突由 busy_timeout 重试兜底。
	conn.SetMaxOpenConns(4)
	conn.SetMaxIdleConns(4)

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}
	return &DB{DB: conn, dir: dir, path: path}, nil
}

// Path 返回数据库文件路径
func (d *DB) Path() string { return d.path }

// Dir 返回数据目录
func (d *DB) Dir() string { return d.dir }

// BackupDir 返回备份目录
func (d *DB) BackupDir() string { return filepath.Join(d.dir, "backups") }

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func splitList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func joinList(items []string) string {
	cleaned := make([]string, 0, len(items))
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" {
			cleaned = append(cleaned, item)
		}
	}
	return strings.Join(cleaned, ",")
}
