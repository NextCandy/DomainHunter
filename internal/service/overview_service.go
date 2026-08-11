package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/query"
	"DomainHunter/internal/repository"
)

// OverviewItem 概览页里的一行域名摘要
type OverviewItem struct {
	Domain     string        `json:"domain"`
	Status     domain.Status `json:"status"`
	OldStatus  domain.Status `json:"old_status,omitempty"`
	Registrar  string        `json:"registrar,omitempty"`
	ExpiryAt   *time.Time    `json:"expiry_at,omitempty"`
	Provider   string        `json:"provider,omitempty"`
	ObservedAt *time.Time    `json:"observed_at,omitempty"`
	Message    string        `json:"message,omitempty"`
}

// Overview 概览页数据
type Overview struct {
	Total           int                    `json:"total"`
	StatusCounts    map[domain.Status]int  `json:"status_counts"`
	RecentChanges   []OverviewItem         `json:"recent_changes"`
	UpcomingExpiry  []OverviewItem         `json:"upcoming_expiry"`
	RecentAvailable []OverviewItem         `json:"recent_available"`
	QueryFailures   []OverviewItem         `json:"query_failures"`
	Providers       []query.ProviderHealth `json:"providers"`
	Monitor         map[string]any         `json:"monitor"`
	History         map[string]int64       `json:"history"`
}

// OverviewService 汇总概览页需要的数据
type OverviewService struct {
	domains      repository.DomainRepository
	results      repository.ResultRepository
	observations repository.ObservationRepository
	engine       *query.Engine
	monitor      *MonitorService
	domainSvc    *DomainService
}

// NewOverviewService 创建概览服务
func NewOverviewService(
	domains repository.DomainRepository,
	results repository.ResultRepository,
	observations repository.ObservationRepository,
	engine *query.Engine,
	monitor *MonitorService,
	domainSvc *DomainService,
) *OverviewService {
	return &OverviewService{
		domains:      domains,
		results:      results,
		observations: observations,
		engine:       engine,
		monitor:      monitor,
		domainSvc:    domainSvc,
	}
}

// Build 组装概览数据
func (s *OverviewService) Build(ctx context.Context) (*Overview, error) {
	counts, total, err := s.domainSvc.StatusCounts(ctx)
	if err != nil {
		return nil, err
	}

	entries, err := s.domains.List(ctx, true)
	if err != nil {
		return nil, err
	}
	results, err := s.results.LoadAll(ctx)
	if err != nil {
		return nil, err
	}

	overview := &Overview{
		Total:        total,
		StatusCounts: counts,
		Monitor:      s.monitor.Stats(ctx, s.domainSvc),
	}

	// 保证所有状态都有键，前端不需要处理缺失
	for _, status := range domain.AllStatuses() {
		if _, ok := overview.StatusCounts[status]; !ok {
			overview.StatusCounts[status] = 0
		}
	}

	now := time.Now()
	horizon := now.AddDate(0, 0, 60)
	for _, entry := range entries {
		res, ok := results[strings.ToLower(entry.Name)]
		if !ok {
			continue
		}
		switch {
		case res.ExpiryDate != nil && res.ExpiryDate.After(now) && res.ExpiryDate.Before(horizon):
			overview.UpcomingExpiry = append(overview.UpcomingExpiry, OverviewItem{
				Domain: res.Name, Status: res.Status, Registrar: res.Registrar, ExpiryAt: res.ExpiryDate,
			})
		}
		if res.Status == domain.StatusAvailable {
			checked := res.LastChecked
			overview.RecentAvailable = append(overview.RecentAvailable, OverviewItem{
				Domain: res.Name, Status: res.Status, Provider: res.QueryMethod, ObservedAt: &checked,
			})
		}
		if res.Status == domain.StatusError {
			checked := res.LastChecked
			overview.QueryFailures = append(overview.QueryFailures, OverviewItem{
				Domain: res.Name, Status: res.Status, Provider: res.QueryMethod,
				ObservedAt: &checked, Message: res.ErrorMessage,
			})
		}
	}

	sort.Slice(overview.UpcomingExpiry, func(i, j int) bool {
		return beforePtr(overview.UpcomingExpiry[i].ExpiryAt, overview.UpcomingExpiry[j].ExpiryAt)
	})
	sort.Slice(overview.RecentAvailable, func(i, j int) bool {
		return beforePtr(overview.RecentAvailable[j].ObservedAt, overview.RecentAvailable[i].ObservedAt)
	})
	sort.Slice(overview.QueryFailures, func(i, j int) bool {
		return beforePtr(overview.QueryFailures[j].ObservedAt, overview.QueryFailures[i].ObservedAt)
	})
	overview.UpcomingExpiry = limitItems(overview.UpcomingExpiry, 10)
	overview.RecentAvailable = limitItems(overview.RecentAvailable, 10)
	overview.QueryFailures = limitItems(overview.QueryFailures, 10)

	if changes, err := s.observations.ListRecentChanges(ctx, 15); err == nil {
		for _, change := range changes {
			observed := change.ObservedAt
			overview.RecentChanges = append(overview.RecentChanges, OverviewItem{
				Domain:     change.Domain,
				Status:     change.Status,
				Registrar:  change.Registrar,
				Provider:   change.Provider,
				ObservedAt: &observed,
			})
		}
	}

	overview.Providers = s.engine.Health().Snapshot(s.engine.Providers().Names())

	if observations, attempts, err := s.observations.Stats(ctx); err == nil {
		overview.History = map[string]int64{"observations": observations, "attempts": attempts}
	}
	return overview, nil
}

func limitItems(items []OverviewItem, n int) []OverviewItem {
	if len(items) > n {
		return items[:n]
	}
	return items
}
