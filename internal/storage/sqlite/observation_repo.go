package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/repository"
)

// ObservationRepo domain_observations + query_attempts 的 SQLite 实现
type ObservationRepo struct{ db *DB }

// NewObservationRepo 创建历史仓储
func NewObservationRepo(db *DB) *ObservationRepo { return &ObservationRepo{db: db} }

// Save 在同一事务里写入一条观测与它的全部查询尝试
func (r *ObservationRepo) Save(ctx context.Context, obs domain.Observation, attempts []domain.Attempt) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO domain_observations(domain_id, domain, status, registrar, registered_at,
			updated_at, expiry_at, name_servers, provider, confidence, changed, observed_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		obs.DomainID, domain.Normalize(obs.Domain), string(obs.Status), obs.Registrar, obs.RegisteredAt,
		obs.UpdatedAt, obs.ExpiryAt, joinList(obs.NameServers), obs.Provider, string(obs.Confidence),
		boolToInt(obs.Changed), obs.ObservedAt)
	if err != nil {
		return 0, fmt.Errorf("写入观测记录失败: %w", err)
	}
	observationID, _ := res.LastInsertId()

	for _, attempt := range attempts {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO query_attempts(domain_id, domain, observation_id, provider, status,
				success, latency_ms, error_message, raw_response, queried_at)
			 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			attempt.DomainID, domain.Normalize(attempt.Domain), observationID, attempt.Provider,
			string(attempt.Status), boolToInt(attempt.Success), attempt.LatencyMS,
			attempt.ErrorMessage, attempt.RawResponse, attempt.QueriedAt); err != nil {
			return 0, fmt.Errorf("写入查询尝试失败: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return observationID, nil
}

const observationColumns = `id, domain_id, domain, status, COALESCE(registrar,''), registered_at,
	updated_at, expiry_at, COALESCE(name_servers,''), COALESCE(provider,''), COALESCE(confidence,''),
	COALESCE(changed,0), observed_at`

func scanObservation(scanner interface{ Scan(...any) error }) (domain.Observation, error) {
	var (
		obs                         domain.Observation
		status, confidence, ns      string
		registered, updated, expiry sql.NullTime
		changed                     int
	)
	if err := scanner.Scan(&obs.ID, &obs.DomainID, &obs.Domain, &status, &obs.Registrar,
		&registered, &updated, &expiry, &ns, &obs.Provider, &confidence, &changed, &obs.ObservedAt); err != nil {
		return obs, err
	}
	obs.Status = domain.Status(status)
	obs.Confidence = domain.Confidence(confidence)
	obs.NameServers = splitList(ns)
	obs.Changed = changed == 1
	if registered.Valid {
		t := registered.Time
		obs.RegisteredAt = &t
	}
	if updated.Valid {
		t := updated.Time
		obs.UpdatedAt = &t
	}
	if expiry.Valid {
		t := expiry.Time
		obs.ExpiryAt = &t
	}
	return obs, nil
}

// ListByDomain 按时间倒序返回某个域名的观测历史
func (r *ObservationRepo) ListByDomain(ctx context.Context, name string, limit int) ([]domain.Observation, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+observationColumns+` FROM domain_observations
		 WHERE lower(domain) = lower(?) ORDER BY observed_at DESC, id DESC LIMIT ?`,
		domain.Normalize(name), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Observation
	for rows.Next() {
		obs, err := scanObservation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, obs)
	}
	return out, rows.Err()
}

// ListRecentChanges 返回最近发生状态变化的观测（概览页"最近状态变化"）
func (r *ObservationRepo) ListRecentChanges(ctx context.Context, limit int) ([]domain.Observation, error) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+observationColumns+` FROM domain_observations
		 WHERE changed = 1 ORDER BY observed_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Observation
	for rows.Next() {
		obs, err := scanObservation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, obs)
	}
	return out, rows.Err()
}

const attemptColumns = `id, domain_id, domain, observation_id, provider, COALESCE(status,''),
	COALESCE(success,0), COALESCE(latency_ms,0), COALESCE(error_message,''), COALESCE(raw_response,''), queried_at`

func scanAttempt(scanner interface{ Scan(...any) error }) (domain.Attempt, error) {
	var (
		a       domain.Attempt
		status  string
		success int
	)
	if err := scanner.Scan(&a.ID, &a.DomainID, &a.Domain, &a.ObservationID, &a.Provider, &status,
		&success, &a.LatencyMS, &a.ErrorMessage, &a.RawResponse, &a.QueriedAt); err != nil {
		return a, err
	}
	a.Status = domain.Status(status)
	a.Success = success == 1
	return a, nil
}

// ListAttempts 返回某个域名最近的查询尝试
func (r *ObservationRepo) ListAttempts(ctx context.Context, name string, limit int) ([]domain.Attempt, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+attemptColumns+` FROM query_attempts
		 WHERE lower(domain) = lower(?) ORDER BY queried_at DESC, id DESC LIMIT ?`,
		domain.Normalize(name), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Attempt
	for rows.Next() {
		a, err := scanAttempt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListAttemptsByObservation 返回某次观测对应的全部查询尝试
func (r *ObservationRepo) ListAttemptsByObservation(ctx context.Context, observationID int64) ([]domain.Attempt, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+attemptColumns+` FROM query_attempts WHERE observation_id = ? ORDER BY id ASC`, observationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Attempt
	for rows.Next() {
		a, err := scanAttempt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Stats 返回历史表的行数
func (r *ObservationRepo) Stats(ctx context.Context) (int64, int64, error) {
	var observations, attempts int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM domain_observations`).Scan(&observations); err != nil {
		return 0, 0, err
	}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM query_attempts`).Scan(&attempts); err != nil {
		return 0, 0, err
	}
	return observations, attempts, nil
}

// Prune 按保留策略清理历史。
//
// 树莓派/NAS 上会长期运行，历史表必须有上限：先按天数清理，再按"每域名最多
// N 条"清理，最后删掉没有对应观测的孤儿尝试。
func (r *ObservationRepo) Prune(ctx context.Context, retention repository.Retention) (int64, int64, error) {
	var observations, attempts int64

	if retention.Days > 0 {
		cutoff := time.Now().AddDate(0, 0, -retention.Days)
		res, err := r.db.ExecContext(ctx, `DELETE FROM domain_observations WHERE observed_at < ?`, cutoff)
		if err != nil {
			return 0, 0, fmt.Errorf("按天数清理观测记录失败: %w", err)
		}
		n, _ := res.RowsAffected()
		observations += n

		res, err = r.db.ExecContext(ctx, `DELETE FROM query_attempts WHERE queried_at < ?`, cutoff)
		if err != nil {
			return 0, 0, fmt.Errorf("按天数清理查询尝试失败: %w", err)
		}
		n, _ = res.RowsAffected()
		attempts += n
	}

	if retention.MaxPerDomain > 0 {
		res, err := r.db.ExecContext(ctx,
			`DELETE FROM domain_observations WHERE id IN (
				SELECT id FROM (
					SELECT id, ROW_NUMBER() OVER (PARTITION BY domain ORDER BY observed_at DESC, id DESC) AS rn
					FROM domain_observations
				) WHERE rn > ?
			)`, retention.MaxPerDomain)
		if err != nil {
			return 0, 0, fmt.Errorf("按条数清理观测记录失败: %w", err)
		}
		n, _ := res.RowsAffected()
		observations += n
	}

	// 观测被删掉后，它的查询尝试也没有意义了
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM query_attempts WHERE observation_id > 0
		 AND observation_id NOT IN (SELECT id FROM domain_observations)`)
	if err != nil {
		return 0, 0, fmt.Errorf("清理孤立查询尝试失败: %w", err)
	}
	n, _ := res.RowsAffected()
	attempts += n

	return observations, attempts, nil
}
