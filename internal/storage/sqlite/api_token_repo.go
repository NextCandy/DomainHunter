package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"DomainHunter/internal/repository"
)

// APITokenRepo 保存 Bearer token 的不可逆哈希。
type APITokenRepo struct{ db *DB }

// NewAPITokenRepo 创建 API token 仓储。
func NewAPITokenRepo(db *DB) *APITokenRepo { return &APITokenRepo{db: db} }

const tokenColumns = `id, name, COALESCE(scopes,''), created_at, last_used_at, revoked_at`

func scanToken(scanner interface{ Scan(...any) error }) (repository.APIToken, error) {
	var token repository.APIToken
	var scopes string
	var lastUsed, revoked sql.NullTime
	if err := scanner.Scan(&token.ID, &token.Name, &scopes, &token.CreatedAt, &lastUsed, &revoked); err != nil {
		return token, err
	}
	token.Scopes = splitList(scopes)
	if lastUsed.Valid {
		value := lastUsed.Time
		token.LastUsedAt = &value
	}
	if revoked.Valid {
		value := revoked.Time
		token.RevokedAt = &value
	}
	return token, nil
}

// Create 写入 token 哈希。
func (r *APITokenRepo) Create(ctx context.Context, name string, scopes []string, tokenHash string) (*repository.APIToken, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.TrimSpace(tokenHash) == "" {
		return nil, fmt.Errorf("token 名称和哈希不能为空")
	}
	if len(scopes) == 0 {
		scopes = []string{"read", "write"}
	}
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO api_tokens(name, token_hash, scopes) VALUES(?, ?, ?)`,
		name, tokenHash, joinList(scopes))
	if err != nil {
		return nil, fmt.Errorf("创建 API token 失败: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return r.get(ctx, id)
}

func (r *APITokenRepo) get(ctx context.Context, id int64) (*repository.APIToken, error) {
	token, err := scanToken(r.db.QueryRowContext(ctx,
		`SELECT `+tokenColumns+` FROM api_tokens WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	return &token, nil
}

// List 返回未撤销 token 的元数据，不返回哈希。
func (r *APITokenRepo) List(ctx context.Context) ([]repository.APIToken, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+tokenColumns+` FROM api_tokens ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []repository.APIToken
	for rows.Next() {
		token, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, token)
	}
	return out, rows.Err()
}

// Validate 校验哈希并原子更新最后使用时间。
func (r *APITokenRepo) Validate(ctx context.Context, tokenHash string) (*repository.APIToken, error) {
	token, err := scanToken(r.db.QueryRowContext(ctx,
		`SELECT `+tokenColumns+` FROM api_tokens WHERE token_hash = ? AND revoked_at IS NULL`, tokenHash))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if _, err := r.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, now, token.ID); err != nil {
		return nil, err
	}
	token.LastUsedAt = &now
	return &token, nil
}

// Revoke 软撤销 token，保留审计信息。
func (r *APITokenRepo) Revoke(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE api_tokens SET revoked_at = COALESCE(revoked_at, CURRENT_TIMESTAMP) WHERE id = ?`, id)
	return err
}
