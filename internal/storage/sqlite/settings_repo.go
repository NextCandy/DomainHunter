package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/repository"
)

// SettingsRepo app_settings 表的 SQLite 实现
type SettingsRepo struct{ db *DB }

// NewSettingsRepo 创建设置仓储
func NewSettingsRepo(db *DB) *SettingsRepo { return &SettingsRepo{db: db} }

// All 读取全部设置
func (r *SettingsRepo) All(ctx context.Context) (map[string]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT key, COALESCE(value,'') FROM app_settings`)
	if err != nil {
		return nil, fmt.Errorf("查询设置失败: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// Get 读取单个设置
func (r *SettingsRepo) Get(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(value,'') FROM app_settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("读取设置失败(%s): %w", key, err)
	}
	return value, true, nil
}

// Upsert 批量写入设置
func (r *SettingsRepo) Upsert(ctx context.Context, values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO app_settings(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for k, v := range values {
		if _, err := stmt.ExecContext(ctx, k, v); err != nil {
			return fmt.Errorf("写入设置失败(%s): %w", k, err)
		}
	}
	return tx.Commit()
}

// NotificationRepo notification_history 表的 SQLite 实现
type NotificationRepo struct{ db *DB }

// NewNotificationRepo 创建通知历史仓储
func NewNotificationRepo(db *DB) *NotificationRepo { return &NotificationRepo{db: db} }

// Last 读取域名最近一次通知记录
func (r *NotificationRepo) Last(ctx context.Context, name string) (*repository.NotificationRecord, error) {
	var rec repository.NotificationRecord
	err := r.db.QueryRowContext(ctx,
		`SELECT id, domain, status, COALESCE(old_status,''), sent_at, COALESCE(notification_type,'status_change'), read_at
		 FROM notification_history WHERE domain = ? ORDER BY sent_at DESC LIMIT 1`,
		domain.Normalize(name)).Scan(&rec.ID, &rec.Domain, &rec.Status, &rec.OldStatus, &rec.SentAt, &rec.Type, &rec.ReadAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询通知记录失败: %w", err)
	}
	return &rec, nil
}

// Save 写入通知记录（同域名同状态覆盖时间）
func (r *NotificationRepo) Save(ctx context.Context, name, status, oldStatus string) error {
	return r.SaveEvent(ctx, name, status, oldStatus, "status_change")
}

// SaveEvent 写入一条带真实分类的通知记录。再次触发同一域名/事件时，
// read_at 会清空，避免新消息继续显示为已读。
func (r *NotificationRepo) SaveEvent(ctx context.Context, name, status, oldStatus, eventType string) error {
	name = domain.Normalize(name)
	if name == "" {
		return fmt.Errorf("域名不能为空")
	}
	if strings.TrimSpace(eventType) == "" {
		eventType = "status_change"
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO notification_history(domain, status, old_status, notification_type)
		 VALUES(?, ?, ?, ?)
		 ON CONFLICT(domain, status) DO UPDATE SET sent_at = CURRENT_TIMESTAMP,
		 old_status = excluded.old_status, notification_type = excluded.notification_type, read_at = NULL`,
		name, status, oldStatus, eventType)
	if err != nil {
		return fmt.Errorf("保存通知记录失败: %w", err)
	}
	return nil
}

// ListRecent 返回最近的通知记录
func (r *NotificationRepo) ListRecent(ctx context.Context, limit int) ([]repository.NotificationRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, domain, status, COALESCE(old_status,''), sent_at, COALESCE(notification_type,'status_change'), read_at
		 FROM notification_history ORDER BY sent_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []repository.NotificationRecord
	for rows.Next() {
		var rec repository.NotificationRecord
		if err := rows.Scan(&rec.ID, &rec.Domain, &rec.Status, &rec.OldStatus, &rec.SentAt, &rec.Type, &rec.ReadAt); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (r *NotificationRepo) MarkRead(ctx context.Context, ids []int64, read bool) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i], args[i] = "?", id
	}
	value := "CURRENT_TIMESTAMP"
	if !read {
		value = "NULL"
	}
	result, err := r.db.ExecContext(ctx, `UPDATE notification_history SET read_at=`+value+` WHERE id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
