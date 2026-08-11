package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"DomainHunter/internal/repository"
)

// NotificationConfigRepo 保存规则、模板与摘要配置。
type NotificationConfigRepo struct{ db *DB }

// NewNotificationConfigRepo 创建通知配置仓储。
func NewNotificationConfigRepo(db *DB) *NotificationConfigRepo {
	return &NotificationConfigRepo{db: db}
}

func scanRule(scanner interface{ Scan(...any) error }) (repository.NotificationRule, error) {
	var rule repository.NotificationRule
	var enabled, perDomain, digest int
	var statuses string
	if err := scanner.Scan(&rule.ID, &rule.Name, &enabled, &statuses, &rule.SilenceStart,
		&rule.SilenceEnd, &perDomain, &digest); err != nil {
		return rule, err
	}
	rule.Enabled = enabled == 1
	rule.Statuses = splitList(statuses)
	rule.PerDomain = perDomain == 1
	rule.DigestEnabled = digest == 1
	return rule, nil
}

const ruleColumns = `id, name, COALESCE(enabled,1), COALESCE(statuses,''),
	COALESCE(silence_start,''), COALESCE(silence_end,''), COALESCE(per_domain,1),
	COALESCE(digest_enabled,0)`

func (r *NotificationConfigRepo) ListRules(ctx context.Context) ([]repository.NotificationRule, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+ruleColumns+` FROM notification_rules ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []repository.NotificationRule
	for rows.Next() {
		item, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *NotificationConfigRepo) CreateRule(ctx context.Context, rule repository.NotificationRule) (*repository.NotificationRule, error) {
	if strings.TrimSpace(rule.Name) == "" {
		return nil, fmt.Errorf("通知规则名称不能为空")
	}
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO notification_rules(name, enabled, statuses, silence_start, silence_end, per_domain, digest_enabled)
		 VALUES(?, ?, ?, ?, ?, ?, ?)`, rule.Name, boolToInt(rule.Enabled), joinList(rule.Statuses),
		rule.SilenceStart, rule.SilenceEnd, boolToInt(rule.PerDomain), boolToInt(rule.DigestEnabled))
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return r.getRule(ctx, id)
}

func (r *NotificationConfigRepo) getRule(ctx context.Context, id int64) (*repository.NotificationRule, error) {
	rule, err := scanRule(r.db.QueryRowContext(ctx, `SELECT `+ruleColumns+` FROM notification_rules WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

func (r *NotificationConfigRepo) UpdateRule(ctx context.Context, rule repository.NotificationRule) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE notification_rules SET name=?, enabled=?, statuses=?, silence_start=?, silence_end=?, per_domain=?, digest_enabled=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		rule.Name, boolToInt(rule.Enabled), joinList(rule.Statuses), rule.SilenceStart, rule.SilenceEnd,
		boolToInt(rule.PerDomain), boolToInt(rule.DigestEnabled), rule.ID)
	return err
}

func (r *NotificationConfigRepo) DeleteRule(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM notification_rules WHERE id = ?`, id)
	return err
}

func scanTemplate(scanner interface{ Scan(...any) error }) (repository.NotificationTemplate, error) {
	var template repository.NotificationTemplate
	var enabled int
	if err := scanner.Scan(&template.ID, &template.Name, &template.EventType, &template.Subject, &template.Body, &enabled); err != nil {
		return template, err
	}
	template.Enabled = enabled == 1
	return template, nil
}

const templateColumns = `id, name, COALESCE(event_type,'status_change'), COALESCE(subject,''), COALESCE(body,''), COALESCE(enabled,1)`

func (r *NotificationConfigRepo) ListTemplates(ctx context.Context) ([]repository.NotificationTemplate, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+templateColumns+` FROM notification_templates ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []repository.NotificationTemplate
	for rows.Next() {
		item, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *NotificationConfigRepo) CreateTemplate(ctx context.Context, template repository.NotificationTemplate) (*repository.NotificationTemplate, error) {
	if strings.TrimSpace(template.Name) == "" {
		return nil, fmt.Errorf("通知模板名称不能为空")
	}
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO notification_templates(name, event_type, subject, body, enabled) VALUES(?, ?, ?, ?, ?)`,
		template.Name, template.EventType, template.Subject, template.Body, boolToInt(template.Enabled))
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return r.getTemplate(ctx, id)
}

func (r *NotificationConfigRepo) getTemplate(ctx context.Context, id int64) (*repository.NotificationTemplate, error) {
	template, err := scanTemplate(r.db.QueryRowContext(ctx, `SELECT `+templateColumns+` FROM notification_templates WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	return &template, nil
}

func (r *NotificationConfigRepo) UpdateTemplate(ctx context.Context, template repository.NotificationTemplate) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE notification_templates SET name=?, event_type=?, subject=?, body=?, enabled=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		template.Name, template.EventType, template.Subject, template.Body, boolToInt(template.Enabled), template.ID)
	return err
}

func (r *NotificationConfigRepo) DeleteTemplate(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM notification_templates WHERE id = ?`, id)
	return err
}

func (r *NotificationConfigRepo) GetDigest(ctx context.Context) (*repository.NotificationDigest, error) {
	var digest repository.NotificationDigest
	var enabled int
	var lastSent sql.NullTime
	err := r.db.QueryRowContext(ctx,
		`SELECT enabled, hour, minute, last_sent_at FROM notification_digest WHERE id = 1`).Scan(
		&enabled, &digest.Hour, &digest.Minute, &lastSent)
	if err != nil {
		return nil, err
	}
	digest.Enabled = enabled == 1
	if lastSent.Valid {
		digest.LastSentAt = lastSent.Time
	}
	return &digest, nil
}

func (r *NotificationConfigRepo) UpdateDigest(ctx context.Context, digest repository.NotificationDigest) error {
	if digest.Hour < 0 || digest.Hour > 23 || digest.Minute < 0 || digest.Minute > 59 {
		return fmt.Errorf("摘要时间无效")
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE notification_digest SET enabled=?, hour=?, minute=? WHERE id=1`,
		boolToInt(digest.Enabled), digest.Hour, digest.Minute)
	return err
}

// MarkDigestSent 供后续摘要调度器记录发送时间。
func (r *NotificationConfigRepo) MarkDigestSent(ctx context.Context, when time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE notification_digest SET last_sent_at=? WHERE id=1`, when)
	return err
}
