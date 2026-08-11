package sqlite

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Backup 使用 SQLite 的 VACUUM INTO 生成一致性备份。
//
// 运行中的库不能直接 cp：WAL 模式下 db / db-wal / db-shm 三个文件的状态可能
// 不一致，拷出来的库有概率损坏。VACUUM INTO 由 SQLite 自己保证快照一致性。
func (d *DB) Backup(label string) (string, error) {
	dir := d.BackupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建备份目录失败: %w", err)
	}

	name := fmt.Sprintf("puff-%s", time.Now().Format("20060102-150405"))
	if label = sanitizeLabel(label); label != "" {
		name += "-" + label
	}
	target := filepath.Join(dir, name+".db")

	// VACUUM INTO 要求目标文件不存在
	if _, err := os.Stat(target); err == nil {
		target = filepath.Join(dir, fmt.Sprintf("%s-%d.db", name, time.Now().UnixNano()%1000))
	}

	if _, err := d.Exec(fmt.Sprintf("VACUUM INTO %s", quoteLiteral(target))); err != nil {
		return "", fmt.Errorf("VACUUM INTO 失败: %w", err)
	}
	return target, nil
}

// PruneBackups 只保留最近 keep 份自动备份，避免树莓派/NAS 上无限增长。
// 只清理本程序生成的 puff-*.db，不会碰用户自己放进去的文件。
func (d *DB) PruneBackups(keep int) error {
	if keep <= 0 {
		keep = 5
	}
	entries, err := os.ReadDir(d.BackupDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	type backupFile struct {
		path string
		mod  time.Time
	}
	var files []backupFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "puff-") || !strings.HasSuffix(name, ".db") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, backupFile{filepath.Join(d.BackupDir(), name), info.ModTime()})
	}
	if len(files) <= keep {
		return nil
	}

	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	var firstErr error
	for _, f := range files[keep:] {
		if err := os.Remove(f.path); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ListBackups 列出自动备份文件（新的在前）
func (d *DB) ListBackups() ([]string, error) {
	entries, err := os.ReadDir(d.BackupDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "puff-") && strings.HasSuffix(entry.Name(), ".db") {
			out = append(out, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out, nil
}

func sanitizeLabel(label string) string {
	label = strings.TrimSpace(strings.ToLower(label))
	var b strings.Builder
	for _, r := range label {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r == ' ' || r == '_':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// quoteLiteral 把路径包成 SQL 字符串字面量（VACUUM INTO 不支持参数绑定）
func quoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
