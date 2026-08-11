package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/repository"
)

// DomainExportRecord 是导入/导出的稳定记录格式；状态字段只读，导入时不会覆盖查询结果。
type DomainExportRecord struct {
	Domain      string        `json:"domain"`
	Status      domain.Status `json:"status,omitempty"`
	Registrar   string        `json:"registrar,omitempty"`
	FolderID    *int64        `json:"folder_id,omitempty"`
	Folder      string        `json:"folder,omitempty"`
	Tags        []string      `json:"tags,omitempty"`
	Note        string        `json:"note,omitempty"`
	Enabled     bool          `json:"enabled"`
	Notify      bool          `json:"notify"`
	AddedAt     *time.Time    `json:"added_at,omitempty"`
	LastChecked *time.Time    `json:"last_checked,omitempty"`
}

// DomainImportRecord 是 CSV/JSON 导入的统一输入格式。
type DomainImportRecord struct {
	Domain   string   `json:"domain"`
	FolderID *int64   `json:"folder_id,omitempty"`
	Folder   string   `json:"folder,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Note     *string  `json:"note,omitempty"`
	Enabled  *bool    `json:"enabled,omitempty"`
	Notify   *bool    `json:"notify,omitempty"`
}

// DomainImportResult 描述一次导入结果。
type DomainImportResult struct {
	Imported    int      `json:"imported"`
	Overwritten int      `json:"overwritten"`
	Skipped     int      `json:"skipped"`
	Duplicates  int      `json:"duplicates"`
	Invalid     []string `json:"invalid,omitempty"`
}

// FolderTreeNode 是文件夹树的 API 结构。
type FolderTreeNode struct {
	domain.Folder
	Children []*FolderTreeNode `json:"children,omitempty"`
}

// ListFolders 返回扁平文件夹列表和树形结构。
func (s *DomainService) ListFolders(ctx context.Context) ([]domain.Folder, []*FolderTreeNode, error) {
	if s.folders == nil {
		return nil, nil, fmt.Errorf("文件夹仓储未初始化")
	}
	items, err := s.folders.List(ctx)
	if err != nil {
		return nil, nil, err
	}
	nodes := make(map[int64]*FolderTreeNode, len(items))
	for _, item := range items {
		copyItem := item
		nodes[item.ID] = &FolderTreeNode{Folder: copyItem}
	}
	var roots []*FolderTreeNode
	for _, item := range items {
		node := nodes[item.ID]
		if item.ParentID != nil {
			if parent := nodes[*item.ParentID]; parent != nil && parent.ID != item.ID {
				parent.Children = append(parent.Children, node)
				continue
			}
		}
		roots = append(roots, node)
	}
	return items, roots, nil
}

func (s *DomainService) CreateFolder(ctx context.Context, name string, parentID *int64) (*domain.Folder, error) {
	if s.folders == nil {
		return nil, fmt.Errorf("文件夹仓储未初始化")
	}
	if parentID != nil {
		parent, err := s.folders.Get(ctx, *parentID)
		if err != nil {
			return nil, err
		}
		if parent == nil {
			return nil, fmt.Errorf("父文件夹不存在")
		}
	}
	return s.folders.Create(ctx, name, parentID)
}

func (s *DomainService) UpdateFolder(ctx context.Context, id int64, name string, parentID *int64) error {
	if s.folders == nil {
		return fmt.Errorf("文件夹仓储未初始化")
	}
	if parentID != nil {
		if *parentID == id {
			return fmt.Errorf("文件夹不能成为自己的父节点")
		}
		parent, err := s.folders.Get(ctx, *parentID)
		if err != nil {
			return err
		}
		if parent == nil {
			return fmt.Errorf("父文件夹不存在")
		}
	}
	return s.folders.Update(ctx, id, name, parentID)
}

func (s *DomainService) DeleteFolder(ctx context.Context, id int64) error {
	if s.folders == nil {
		return fmt.Errorf("文件夹仓储未初始化")
	}
	return s.folders.Delete(ctx, id)
}

func (s *DomainService) MoveToFolder(ctx context.Context, names []string, folderID *int64) (int64, error) {
	if s.folders == nil {
		return 0, fmt.Errorf("文件夹仓储未初始化")
	}
	if folderID != nil {
		folder, err := s.folders.Get(ctx, *folderID)
		if err != nil {
			return 0, err
		}
		if folder == nil {
			return 0, fmt.Errorf("目标文件夹不存在")
		}
	}
	return s.folders.MoveDomains(ctx, names, folderID)
}

// ExportDomains 导出全部监控域名及分组/备注/标签信息。
func (s *DomainService) ExportDomains(ctx context.Context) ([]DomainExportRecord, error) {
	entries, err := s.domains.List(ctx, false)
	if err != nil {
		return nil, err
	}
	results, err := s.results.LoadAll(ctx)
	if err != nil {
		return nil, err
	}
	folderNames := s.folderNames(ctx)
	out := make([]DomainExportRecord, 0, len(entries))
	for _, entry := range entries {
		record := DomainExportRecord{
			Domain: entry.Name, FolderID: entry.FolderID, Folder: folderNames[folderIDKey(entry.FolderID)],
			Tags: append([]string(nil), entry.Tags...), Note: entry.Note, Enabled: entry.Enabled,
			Notify: entry.Notify, AddedAt: &entry.CreatedAt,
		}
		if info, ok := results[strings.ToLower(entry.Name)]; ok {
			record.Status, record.Registrar, record.LastChecked = info.Status, info.Registrar, &info.LastChecked
		}
		out = append(out, record)
	}
	return out, nil
}

// ImportDomains 导入域名。mode 支持 skip、overwrite、deduplicate（及其常见别名）。
func (s *DomainService) ImportDomains(ctx context.Context, records []DomainImportRecord, mode string) (*DomainImportResult, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "", "skip", "deduplicate", "dedupe", "overwrite", "replace":
	default:
		return nil, fmt.Errorf("不支持的导入模式: %s", mode)
	}
	if mode == "dedupe" {
		mode = "deduplicate"
	}
	if mode == "replace" {
		mode = "overwrite"
	}
	result := &DomainImportResult{}
	seen := map[string]bool{}
	for _, item := range records {
		name := domain.Normalize(item.Domain)
		if name == "" || ValidateName(name) != nil {
			if name != "" {
				result.Invalid = append(result.Invalid, name)
			}
			continue
		}
		if seen[name] {
			result.Duplicates++
			if mode != "overwrite" {
				result.Skipped++
				continue
			}
		}
		seen[name] = true
		existing, err := s.domains.Get(ctx, name)
		if err != nil {
			return nil, err
		}
		if existing != nil && mode != "overwrite" {
			result.Skipped++
			continue
		}
		folderID, err := s.importFolderID(ctx, item)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			enabled, notify := true, true
			if item.Enabled != nil {
				enabled = *item.Enabled
			}
			if item.Notify != nil {
				notify = *item.Notify
			}
			if err := s.domains.Create(ctx, name, enabled, notify); err != nil {
				return nil, err
			}
			patch := repository.DomainPatch{FolderID: folderID}
			if item.Tags != nil {
				patch.Tags = &item.Tags
			}
			if item.Note != nil {
				patch.Note = item.Note
			}
			if err := s.domains.Update(ctx, name, patch); err != nil {
				return nil, err
			}
			result.Imported++
			s.scheduleImmediate(name)
			continue
		}
		patch := repository.DomainPatch{FolderID: folderID}
		if folderID == nil && item.Folder != "" {
			patch.ClearFolder = true
		}
		if item.Enabled != nil {
			patch.Enabled = item.Enabled
		}
		if item.Notify != nil {
			patch.Notify = item.Notify
		}
		if item.Tags != nil {
			patch.Tags = &item.Tags
		}
		if item.Note != nil {
			patch.Note = item.Note
		}
		if err := s.domains.Update(ctx, name, patch); err != nil {
			return nil, err
		}
		result.Overwritten++
	}
	return result, nil
}

func (s *DomainService) importFolderID(ctx context.Context, item DomainImportRecord) (*int64, error) {
	if item.FolderID != nil {
		if s.folders == nil {
			return nil, fmt.Errorf("文件夹仓储未初始化")
		}
		folder, err := s.folders.Get(ctx, *item.FolderID)
		if err != nil {
			return nil, err
		}
		if folder == nil {
			return nil, fmt.Errorf("文件夹不存在: %d", *item.FolderID)
		}
		return item.FolderID, nil
	}
	if strings.TrimSpace(item.Folder) == "" {
		return nil, nil
	}
	if s.folders == nil {
		return nil, fmt.Errorf("文件夹仓储未初始化")
	}
	folders, err := s.folders.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, folder := range folders {
		if strings.EqualFold(folder.Name, strings.TrimSpace(item.Folder)) {
			id := folder.ID
			return &id, nil
		}
	}
	folder, err := s.folders.Create(ctx, strings.TrimSpace(item.Folder), nil)
	if err != nil {
		return nil, err
	}
	return &folder.ID, nil
}

// RetryFailed 异步把 error/unknown 域名均摊到 30 秒窗口。
func (s *DomainService) RetryFailed(ctx context.Context) (int, error) {
	entries, err := s.domains.List(ctx, true)
	if err != nil {
		return 0, err
	}
	results, err := s.results.LoadAll(ctx)
	if err != nil {
		return 0, err
	}
	var names []string
	for _, entry := range entries {
		info, ok := results[strings.ToLower(entry.Name)]
		if !ok || info.Status == domain.StatusError || info.Status == domain.StatusUnknown {
			names = append(names, entry.Name)
		}
	}
	if len(names) == 0 || s.enqueue == nil {
		return len(names), nil
	}
	interval := 30 * time.Second
	if len(names) > 1 {
		interval /= time.Duration(len(names) - 1)
	}
	go func() {
		for i, name := range names {
			if i > 0 {
				time.Sleep(interval)
			}
			s.enqueue(name, domain.PriorityManual)
		}
	}()
	return len(names), nil
}

func (s *DomainService) folderNames(ctx context.Context) map[string]string {
	out := map[string]string{}
	if s.folders == nil {
		return out
	}
	items, err := s.folders.List(ctx)
	if err != nil {
		return out
	}
	for _, item := range items {
		out[folderIDKey(&item.ID)] = item.Name
	}
	return out
}

func folderIDKey(id *int64) string {
	if id == nil {
		return ""
	}
	return fmt.Sprintf("%d", *id)
}
