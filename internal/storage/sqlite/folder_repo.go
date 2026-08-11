package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"DomainHunter/internal/domain"
)

// FolderRepo folders 表的 SQLite 实现。
type FolderRepo struct{ db *DB }

// NewFolderRepo 创建文件夹仓储。
func NewFolderRepo(db *DB) *FolderRepo { return &FolderRepo{db: db} }

func scanFolder(scanner interface{ Scan(...any) error }) (domain.Folder, error) {
	var folder domain.Folder
	var parent sql.NullInt64
	if err := scanner.Scan(&folder.ID, &folder.Name, &parent, &folder.CreatedAt); err != nil {
		return folder, err
	}
	if parent.Valid {
		value := parent.Int64
		folder.ParentID = &value
	}
	return folder, nil
}

// List 返回全部文件夹，按树稳定排序前先按 ID 排序。
func (r *FolderRepo) List(ctx context.Context) ([]domain.Folder, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, name, parent_id, created_at FROM folders ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("查询文件夹失败: %w", err)
	}
	defer rows.Close()
	var out []domain.Folder
	for rows.Next() {
		folder, err := scanFolder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, folder)
	}
	return out, rows.Err()
}

// Get 读取单个文件夹。
func (r *FolderRepo) Get(ctx context.Context, id int64) (*domain.Folder, error) {
	folder, err := scanFolder(r.db.QueryRowContext(ctx,
		`SELECT id, name, parent_id, created_at FROM folders WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &folder, nil
}

// Create 新建文件夹。
func (r *FolderRepo) Create(ctx context.Context, name string, parentID *int64) (*domain.Folder, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("文件夹名称不能为空")
	}
	res, err := r.db.ExecContext(ctx, `INSERT INTO folders(name, parent_id) VALUES(?, ?)`, name, parentID)
	if err != nil {
		return nil, fmt.Errorf("创建文件夹失败: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

// Update 修改文件夹名称/父节点。
func (r *FolderRepo) Update(ctx context.Context, id int64, name string, parentID *int64) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("文件夹名称不能为空")
	}
	_, err := r.db.ExecContext(ctx, `UPDATE folders SET name = ?, parent_id = ? WHERE id = ?`, name, parentID, id)
	return err
}

// Delete 删除文件夹并把其中域名移回根目录；不删除域名。
func (r *FolderRepo) Delete(ctx context.Context, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE domains SET folder_id = NULL WHERE folder_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE folders SET parent_id = NULL WHERE parent_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM folders WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// MoveDomains 把一批域名移动到文件夹；folderID 为 nil 时移到根目录。
func (r *FolderRepo) MoveDomains(ctx context.Context, names []string, folderID *int64) (int64, error) {
	cleaned := make([]string, 0, len(names))
	seen := map[string]struct{}{}
	for _, name := range names {
		name = domain.Normalize(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		cleaned = append(cleaned, name)
	}
	if len(cleaned) == 0 {
		return 0, fmt.Errorf("没有有效的域名需要移动")
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(cleaned)), ",")
	args := make([]any, 0, len(cleaned)+1)
	args = append(args, folderID)
	for _, name := range cleaned {
		args = append(args, name)
	}
	res, err := r.db.ExecContext(ctx, `UPDATE domains SET folder_id = ? WHERE lower(name) IN (`+placeholders+`)`, args...)
	if err != nil {
		return 0, err
	}
	count, _ := res.RowsAffected()
	return count, nil
}
