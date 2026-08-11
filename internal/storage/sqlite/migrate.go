package sqlite

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"DomainHunter/internal/logger"
)

// Migration 一次 schema 变更。
//
// 约束（对应"Migration 安全"要求）：
//   - 可重复检测：已应用的版本记录在 schema_migrations，不会重复执行
//   - 不破坏已有数据：只做 CREATE TABLE IF NOT EXISTS / ADD COLUMN / CREATE INDEX
//   - 使用事务：任一条语句失败则整条迁移回滚，并中止后续升级
type Migration struct {
	Version string
	Name    string
	Stmts   []string
}

// migrations 按版本号顺序执行，只允许追加，不允许修改已发布的条目。
var migrations = []Migration{
	{
		Version: "001",
		Name:    "baseline",
		Stmts: []string{
			`CREATE TABLE IF NOT EXISTS app_settings (
				key TEXT PRIMARY KEY,
				value TEXT
			)`,
			`CREATE TABLE IF NOT EXISTS domains (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT UNIQUE NOT NULL,
				enabled INTEGER NOT NULL DEFAULT 1,
				notify INTEGER NOT NULL DEFAULT 1,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP
			)`,
			`CREATE TABLE IF NOT EXISTS domain_results (
				domain TEXT PRIMARY KEY,
				status TEXT,
				registrar TEXT,
				last_checked DATETIME,
				query_method TEXT,
				created_at DATETIME,
				expiry_at DATETIME,
				updated_at DATETIME,
				name_servers TEXT,
				whois_raw TEXT,
				error_message TEXT,
				created_at_record DATETIME DEFAULT CURRENT_TIMESTAMP
			)`,
			`CREATE TABLE IF NOT EXISTS notification_history (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				domain TEXT NOT NULL,
				status TEXT NOT NULL,
				old_status TEXT,
				sent_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				notification_type TEXT DEFAULT 'status_change',
				UNIQUE(domain, status)
			)`,
		},
	},
	{
		Version: "002",
		Name:    "domain_scheduling_columns",
		Stmts: []string{
			`ALTER TABLE domains ADD COLUMN priority INTEGER NOT NULL DEFAULT 0`,
			`ALTER TABLE domains ADD COLUMN retry_count INTEGER NOT NULL DEFAULT 0`,
			`ALTER TABLE domains ADD COLUMN next_check_at DATETIME`,
			`ALTER TABLE domains ADD COLUMN favorite INTEGER NOT NULL DEFAULT 0`,
			`ALTER TABLE domains ADD COLUMN note TEXT`,
			`ALTER TABLE domains ADD COLUMN tags TEXT`,
			`ALTER TABLE domains ADD COLUMN last_notified_status TEXT`,
			`CREATE INDEX IF NOT EXISTS idx_domains_schedule
				ON domains(enabled, next_check_at, priority)`,
		},
	},
	{
		Version: "003",
		Name:    "observation_history",
		Stmts: []string{
			`CREATE TABLE IF NOT EXISTS domain_observations (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				domain_id INTEGER NOT NULL DEFAULT 0,
				domain TEXT NOT NULL,
				status TEXT NOT NULL,
				registrar TEXT,
				registered_at DATETIME,
				updated_at DATETIME,
				expiry_at DATETIME,
				name_servers TEXT,
				provider TEXT,
				confidence TEXT,
				changed INTEGER NOT NULL DEFAULT 0,
				observed_at DATETIME NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_observations_domain
				ON domain_observations(domain, observed_at DESC)`,
		},
	},
	{
		Version: "004",
		Name:    "query_attempts",
		Stmts: []string{
			`CREATE TABLE IF NOT EXISTS query_attempts (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				domain_id INTEGER NOT NULL DEFAULT 0,
				domain TEXT NOT NULL,
				observation_id INTEGER NOT NULL DEFAULT 0,
				provider TEXT NOT NULL,
				status TEXT,
				success INTEGER NOT NULL DEFAULT 0,
				latency_ms INTEGER NOT NULL DEFAULT 0,
				error_message TEXT,
				raw_response TEXT,
				queried_at DATETIME NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_attempts_domain
				ON query_attempts(domain, queried_at DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_attempts_observation
				ON query_attempts(observation_id)`,
		},
	},
	{
		Version: "005",
		Name:    "auth_hardening",
		Stmts: []string{
			// server_password_hash 与 session_secret 走 app_settings，
			// 这里只保证键存在，具体值由 auth 层在首次登录/启动时写入。
			`INSERT INTO app_settings(key, value)
				SELECT 'server_password_hash', ''
				WHERE NOT EXISTS (SELECT 1 FROM app_settings WHERE key = 'server_password_hash')`,
			`INSERT INTO app_settings(key, value)
				SELECT 'session_secret', ''
				WHERE NOT EXISTS (SELECT 1 FROM app_settings WHERE key = 'session_secret')`,
		},
	},
}

// AppliedMigration 已应用的迁移记录
type AppliedMigration struct {
	Version   string    `json:"version"`
	Name      string    `json:"name"`
	AppliedAt time.Time `json:"applied_at"`
}

// Migrate 执行所有未应用的迁移。
//
// 存在待执行迁移时，会先用 VACUUM INTO 生成一份一致的数据库备份，
// 备份失败即中止升级 —— 宁可不升级，也不能在没有回滚点的情况下改结构。
func (d *DB) Migrate() error {
	d.migrateOnce.Do(func() { d.migrateErr = d.migrate() })
	return d.migrateErr
}

func (d *DB) migrate() error {
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		name TEXT,
		applied_at DATETIME NOT NULL
	)`); err != nil {
		return fmt.Errorf("创建 schema_migrations 失败: %w", err)
	}

	applied, err := d.appliedVersions()
	if err != nil {
		return err
	}

	// 老库里已经有 001 的四张表但没有迁移记录：把 001 标记为已应用，
	// 避免对现网数据重复执行建表语句。
	if len(applied) == 0 {
		legacy, err := d.tableExists("domain_results")
		if err != nil {
			return err
		}
		if legacy {
			if err := d.markApplied(migrations[0]); err != nil {
				return err
			}
			applied[migrations[0].Version] = true
			logger.Info("检测到既有 Puff 数据库，已将迁移 001_baseline 标记为已应用")
		}
	}

	pending := make([]Migration, 0, len(migrations))
	for _, m := range migrations {
		if !applied[m.Version] {
			pending = append(pending, m)
		}
	}
	if len(pending) == 0 {
		return nil
	}

	logger.Info("检测到 %d 条待执行的数据库迁移，升级前先生成备份", len(pending))
	backup, err := d.Backup("pre-migration")
	if err != nil {
		return fmt.Errorf("迁移前备份数据库失败，已中止升级: %w", err)
	}
	logger.Info("数据库备份已生成: %s", backup)

	for _, m := range pending {
		if err := d.applyMigration(m); err != nil {
			return fmt.Errorf("迁移 %s_%s 失败，已停止升级（备份: %s）: %w", m.Version, m.Name, backup, err)
		}
		logger.Info("迁移 %s_%s 已应用", m.Version, m.Name)
	}

	if err := d.PruneBackups(5); err != nil {
		logger.Warn("清理旧备份失败: %v", err)
	}
	return nil
}

func (d *DB) applyMigration(m Migration) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, stmt := range m.Stmts {
		if _, err := tx.Exec(stmt); err != nil {
			// ALTER TABLE ADD COLUMN 对已存在的列会报错。老库可能已经被
			// 早期版本的零散 ALTER 加过同名列，这种情况视为已满足。
			if isDuplicateColumn(err) {
				continue
			}
			return fmt.Errorf("执行语句失败: %s: %w", firstLine(stmt), err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations(version, name, applied_at) VALUES(?, ?, ?)`,
		m.Version, m.Name, time.Now().UTC()); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) markApplied(m Migration) error {
	_, err := d.Exec(`INSERT OR IGNORE INTO schema_migrations(version, name, applied_at) VALUES(?, ?, ?)`,
		m.Version, m.Name, time.Now().UTC())
	return err
}

func (d *DB) appliedVersions() (map[string]bool, error) {
	rows, err := d.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("读取迁移记录失败: %w", err)
	}
	defer rows.Close()

	applied := map[string]bool{}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		applied[version] = true
	}
	return applied, rows.Err()
}

// AppliedMigrations 返回已应用的迁移列表（供 /health 与文档使用）
func (d *DB) AppliedMigrations() ([]AppliedMigration, error) {
	rows, err := d.Query(`SELECT version, COALESCE(name,''), applied_at FROM schema_migrations ORDER BY version ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AppliedMigration
	for rows.Next() {
		var m AppliedMigration
		if err := rows.Scan(&m.Version, &m.Name, &m.AppliedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (d *DB) tableExists(name string) (bool, error) {
	var found string
	err := d.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&found)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func isDuplicateColumn(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate column name")
}

func firstLine(stmt string) string {
	stmt = strings.TrimSpace(stmt)
	if idx := strings.IndexAny(stmt, "\r\n"); idx > 0 {
		return stmt[:idx]
	}
	if len(stmt) > 80 {
		return stmt[:80]
	}
	return stmt
}
