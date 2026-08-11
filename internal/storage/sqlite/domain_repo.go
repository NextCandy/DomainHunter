package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/repository"
)

// DomainRepo domains 表的 SQLite 实现
type DomainRepo struct{ db *DB }

// NewDomainRepo 创建域名仓储
func NewDomainRepo(db *DB) *DomainRepo { return &DomainRepo{db: db} }

const domainColumns = `id, name, enabled, notify, created_at,
	COALESCE(priority,0), COALESCE(retry_count,0), next_check_at,
	COALESCE(favorite,0), COALESCE(note,''), COALESCE(tags,''), folder_id`

func scanDomain(scanner interface{ Scan(...any) error }) (domain.Domain, error) {
	var (
		d                         domain.Domain
		enabled, notify, favorite int
		next                      sql.NullTime
		note, tags                string
		folderID                  sql.NullInt64
	)
	if err := scanner.Scan(&d.ID, &d.Name, &enabled, &notify, &d.CreatedAt,
		&d.Priority, &d.RetryCount, &next, &favorite, &note, &tags, &folderID); err != nil {
		return d, err
	}
	d.Enabled = enabled == 1
	d.Notify = notify == 1
	d.Favorite = favorite == 1
	d.Note = note
	d.Tags = splitList(tags)
	if folderID.Valid {
		value := folderID.Int64
		d.FolderID = &value
	}
	if next.Valid {
		t := next.Time
		d.NextCheckAt = &t
	}
	return d, nil
}

// List 返回域名列表
func (r *DomainRepo) List(ctx context.Context, enabledOnly bool) ([]domain.Domain, error) {
	q := `SELECT ` + domainColumns + ` FROM domains`
	if enabledOnly {
		q += ` WHERE enabled = 1`
	}
	q += ` ORDER BY id ASC`

	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("查询域名失败: %w", err)
	}
	defer rows.Close()

	var out []domain.Domain
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Get 读取单个域名
func (r *DomainRepo) Get(ctx context.Context, name string) (*domain.Domain, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+domainColumns+` FROM domains WHERE lower(name) = lower(?)`, domain.Normalize(name))
	d, err := scanDomain(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Create 新增（或覆盖启用状态）域名
func (r *DomainRepo) Create(ctx context.Context, name string, enabled, notify bool) error {
	name = domain.Normalize(name)
	if name == "" {
		return fmt.Errorf("域名不能为空")
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO domains(name, enabled, notify, next_check_at) VALUES(?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET enabled=excluded.enabled, notify=excluded.notify`,
		name, boolToInt(enabled), boolToInt(notify), time.Now())
	if err != nil {
		return fmt.Errorf("写入域名失败: %w", err)
	}
	return nil
}

// Delete 删除域名及其全部关联数据
func (r *DomainRepo) Delete(ctx context.Context, name string) error {
	_, err := r.DeleteMany(ctx, []string{name})
	return err
}

// DeleteMany 批量删除域名及其全部关联数据（单事务）
func (r *DomainRepo) DeleteMany(ctx context.Context, names []string) (int64, error) {
	cleaned := make([]any, 0, len(names))
	for _, name := range names {
		if n := domain.Normalize(name); n != "" {
			cleaned = append(cleaned, n)
		}
	}
	if len(cleaned) == 0 {
		return 0, fmt.Errorf("没有有效的域名需要删除")
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(cleaned)), ",")

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `DELETE FROM domains WHERE name IN (`+placeholders+`)`, cleaned...)
	if err != nil {
		return 0, fmt.Errorf("删除域名失败: %w", err)
	}
	deleted, _ := res.RowsAffected()

	for _, stmt := range []string{
		`DELETE FROM domain_results WHERE domain IN (` + placeholders + `)`,
		`DELETE FROM notification_history WHERE domain IN (` + placeholders + `)`,
		`DELETE FROM domain_observations WHERE domain IN (` + placeholders + `)`,
		`DELETE FROM query_attempts WHERE domain IN (` + placeholders + `)`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, cleaned...); err != nil {
			return 0, fmt.Errorf("删除关联数据失败: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交事务失败: %w", err)
	}
	return deleted, nil
}

// Update 局部更新域名属性
func (r *DomainRepo) Update(ctx context.Context, name string, patch repository.DomainPatch) error {
	sets := make([]string, 0, 6)
	args := make([]any, 0, 7)

	if patch.Enabled != nil {
		sets = append(sets, "enabled = ?")
		args = append(args, boolToInt(*patch.Enabled))
	}
	if patch.Notify != nil {
		sets = append(sets, "notify = ?")
		args = append(args, boolToInt(*patch.Notify))
	}
	if patch.Favorite != nil {
		sets = append(sets, "favorite = ?")
		args = append(args, boolToInt(*patch.Favorite))
	}
	if patch.Note != nil {
		sets = append(sets, "note = ?")
		args = append(args, *patch.Note)
	}
	if patch.Tags != nil {
		sets = append(sets, "tags = ?")
		args = append(args, joinList(*patch.Tags))
	}
	if patch.Priority != nil {
		sets = append(sets, "priority = ?")
		args = append(args, *patch.Priority)
	}
	if patch.FolderID != nil {
		sets = append(sets, "folder_id = ?")
		args = append(args, *patch.FolderID)
	} else if patch.ClearFolder {
		sets = append(sets, "folder_id = NULL")
	}
	if len(sets) == 0 {
		return nil
	}

	args = append(args, domain.Normalize(name))
	_, err := r.db.ExecContext(ctx,
		`UPDATE domains SET `+strings.Join(sets, ", ")+` WHERE lower(name) = lower(?)`, args...)
	if err != nil {
		return fmt.Errorf("更新域名失败: %w", err)
	}
	return nil
}

// IsEnabled 判断域名是否仍在当前监控列表中。
// 查询结果写回前必须核对，避免与并发删除竞态时留下孤立记录。
func (r *DomainRepo) IsEnabled(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM domains WHERE enabled = 1 AND lower(name) = lower(?))`,
		domain.Normalize(name)).Scan(&exists)
	return exists, err
}

// DueForCheck 返回到期待查询的域名
func (r *DomainRepo) DueForCheck(ctx context.Context, now time.Time, limit int) ([]domain.Domain, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+domainColumns+` FROM domains
		 WHERE enabled = 1 AND (next_check_at IS NULL OR next_check_at <= ?)
		 ORDER BY COALESCE(priority,0) DESC, next_check_at ASC
		 LIMIT ?`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("查询到期域名失败: %w", err)
	}
	defer rows.Close()

	var out []domain.Domain
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ScheduleNext 写回下次检查时间与重试计数
func (r *DomainRepo) ScheduleNext(ctx context.Context, name string, next time.Time, retryCount int) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE domains SET next_check_at = ?, retry_count = ? WHERE lower(name) = lower(?)`,
		next, retryCount, domain.Normalize(name))
	return err
}

// BackfillSchedule 为 next_check_at 为空的域名补一个初始时间。
//
// 只在升级后的第一次启动生效。已有的 last_checked + 间隔如果还在未来就沿用；
// 否则把这些"已经到期"的域名**均匀打散**到接下来的一个间隔窗口里 ——
// 否则几百个域名会在同一秒全部到期，把注册局和本地备用服务打爆。
func (r *DomainRepo) BackfillSchedule(ctx context.Context, defaultInterval time.Duration) (int64, error) {
	if defaultInterval <= 0 {
		defaultInterval = 5 * time.Minute
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT d.name, r.last_checked FROM domains d
		 LEFT JOIN domain_results r ON lower(r.domain) = lower(d.name)
		 WHERE d.next_check_at IS NULL
		 ORDER BY d.id ASC`)
	if err != nil {
		return 0, fmt.Errorf("查询待补齐调度时间的域名失败: %w", err)
	}

	type pending struct {
		name        string
		lastChecked sql.NullTime
	}
	var todo []pending
	for rows.Next() {
		var item pending
		if err := rows.Scan(&item.name, &item.lastChecked); err != nil {
			rows.Close()
			return 0, err
		}
		todo = append(todo, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(todo) == 0 {
		return 0, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `UPDATE domains SET next_check_at = ? WHERE name = ?`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	now := time.Now()
	var updated int64
	for i, item := range todo {
		next := now
		if item.lastChecked.Valid {
			next = item.lastChecked.Time.Add(defaultInterval)
		}
		if !next.After(now) {
			// 已经到期：按顺序均摊到 [now, now+interval) 内的一个时刻
			offset := time.Duration(float64(defaultInterval) * float64(i) / float64(len(todo)))
			next = now.Add(offset)
		}
		if _, err := stmt.ExecContext(ctx, next, item.name); err != nil {
			return 0, fmt.Errorf("补齐调度时间失败(%s): %w", item.name, err)
		}
		updated++
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return updated, nil
}

// LastNotifiedStatus 读取最近一次已通知的状态
func (r *DomainRepo) LastNotifiedStatus(ctx context.Context, name string) (string, error) {
	var status sql.NullString
	err := r.db.QueryRowContext(ctx,
		`SELECT last_notified_status FROM domains WHERE lower(name) = lower(?)`,
		domain.Normalize(name)).Scan(&status)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return status.String, nil
}

// SetLastNotifiedStatus 记录最近一次已通知的状态
func (r *DomainRepo) SetLastNotifiedStatus(ctx context.Context, name, status string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE domains SET last_notified_status = ? WHERE lower(name) = lower(?)`,
		status, domain.Normalize(name))
	return err
}

// Count 统计域名数量
func (r *DomainRepo) Count(ctx context.Context, enabledOnly bool) (int, error) {
	q := `SELECT COUNT(*) FROM domains`
	if enabledOnly {
		q += ` WHERE enabled = 1`
	}
	var n int
	err := r.db.QueryRowContext(ctx, q).Scan(&n)
	return n, err
}

// CleanOrphaned 清理不在 domains 表中的结果与通知历史
func (r *DomainRepo) CleanOrphaned(ctx context.Context) (int64, int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`DELETE FROM domain_results WHERE lower(domain) NOT IN (SELECT lower(name) FROM domains)`)
	if err != nil {
		return 0, 0, fmt.Errorf("清理孤立查询结果失败: %w", err)
	}
	results, _ := res.RowsAffected()

	res, err = tx.ExecContext(ctx,
		`DELETE FROM notification_history WHERE lower(domain) NOT IN (SELECT lower(name) FROM domains)`)
	if err != nil {
		return 0, 0, fmt.Errorf("清理孤立通知历史失败: %w", err)
	}
	notifications, _ := res.RowsAffected()

	for _, stmt := range []string{
		`DELETE FROM domain_observations WHERE lower(domain) NOT IN (SELECT lower(name) FROM domains)`,
		`DELETE FROM query_attempts WHERE lower(domain) NOT IN (SELECT lower(name) FROM domains)`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return 0, 0, fmt.Errorf("清理孤立历史数据失败: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("提交事务失败: %w", err)
	}
	return results, notifications, nil
}
