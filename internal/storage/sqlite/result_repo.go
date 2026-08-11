package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"DomainHunter/internal/domain"
)

// ResultRepo domain_results 表的 SQLite 实现（当前状态快照）
type ResultRepo struct{ db *DB }

// NewResultRepo 创建结果仓储
func NewResultRepo(db *DB) *ResultRepo { return &ResultRepo{db: db} }

const resultColumns = `domain, COALESCE(status,''), COALESCE(registrar,''), last_checked,
	COALESCE(query_method,''), created_at, expiry_at, updated_at,
	COALESCE(name_servers,''), COALESCE(whois_raw,''), COALESCE(error_message,''),
	COALESCE(epp_statuses,'')`

func scanResult(scanner interface{ Scan(...any) error }) (domain.Info, error) {
	var (
		info        domain.Info
		status      string
		nameServers string
		created     sql.NullTime
		expiry      sql.NullTime
		updated     sql.NullTime
		lastChecked sql.NullTime
		eppStatuses string
	)
	if err := scanner.Scan(&info.Name, &status, &info.Registrar, &lastChecked, &info.QueryMethod,
		&created, &expiry, &updated, &nameServers, &info.WhoisRaw, &info.ErrorMessage, &eppStatuses); err != nil {
		return info, err
	}
	info.Status = domain.Status(status)
	if lastChecked.Valid {
		info.LastChecked = lastChecked.Time
	}
	if created.Valid {
		t := created.Time
		info.CreatedDate = &t
	}
	if expiry.Valid {
		t := expiry.Time
		info.ExpiryDate = &t
	}
	if updated.Valid {
		t := updated.Time
		info.UpdatedDate = &t
	}
	info.NameServers = splitList(nameServers)
	info.EPPStatuses = splitList(eppStatuses)
	return info, nil
}

// Get 读取单个域名的当前状态
func (r *ResultRepo) Get(ctx context.Context, name string) (*domain.Info, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+resultColumns+` FROM domain_results WHERE domain = ?`, domain.Normalize(name))
	info, err := scanResult(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询域名结果失败: %w", err)
	}
	return &info, nil
}

// LoadAll 读取全部域名的当前状态
func (r *ResultRepo) LoadAll(ctx context.Context) (map[string]domain.Info, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+resultColumns+` FROM domain_results ORDER BY domain ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]domain.Info)
	for rows.Next() {
		info, err := scanResult(rows)
		if err != nil {
			return nil, err
		}
		out[strings.ToLower(info.Name)] = info
	}
	return out, rows.Err()
}

// Save 写入/更新单个域名的当前状态
func (r *ResultRepo) Save(ctx context.Context, info domain.Info) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO domain_results(domain, status, registrar, last_checked, query_method,
			created_at, expiry_at, updated_at, name_servers, whois_raw, error_message, epp_statuses)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(domain) DO UPDATE SET
			status=excluded.status, registrar=excluded.registrar, last_checked=excluded.last_checked,
			query_method=excluded.query_method, created_at=excluded.created_at, expiry_at=excluded.expiry_at,
			updated_at=excluded.updated_at, name_servers=excluded.name_servers,
			whois_raw=excluded.whois_raw, error_message=excluded.error_message,
			epp_statuses=excluded.epp_statuses`,
		domain.Normalize(info.Name), string(info.Status), info.Registrar, info.LastChecked, info.QueryMethod,
		info.CreatedDate, info.ExpiryDate, info.UpdatedDate, joinList(info.NameServers),
		info.WhoisRaw, info.ErrorMessage, joinList(info.EPPStatuses))
	return err
}

// UpdateRaw 只更新原始报文
func (r *ResultRepo) UpdateRaw(ctx context.Context, name, raw string) error {
	name = domain.Normalize(name)
	if name == "" {
		return fmt.Errorf("域名不能为空")
	}
	_, err := r.db.ExecContext(ctx, `UPDATE domain_results SET whois_raw = ? WHERE domain = ?`, raw, name)
	if err != nil {
		return fmt.Errorf("更新WHOIS原始数据失败: %w", err)
	}
	return nil
}
