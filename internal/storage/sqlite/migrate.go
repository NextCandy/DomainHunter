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
	{
		Version: "006",
		Name:    "epp_statuses",
		Stmts: []string{
			`ALTER TABLE domain_results ADD COLUMN epp_statuses TEXT`,
		},
	},
	{
		Version: "007",
		Name:    "folders",
		Stmts: []string{
			`CREATE TABLE IF NOT EXISTS folders (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL UNIQUE,
				parent_id INTEGER,
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			`ALTER TABLE domains ADD COLUMN folder_id INTEGER`,
			`CREATE INDEX IF NOT EXISTS idx_domains_folder ON domains(folder_id)`,
		},
	},
	{
		Version: "008",
		Name:    "api_tokens",
		Stmts: []string{
			`CREATE TABLE IF NOT EXISTS api_tokens (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL,
				token_hash TEXT NOT NULL UNIQUE,
				scopes TEXT NOT NULL DEFAULT 'read,write',
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				last_used_at DATETIME,
				revoked_at DATETIME
			)`,
			`CREATE INDEX IF NOT EXISTS idx_api_tokens_active ON api_tokens(token_hash, revoked_at)`,
		},
	},
	{
		Version: "009",
		Name:    "notification_rules_templates_digest",
		Stmts: []string{
			`CREATE TABLE IF NOT EXISTS notification_rules (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL,
				enabled INTEGER NOT NULL DEFAULT 1,
				statuses TEXT NOT NULL DEFAULT '',
				silence_start TEXT NOT NULL DEFAULT '',
				silence_end TEXT NOT NULL DEFAULT '',
				per_domain INTEGER NOT NULL DEFAULT 1,
				digest_enabled INTEGER NOT NULL DEFAULT 0,
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			`CREATE TABLE IF NOT EXISTS notification_templates (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL,
				event_type TEXT NOT NULL DEFAULT 'status_change',
				subject TEXT NOT NULL DEFAULT '',
				body TEXT NOT NULL DEFAULT '',
				enabled INTEGER NOT NULL DEFAULT 1,
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			`CREATE TABLE IF NOT EXISTS notification_digest (
				id INTEGER PRIMARY KEY CHECK(id = 1),
				enabled INTEGER NOT NULL DEFAULT 0,
				hour INTEGER NOT NULL DEFAULT 8,
				minute INTEGER NOT NULL DEFAULT 0,
				last_sent_at DATETIME
			)`,
			`INSERT INTO notification_digest(id) SELECT 1 WHERE NOT EXISTS (SELECT 1 FROM notification_digest WHERE id = 1)`,
		},
	},
	{
		Version: "010",
		Name:    "p1_saved_views_ai_automation",
		Stmts: []string{
			`CREATE TABLE IF NOT EXISTS saved_views (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL UNIQUE,
				filter_json TEXT NOT NULL,
				shared INTEGER NOT NULL DEFAULT 0,
				created_by TEXT NOT NULL DEFAULT '',
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			`CREATE INDEX IF NOT EXISTS idx_saved_views_updated ON saved_views(updated_at DESC)`,
			`CREATE TABLE IF NOT EXISTS ai_provider_settings (
				id INTEGER PRIMARY KEY CHECK(id = 1),
				provider TEXT NOT NULL DEFAULT 'deepseek',
				base_url TEXT NOT NULL DEFAULT 'https://api.deepseek.com',
				model TEXT NOT NULL DEFAULT 'deepseek-v4-flash',
				encrypted_api_key TEXT NOT NULL DEFAULT '',
				timeout_seconds INTEGER NOT NULL DEFAULT 30,
				concurrency INTEGER NOT NULL DEFAULT 1,
				max_output_tokens INTEGER NOT NULL DEFAULT 1200,
				daily_limit INTEGER NOT NULL DEFAULT 50,
				cache_ttl_seconds INTEGER NOT NULL DEFAULT 86400,
				enabled INTEGER NOT NULL DEFAULT 0,
				updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			`INSERT INTO ai_provider_settings(id) SELECT 1 WHERE NOT EXISTS (SELECT 1 FROM ai_provider_settings WHERE id = 1)`,
			`CREATE TABLE IF NOT EXISTS ai_jobs (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				domain TEXT NOT NULL,
				status TEXT NOT NULL DEFAULT 'queued',
				input_fingerprint TEXT NOT NULL,
				input_json TEXT NOT NULL,
				attempts INTEGER NOT NULL DEFAULT 0,
				max_attempts INTEGER NOT NULL DEFAULT 3,
				available_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				lease_until DATETIME,
				last_error TEXT NOT NULL DEFAULT '',
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				started_at DATETIME,
				completed_at DATETIME
			)`,
			`CREATE INDEX IF NOT EXISTS idx_ai_jobs_claim ON ai_jobs(status, available_at, lease_until)`,
			`CREATE INDEX IF NOT EXISTS idx_ai_jobs_domain ON ai_jobs(domain, created_at DESC)`,
			`CREATE TABLE IF NOT EXISTS ai_domain_valuations (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				domain TEXT NOT NULL UNIQUE,
				job_id INTEGER NOT NULL,
				provider TEXT NOT NULL DEFAULT 'deepseek',
				model TEXT NOT NULL,
				analysis_version TEXT NOT NULL,
				input_fingerprint TEXT NOT NULL,
				result_json TEXT NOT NULL,
				quality_score INTEGER NOT NULL,
				liquidity_score INTEGER NOT NULL,
				risk_level TEXT NOT NULL,
				value_low REAL NOT NULL,
				value_high REAL NOT NULL,
				confidence TEXT NOT NULL,
				generated_at DATETIME NOT NULL,
				expires_at DATETIME NOT NULL,
				FOREIGN KEY(job_id) REFERENCES ai_jobs(id)
			)`,
			`CREATE INDEX IF NOT EXISTS idx_ai_valuations_expiry ON ai_domain_valuations(expires_at)`,
			`CREATE TABLE IF NOT EXISTS automation_rules (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL UNIQUE,
				enabled INTEGER NOT NULL DEFAULT 0,
				dry_run INTEGER NOT NULL DEFAULT 1,
				trigger_json TEXT NOT NULL,
				conditions_json TEXT NOT NULL,
				actions_json TEXT NOT NULL,
				cooldown_seconds INTEGER NOT NULL DEFAULT 86400,
				daily_run_cap INTEGER NOT NULL DEFAULT 100,
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			`CREATE INDEX IF NOT EXISTS idx_automation_rules_enabled ON automation_rules(enabled, updated_at DESC)`,
			`CREATE TABLE IF NOT EXISTS automation_runs (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				rule_id INTEGER NOT NULL,
				event_id TEXT NOT NULL,
				domain TEXT NOT NULL DEFAULT '',
				status TEXT NOT NULL,
				dry_run INTEGER NOT NULL DEFAULT 1,
				action_count INTEGER NOT NULL DEFAULT 0,
				details_json TEXT NOT NULL DEFAULT '{}',
				started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				completed_at DATETIME,
				UNIQUE(rule_id, event_id, domain),
				FOREIGN KEY(rule_id) REFERENCES automation_rules(id)
			)`,
			`CREATE INDEX IF NOT EXISTS idx_automation_runs_rule ON automation_runs(rule_id, started_at DESC)`,
		},
	},
	{
		Version: "011",
		Name:    "p1_automation_events_and_bulk_audits",
		Stmts: []string{
			`CREATE TABLE IF NOT EXISTS automation_cursors (
				key TEXT PRIMARY KEY,
				value TEXT NOT NULL,
				updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			`CREATE TABLE IF NOT EXISTS bulk_action_audits (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				action_type TEXT NOT NULL,
				input_json TEXT NOT NULL,
				matched INTEGER NOT NULL DEFAULT 0,
				task_count INTEGER NOT NULL DEFAULT 0,
				result_json TEXT NOT NULL DEFAULT '{}',
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			`CREATE INDEX IF NOT EXISTS idx_bulk_action_audits_created ON bulk_action_audits(created_at DESC)`,
		},
	},
	{
		Version: "012",
		Name:    "ai_provider_profiles",
		Stmts: []string{
			`CREATE TABLE IF NOT EXISTS ai_provider_profiles (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					name TEXT NOT NULL UNIQUE,
					provider TEXT NOT NULL,
					base_url TEXT NOT NULL,
					model TEXT NOT NULL,
					encrypted_api_key TEXT NOT NULL DEFAULT '',
					timeout_seconds INTEGER NOT NULL DEFAULT 30,
					concurrency INTEGER NOT NULL DEFAULT 1,
					max_output_tokens INTEGER NOT NULL DEFAULT 1200,
					daily_limit INTEGER NOT NULL DEFAULT 50,
					cache_ttl_seconds INTEGER NOT NULL DEFAULT 86400,
					enabled INTEGER NOT NULL DEFAULT 0,
					is_default INTEGER NOT NULL DEFAULT 0,
					created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
				)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_ai_provider_profiles_default ON ai_provider_profiles(is_default) WHERE is_default = 1`,
			`INSERT INTO ai_provider_profiles(name,provider,base_url,model,encrypted_api_key,timeout_seconds,concurrency,max_output_tokens,daily_limit,cache_ttl_seconds,enabled,is_default)
					SELECT
						CASE
							WHEN provider = 'deepseek' AND base_url = 'https://api.deepseek.com' AND model = 'deepseek-v4-flash' AND encrypted_api_key = '' THEN 'OpenAI Compatible'
							WHEN provider = 'deepseek' THEN 'DeepSeek'
							ELSE provider
						END,
						CASE
							WHEN provider = 'deepseek' AND base_url = 'https://api.deepseek.com' AND model = 'deepseek-v4-flash' AND encrypted_api_key = '' THEN 'openai_compatible'
							ELSE provider
						END,
						CASE
							WHEN provider = 'deepseek' AND base_url = 'https://api.deepseek.com' AND model = 'deepseek-v4-flash' AND encrypted_api_key = '' THEN 'https://opencode.ai/zen/v1'
							ELSE base_url
						END,
						CASE
							WHEN provider = 'deepseek' AND base_url = 'https://api.deepseek.com' AND model = 'deepseek-v4-flash' AND encrypted_api_key = '' THEN 'deepseek-v4-flash-free'
							ELSE model
						END,
						encrypted_api_key,timeout_seconds,concurrency,max_output_tokens,daily_limit,cache_ttl_seconds,enabled,1
					FROM ai_provider_settings
					WHERE id = 1 AND NOT EXISTS (SELECT 1 FROM ai_provider_profiles WHERE is_default = 1)`,
			`UPDATE ai_provider_settings
					SET provider = 'openai_compatible', base_url = 'https://opencode.ai/zen/v1', model = 'deepseek-v4-flash-free'
					WHERE id = 1 AND provider = 'deepseek' AND base_url = 'https://api.deepseek.com' AND model = 'deepseek-v4-flash' AND encrypted_api_key = ''`,
		},
	},
	{
		Version: "013",
		Name:    "strict_research_valuation_v2",
		Stmts: []string{
			// P1 已经使用 ai_provider_profiles / ai_jobs / ai_domain_valuations。
			// 严格估价链路使用独立的结果表，避免升级时改变旧表语义。
			`CREATE TABLE IF NOT EXISTS ai_profiles (
				id TEXT PRIMARY KEY,
				name TEXT NOT NULL UNIQUE,
				provider TEXT NOT NULL,
				enabled INTEGER NOT NULL DEFAULT 1,
				is_default INTEGER NOT NULL DEFAULT 0,
				base_url TEXT NOT NULL,
				base_url_host TEXT NOT NULL,
				model TEXT NOT NULL,
				api_key_ciphertext TEXT NOT NULL DEFAULT '',
				api_key_source TEXT NOT NULL DEFAULT 'none',
				thinking_type TEXT NOT NULL DEFAULT 'disabled',
				reasoning_effort TEXT NOT NULL DEFAULT 'low',
				timeout_seconds INTEGER NOT NULL DEFAULT 30,
				max_tokens INTEGER NOT NULL DEFAULT 900,
				concurrency INTEGER NOT NULL DEFAULT 1,
				daily_limit INTEGER NOT NULL DEFAULT 50,
				cache_ttl_hours INTEGER NOT NULL DEFAULT 24,
				last_tested_at DATETIME,
				last_test_latency_ms INTEGER,
				last_error TEXT NOT NULL DEFAULT '',
				created_at DATETIME NOT NULL,
				updated_at DATETIME NOT NULL
			)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_ai_profiles_single_default
				ON ai_profiles(is_default) WHERE is_default = 1`,
			`CREATE TABLE IF NOT EXISTS ai_valuation_jobs (
				id TEXT PRIMARY KEY,
				domain_id INTEGER NOT NULL DEFAULT 0,
				domain TEXT NOT NULL,
				profile_id TEXT NOT NULL,
				state TEXT NOT NULL,
				priority TEXT NOT NULL DEFAULT 'normal',
				input_fingerprint TEXT NOT NULL,
				prompt_version TEXT NOT NULL,
				lease_until DATETIME,
				attempt_count INTEGER NOT NULL DEFAULT 0,
				queued_at DATETIME NOT NULL,
				started_at DATETIME,
				completed_at DATETIME,
				retry_after DATETIME,
				error_code TEXT NOT NULL DEFAULT '',
				safe_error_message TEXT NOT NULL DEFAULT '',
				causation_id TEXT NOT NULL DEFAULT '',
				created_at DATETIME NOT NULL,
				updated_at DATETIME NOT NULL,
				FOREIGN KEY(profile_id) REFERENCES ai_profiles(id)
			)`,
			`CREATE INDEX IF NOT EXISTS idx_ai_valuation_jobs_take
				ON ai_valuation_jobs(state, priority, retry_after, queued_at)`,
			`CREATE INDEX IF NOT EXISTS idx_ai_valuation_jobs_domain
				ON ai_valuation_jobs(domain, queued_at DESC)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_ai_valuation_jobs_active_dedup
				ON ai_valuation_jobs(domain, profile_id, input_fingerprint, prompt_version)
				WHERE state IN ('queued','running','deferred')`,
			`CREATE TABLE IF NOT EXISTS ai_domain_valuations_v2 (
				id TEXT PRIMARY KEY,
				domain_id INTEGER NOT NULL DEFAULT 0,
				domain TEXT NOT NULL,
				job_id TEXT NOT NULL UNIQUE,
				profile_id TEXT NOT NULL,
				provider TEXT NOT NULL,
				model TEXT NOT NULL,
				prompt_version TEXT NOT NULL,
				input_fingerprint TEXT NOT NULL,
				quality_score INTEGER NOT NULL,
				liquidity_score INTEGER NOT NULL,
				risk_level TEXT NOT NULL,
				confidence TEXT NOT NULL,
				indicative_value_low INTEGER,
				indicative_value_high INTEGER,
				currency TEXT NOT NULL DEFAULT 'USD',
				summary TEXT NOT NULL,
				strengths_json TEXT NOT NULL DEFAULT '[]',
				risks_json TEXT NOT NULL DEFAULT '[]',
				data_gaps_json TEXT NOT NULL DEFAULT '[]',
				evidence_used_json TEXT NOT NULL DEFAULT '[]',
				status_guard TEXT NOT NULL,
				disclaimer TEXT NOT NULL,
				created_at DATETIME NOT NULL,
				expires_at DATETIME,
				FOREIGN KEY(job_id) REFERENCES ai_valuation_jobs(id),
				FOREIGN KEY(profile_id) REFERENCES ai_profiles(id)
			)`,
			`CREATE INDEX IF NOT EXISTS idx_ai_domain_valuations_v2_domain
				ON ai_domain_valuations_v2(domain, created_at DESC)`,
			`CREATE TABLE IF NOT EXISTS ai_audit_log (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				event_type TEXT NOT NULL,
				domain TEXT NOT NULL DEFAULT '',
				profile_id TEXT NOT NULL DEFAULT '',
				job_id TEXT NOT NULL DEFAULT '',
				actor TEXT NOT NULL DEFAULT '',
				details_json TEXT NOT NULL DEFAULT '{}',
				created_at DATETIME NOT NULL
			)`,
		},
	},
	{
		Version: "014",
		Name:    "domain_result_confidence",
		Stmts: []string{
			`ALTER TABLE domain_results ADD COLUMN confidence TEXT NOT NULL DEFAULT ''`,
		},
	},
	{
		Version: "015",
		Name:    "strict_valuation_report_fields",
		Stmts: []string{
			`ALTER TABLE ai_domain_valuations_v2 ADD COLUMN price_evaluation_low INTEGER`,
			`ALTER TABLE ai_domain_valuations_v2 ADD COLUMN price_evaluation_high INTEGER`,
			`ALTER TABLE ai_domain_valuations_v2 ADD COLUMN price_evaluation_currency TEXT NOT NULL DEFAULT 'CNY'`,
			`ALTER TABLE ai_domain_valuations_v2 ADD COLUMN core_analysis TEXT NOT NULL DEFAULT ''`,
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
