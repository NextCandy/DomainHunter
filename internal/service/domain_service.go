package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/logger"
	"DomainHunter/internal/registry"
	"DomainHunter/internal/repository"
)

// ListFilter 域名列表筛选条件
type ListFilter struct {
	Search    string
	Status    string
	TLD       string
	Registrar string
	Provider  string
	Tag       string
	Favorite  bool
	Sort      string
	Order     string
	Page      int
	Limit     int
}

// ListResult 分页结果
type ListResult struct {
	Domains       []*domain.Info `json:"domains"`
	Total         int            `json:"total"`
	TotalFiltered int            `json:"total_filtered"`
	Page          int            `json:"page"`
	Limit         int            `json:"limit"`
	TotalPages    int            `json:"total_pages"`
	HasNext       bool           `json:"has_next"`
	HasPrev       bool           `json:"has_prev"`
	DataStatus    string         `json:"data_status"`
}

// BatchAddResult 批量添加结果
type BatchAddResult struct {
	Added              int      `json:"added_count"`
	Invalid            []string `json:"invalid_domains"`
	Unsupported        []string `json:"unsupported_domains"`
	Duplicate          []string `json:"duplicate_domains"`
	InvalidCount       int      `json:"invalid_count"`
	UnsupportedCount   int      `json:"unsupported_count"`
	DuplicateCount     int      `json:"duplicate_count"`
	AddedDomainsSample []string `json:"added_domains,omitempty"`
}

// Enqueuer 把域名放入调度队列
type Enqueuer interface {
	Enqueue(name string, priority domain.Priority, reason string) bool
}

// DomainService 域名管理业务
type DomainService struct {
	domains      repository.DomainRepository
	results      repository.ResultRepository
	observations repository.ObservationRepository
	log          *logger.Logger
	enqueue      func(name string, priority domain.Priority)
}

// NewDomainService 创建域名服务
func NewDomainService(
	domains repository.DomainRepository,
	results repository.ResultRepository,
	observations repository.ObservationRepository,
) *DomainService {
	return &DomainService{
		domains:      domains,
		results:      results,
		observations: observations,
		log:          logger.Component("domain"),
	}
}

// SetEnqueuer 注入调度入队回调（新增域名后立刻排队查询）
func (s *DomainService) SetEnqueuer(fn func(name string, priority domain.Priority)) { s.enqueue = fn }

// ValidateName 校验域名格式
func ValidateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("域名不能为空")
	}
	if len(name) > 253 {
		return fmt.Errorf("域名长度不能超过253个字符")
	}
	for _, char := range name {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '.' || char == '-') {
			return fmt.Errorf("域名包含无效字符: %c", char)
		}
	}

	parts := strings.Split(name, ".")
	if len(parts) < 2 {
		return fmt.Errorf("域名必须包含至少一个点")
	}
	for i, part := range parts {
		if part == "" {
			return fmt.Errorf("域名部分不能为空")
		}
		if len(part) > 63 {
			return fmt.Errorf("域名部分长度不能超过63个字符")
		}
		if strings.HasPrefix(part, "-") || strings.HasSuffix(part, "-") {
			return fmt.Errorf("域名部分不能以连字符开始或结束: %s", part)
		}
		if i == len(parts)-1 {
			allDigits := true
			for _, char := range part {
				if char < '0' || char > '9' {
					allDigits = false
					break
				}
			}
			if allDigits {
				return fmt.Errorf("顶级域名不能全是数字")
			}
		}
	}
	return nil
}

// List 返回带筛选与分页的域名列表
func (s *DomainService) List(ctx context.Context, filter ListFilter) (*ListResult, error) {
	entries, err := s.domains.List(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("获取域名列表失败: %w", err)
	}
	results, err := s.results.LoadAll(ctx)
	if err != nil {
		s.log.Warn(logger.Fields{"error": err.Error()}, "加载域名结果失败")
		results = map[string]domain.Info{}
	}

	all := make([]*domain.Info, 0, len(entries))
	for i := range entries {
		all = append(all, mergeEntry(entries[i], results))
	}

	filtered := applyFilter(all, filter)
	sortInfos(filtered, filter.Sort, filter.Order)

	page, limit := filter.Page, filter.Limit
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 500 {
		limit = 10
	}

	total := len(filtered)
	start := (page - 1) * limit
	end := start + limit
	if start >= total {
		start, end = 0, 0
	} else if end > total {
		end = total
	}

	var paginated []*domain.Info
	if start < end {
		paginated = filtered[start:end]
	}

	out := &ListResult{
		Domains:       paginated,
		Total:         len(entries),
		TotalFiltered: total,
		Page:          page,
		Limit:         limit,
		TotalPages:    (total + limit - 1) / limit,
		HasNext:       end < total,
		HasPrev:       page > 1,
		DataStatus:    "ok",
	}
	switch {
	case len(entries) == 0:
		out.DataStatus = "empty"
	case len(paginated) == 0:
		out.DataStatus = "no_results"
	}
	return out, nil
}

func mergeEntry(entry domain.Domain, results map[string]domain.Info) *domain.Info {
	key := strings.ToLower(entry.Name)
	created := entry.CreatedAt

	if res, ok := results[key]; ok {
		info := res
		info.AddedAt = &created
		info.Favorite = entry.Favorite
		info.Tags = entry.Tags
		info.Note = entry.Note
		info.NextCheckAt = entry.NextCheckAt
		return &info
	}
	return &domain.Info{
		Name:        entry.Name,
		Status:      domain.StatusUnknown,
		QueryMethod: "pending",
		LastChecked: time.Time{},
		AddedAt:     &created,
		Favorite:    entry.Favorite,
		Tags:        entry.Tags,
		Note:        entry.Note,
		NextCheckAt: entry.NextCheckAt,
	}
}

func applyFilter(items []*domain.Info, filter ListFilter) []*domain.Info {
	search := strings.ToLower(strings.TrimSpace(filter.Search))
	registrarFilter := strings.ToLower(strings.TrimSpace(filter.Registrar))
	tldFilter := strings.ToLower(strings.Trim(strings.TrimSpace(filter.TLD), "."))
	tagFilter := strings.ToLower(strings.TrimSpace(filter.Tag))

	out := make([]*domain.Info, 0, len(items))
	for _, item := range items {
		if search != "" && !strings.Contains(strings.ToLower(item.Name), search) {
			continue
		}
		if filter.Status != "" && string(item.Status) != filter.Status {
			continue
		}
		if tldFilter != "" && !strings.HasSuffix(strings.ToLower(item.Name), "."+tldFilter) {
			continue
		}
		if registrarFilter != "" && !strings.Contains(strings.ToLower(item.Registrar), registrarFilter) {
			continue
		}
		if filter.Provider != "" && item.QueryMethod != filter.Provider {
			continue
		}
		if filter.Favorite && !item.Favorite {
			continue
		}
		if tagFilter != "" && !hasTag(item.Tags, tagFilter) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func hasTag(tags []string, want string) bool {
	for _, tag := range tags {
		if strings.EqualFold(strings.TrimSpace(tag), want) {
			return true
		}
	}
	return false
}

func sortInfos(items []*domain.Info, field, order string) {
	if field == "" {
		return
	}
	desc := strings.EqualFold(order, "desc")

	less := func(i, j int) bool { return items[i].Name < items[j].Name }
	switch field {
	case "name":
	case "status":
		less = func(i, j int) bool {
			return domain.GetStatusInfo(items[i].Status).Priority < domain.GetStatusInfo(items[j].Status).Priority
		}
	case "registrar":
		less = func(i, j int) bool { return items[i].Registrar < items[j].Registrar }
	case "expiry":
		less = func(i, j int) bool { return beforePtr(items[i].ExpiryDate, items[j].ExpiryDate) }
	case "last_checked":
		less = func(i, j int) bool { return items[i].LastChecked.Before(items[j].LastChecked) }
	case "next_check":
		less = func(i, j int) bool { return beforePtr(items[i].NextCheckAt, items[j].NextCheckAt) }
	case "added":
		less = func(i, j int) bool { return beforePtr(items[i].AddedAt, items[j].AddedAt) }
	default:
		return
	}

	sort.SliceStable(items, func(i, j int) bool {
		if desc {
			return less(j, i)
		}
		return less(i, j)
	})
}

func beforePtr(a, b *time.Time) bool {
	switch {
	case a == nil && b == nil:
		return false
	case a == nil:
		return false
	case b == nil:
		return true
	default:
		return a.Before(*b)
	}
}

// Get 返回单个域名的当前状态
func (s *DomainService) Get(ctx context.Context, name string) (*domain.Info, error) {
	name = domain.Normalize(name)
	entry, err := s.domains.Get(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("读取域名失败: %w", err)
	}

	info, err := s.results.Get(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("读取域名信息失败: %w", err)
	}
	if info == nil {
		info = &domain.Info{
			Name:        name,
			Status:      domain.StatusUnknown,
			QueryMethod: "pending",
			LastChecked: time.Now(),
		}
	}
	if entry != nil {
		created := entry.CreatedAt
		info.AddedAt = &created
		info.Favorite = entry.Favorite
		info.Tags = entry.Tags
		info.Note = entry.Note
		info.NextCheckAt = entry.NextCheckAt
	}
	return info, nil
}

// Detail 域名详情：当前状态 + 观测历史 + 查询尝试
type Detail struct {
	Info     *domain.Info         `json:"info"`
	History  []domain.Observation `json:"history"`
	Attempts []domain.Attempt     `json:"attempts"`
}

// GetDetail 组装详情页数据。
//
// domain_results 只存当前快照，不含证据；这里用最近一次观测把可信度补回去，
// 并用该次观测的查询尝试还原"每个查询源分别看到了什么"。
func (s *DomainService) GetDetail(ctx context.Context, name string, historyLimit, attemptLimit int) (*Detail, error) {
	info, err := s.Get(ctx, name)
	if err != nil {
		return nil, err
	}

	detail := &Detail{Info: info}
	history, err := s.observations.ListByDomain(ctx, name, historyLimit)
	if err != nil {
		s.log.Warn(logger.Fields{"domain": name, "error": err.Error()}, "读取观测历史失败")
	} else {
		detail.History = history
	}

	if attempts, err := s.observations.ListAttempts(ctx, name, attemptLimit); err != nil {
		s.log.Warn(logger.Fields{"domain": name, "error": err.Error()}, "读取查询尝试失败")
	} else {
		detail.Attempts = attempts
	}

	if len(detail.History) > 0 {
		latest := detail.History[0]
		if info.Confidence == "" {
			info.Confidence = latest.Confidence
		}
		if len(info.Evidence) == 0 {
			if attempts, err := s.observations.ListAttemptsByObservation(ctx, latest.ID); err == nil {
				info.Evidence = toEvidence(attempts)
			}
		}
	}
	return detail, nil
}

func toEvidence(attempts []domain.Attempt) []domain.Evidence {
	out := make([]domain.Evidence, 0, len(attempts))
	for _, attempt := range attempts {
		out = append(out, domain.Evidence{
			Provider:  attempt.Provider,
			Status:    attempt.Status,
			LatencyMS: attempt.LatencyMS,
			Error:     attempt.ErrorMessage,
			QueriedAt: attempt.QueriedAt,
		})
	}
	return out
}

// History 返回域名的状态观测历史
func (s *DomainService) History(ctx context.Context, name string, limit int) ([]domain.Observation, error) {
	return s.observations.ListByDomain(ctx, domain.Normalize(name), limit)
}

// Attempts 返回域名的查询尝试历史
func (s *DomainService) Attempts(ctx context.Context, name string, limit int) ([]domain.Attempt, error) {
	return s.observations.ListAttempts(ctx, domain.Normalize(name), limit)
}

// RecentChanges 返回全局最近的状态变化
func (s *DomainService) RecentChanges(ctx context.Context, limit int) ([]domain.Observation, error) {
	return s.observations.ListRecentChanges(ctx, limit)
}

// Add 添加单个域名
func (s *DomainService) Add(ctx context.Context, name string) (string, error) {
	name = domain.Normalize(name)
	if err := ValidateName(name); err != nil {
		return "", fmt.Errorf("域名格式无效: %w", err)
	}
	if registry.FindBestTLD(name) == "" {
		return "", fmt.Errorf("该后缀目前不支持进行监控")
	}

	existing, err := s.domains.Get(ctx, name)
	if err != nil {
		return "", fmt.Errorf("检查域名失败: %w", err)
	}
	if existing != nil {
		return "", fmt.Errorf("域名已存在")
	}
	if err := s.domains.Create(ctx, name, true, true); err != nil {
		return "", err
	}
	s.scheduleImmediate(name)
	return name, nil
}

// BatchAdd 批量添加域名
func (s *DomainService) BatchAdd(ctx context.Context, names []string) (*BatchAddResult, error) {
	entries, err := s.domains.List(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("获取已有域名列表失败: %w", err)
	}
	existing := make(map[string]bool, len(entries))
	for _, entry := range entries {
		existing[strings.ToLower(entry.Name)] = true
	}

	out := &BatchAddResult{}
	for _, raw := range names {
		name := domain.Normalize(raw)
		if name == "" {
			continue
		}
		if err := ValidateName(name); err != nil {
			out.Invalid = append(out.Invalid, name)
			continue
		}
		if registry.FindBestTLD(name) == "" {
			out.Unsupported = append(out.Unsupported, name)
			continue
		}
		if existing[name] {
			out.Duplicate = append(out.Duplicate, name)
			continue
		}
		if err := s.domains.Create(ctx, name, true, true); err != nil {
			s.log.Warn(logger.Fields{"domain": name, "error": err.Error()}, "批量添加域名失败")
			continue
		}
		existing[name] = true
		out.Added++
		s.scheduleImmediate(name)
	}

	out.InvalidCount = len(out.Invalid)
	out.UnsupportedCount = len(out.Unsupported)
	out.DuplicateCount = len(out.Duplicate)
	return out, nil
}

func (s *DomainService) scheduleImmediate(name string) {
	if s.enqueue != nil {
		s.enqueue(name, domain.PriorityManual)
	}
}

// Remove 删除域名
func (s *DomainService) Remove(ctx context.Context, name string) error {
	return s.domains.Delete(ctx, domain.Normalize(name))
}

// RemoveMany 批量删除域名
func (s *DomainService) RemoveMany(ctx context.Context, names []string) (int64, error) {
	return s.domains.DeleteMany(ctx, names)
}

// Update 更新域名属性（收藏、备注、标签、通知开关）
func (s *DomainService) Update(ctx context.Context, name string, patch repository.DomainPatch) error {
	return s.domains.Update(ctx, domain.Normalize(name), patch)
}

// CleanOrphaned 清理孤立数据
func (s *DomainService) CleanOrphaned(ctx context.Context) (int64, int64, error) {
	return s.domains.CleanOrphaned(ctx)
}

// StatusCounts 返回各状态的域名数量（只统计当前监控列表内的域名）
func (s *DomainService) StatusCounts(ctx context.Context) (map[domain.Status]int, int, error) {
	entries, err := s.domains.List(ctx, true)
	if err != nil {
		return nil, 0, err
	}
	results, err := s.results.LoadAll(ctx)
	if err != nil {
		return nil, 0, err
	}

	counts := make(map[domain.Status]int, len(domain.AllStatuses()))
	for _, entry := range entries {
		if res, ok := results[strings.ToLower(entry.Name)]; ok {
			counts[res.Status]++
		} else {
			counts[domain.StatusUnknown]++
		}
	}
	return counts, len(entries), nil
}
