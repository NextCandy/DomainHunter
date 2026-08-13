package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"DomainHunter/internal/domain"
)

// ExpiryReminderRepo 保存到期提醒的幂等标记。
type ExpiryReminderRepo struct{ db *DB }

func NewExpiryReminderRepo(db *DB) *ExpiryReminderRepo {
	return &ExpiryReminderRepo{db: db}
}

func (r *ExpiryReminderRepo) Sent(ctx context.Context, name string, expiryAt time.Time, milestone int) (bool, error) {
	var value int
	err := r.db.QueryRowContext(ctx,
		`SELECT 1 FROM expiry_reminders WHERE domain=? AND expiry_at=? AND milestone=? LIMIT 1`,
		domain.Normalize(name), expiryAt.UTC(), milestone).Scan(&value)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("查询到期提醒去重标记失败: %w", err)
	}
	return value == 1, nil
}

func (r *ExpiryReminderRepo) MarkSent(ctx context.Context, name string, expiryAt time.Time, milestone int, sentAt time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO expiry_reminders(domain, expiry_at, milestone, sent_at)
		 VALUES(?, ?, ?, ?)
		 ON CONFLICT(domain, expiry_at, milestone) DO UPDATE SET sent_at=excluded.sent_at`,
		domain.Normalize(name), expiryAt.UTC(), milestone, sentAt.UTC())
	if err != nil {
		return fmt.Errorf("保存到期提醒去重标记失败: %w", err)
	}
	return nil
}
