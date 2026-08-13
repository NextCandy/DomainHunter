package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/query"
	"DomainHunter/internal/repository"
)

// OverviewItem 概览页里的一行域名摘要
type OverviewItem struct {
	Domain     string              `json:"domain"`
	Status     domain.Status       `json:"status"`
	OldStatus  domain.Status       `json:"old_status,omitempty"`
	Registrar  string              `json:"registrar,omitempty"`
	ExpiryAt   *time.Time          `json:"expiry_at,omitempty"`
	Provider   string              `json:"provider,omitempty"`
	ObservedAt *time.Time          `json:"observed_at,omitempty"`
	Message    string              `json:"message,omitempty"`
	Review     *domain.ReviewState `json:"review,omitempty"`
}

type OverviewActionCounts struct {
	Available   int `json:"available"`
	DropWindow  int `json:"drop_window"`
	RenewalRisk int `json:"renewal_risk"`
	Review      int `json:"review"`
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
	ActionCounts    OverviewActionCounts   `json:"action_counts"`
}

// OverviewTrendPoint 是概览趋势图的一天数据。
//
// total / available / high_score / changes 保持前端当前契约；status_counts
// 是完整的逐状态计数，便于前端或 API 客户端按任意状态扩展展示。
type OverviewTrendPoint struct {
	Day          string                `json:"day"`
	Total        int                   `json:"total"`
	Available    int                   `json:"available"`
	HighScore    int                   `json:"high_score"`
	Changes      int                   `json:"changes"`
	StatusCounts map[domain.Status]int `json:"status_counts"`
}

// OverviewTrend 是 /api/v2/overview/trend 的稳定响应契约。
type OverviewTrend struct {
	Days   int                  `json:"days"`
	Points []OverviewTrendPoint `json:"points"`
}

const (
	defaultTrendDays = 7
	maxTrendDays     = 366
	trendDayLayout   = "2006-01-02"
)

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
			overview.ActionCounts.Review++
			continue
		}
		res.Priority = entry.Priority
		res.Review = domain.BuildReviewState(&res, now)
		if res.Review != nil && res.Review.Required {
			overview.ActionCounts.Review++
		}
		if res.Review == nil || !res.Review.Required {
			if res.Confidence == domain.ConfidenceHigh && res.Status == domain.StatusAvailable {
				overview.ActionCounts.Available++
			}
			if res.Confidence == domain.ConfidenceHigh && domain.IsDropStatus(res.Status) {
				overview.ActionCounts.DropWindow++
			}
		}
		if (res.Review == nil || !res.Review.Required) && res.ExpiryDate != nil && res.ExpiryDate.After(now) && res.ExpiryDate.Before(now.AddDate(0, 0, 7)) {
			overview.ActionCounts.RenewalRisk++
		}
		switch {
		case res.ExpiryDate != nil && res.ExpiryDate.After(now) && res.ExpiryDate.Before(horizon):
			overview.UpcomingExpiry = append(overview.UpcomingExpiry, OverviewItem{
				Domain: res.Name, Status: res.Status, Registrar: res.Registrar, ExpiryAt: res.ExpiryDate, Review: res.Review,
			})
		}
		if res.Status == domain.StatusAvailable {
			checked := res.LastChecked
			overview.RecentAvailable = append(overview.RecentAvailable, OverviewItem{
				Domain: res.Name, Status: res.Status, Provider: res.QueryMethod, ObservedAt: &checked, Review: res.Review,
			})
		}
		if res.Status == domain.StatusError {
			checked := res.LastChecked
			overview.QueryFailures = append(overview.QueryFailures, OverviewItem{
				Domain: res.Name, Status: res.Status, Provider: res.QueryMethod,
				ObservedAt: &checked, Message: res.ErrorMessage, Review: res.Review,
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

	if changes, err := s.observations.ListRecentChanges(ctx, 80); err == nil {
		type changeGroup struct {
			item  OverviewItem
			count int
		}
		groups := make(map[string]*changeGroup)
		order := make([]string, 0, len(changes))
		for _, change := range changes {
			observed := change.ObservedAt
			changeInfo := &domain.Info{Name: change.Domain, Status: change.Status, Registrar: change.Registrar, LastChecked: change.ObservedAt, QueryMethod: change.Provider}
			item := OverviewItem{
				Domain:     change.Domain,
				Status:     change.Status,
				Registrar:  change.Registrar,
				Provider:   change.Provider,
				ObservedAt: &observed,
				Review:     domain.BuildReviewState(changeInfo, now),
			}
			key := strings.ToLower(change.Domain) + "|" + string(change.Status)
			if group := groups[key]; group != nil {
				group.count++
				continue
			}
			groups[key] = &changeGroup{item: item, count: 1}
			order = append(order, key)
		}
		for _, key := range order {
			group := groups[key]
			if group.count > 1 {
				group.item.Message = fmt.Sprintf("同状态变化 ×%d", group.count)
			}
			overview.RecentChanges = append(overview.RecentChanges, group.item)
			if len(overview.RecentChanges) == 15 {
				break
			}
		}
	}

	overview.Providers = s.engine.Health().Snapshot(s.engine.Providers().Names())

	if observations, attempts, err := s.observations.Stats(ctx); err == nil {
		overview.History = map[string]int64{"observations": observations, "attempts": attempts}
	}
	return overview, nil
}

// Trend 返回最近 days 个自然日的观测趋势，包含没有观测记录的零值日期。
// 趋势查询是历史分析的可选扩展；旧的 ObservationRepository 实现没有实现
// ObservationAnalyticsRepository 时返回明确错误，不影响旧概览接口。
func (s *OverviewService) Trend(ctx context.Context, days int) (*OverviewTrend, error) {
	days = normalizeTrendDays(days)
	analytics, ok := s.observations.(repository.ObservationAnalyticsRepository)
	if !ok {
		return nil, fmt.Errorf("观测仓储不支持趋势分析")
	}

	now := time.Now().In(time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	start := today.AddDate(0, 0, 1-days)
	fromDay := start.Format(trendDayLayout)
	toDay := today.Format(trendDayLayout)
	counts, err := analytics.DailyStatusCounts(ctx, fromDay, toDay)
	if err != nil {
		return nil, fmt.Errorf("读取趋势数据失败: %w", err)
	}

	points := make([]OverviewTrendPoint, days)
	byDay := make(map[string]*OverviewTrendPoint, days)
	for i := range points {
		day := start.AddDate(0, 0, i).Format(trendDayLayout)
		points[i] = OverviewTrendPoint{
			Day:          day,
			StatusCounts: make(map[domain.Status]int, len(domain.AllStatuses())),
		}
		for _, status := range domain.AllStatuses() {
			points[i].StatusCounts[status] = 0
		}
		byDay[day] = &points[i]
	}

	for _, count := range counts {
		point := byDay[count.Day]
		if point == nil {
			continue
		}
		if count.Count < 0 {
			continue
		}
		point.Total += count.Count
		point.Changes += maxInt(count.Changed)
		point.HighScore += maxInt(count.HighConfidence)
		point.StatusCounts[count.Status] += count.Count
		if count.Status == domain.StatusAvailable {
			point.Available += count.Count
		}
	}

	return &OverviewTrend{Days: days, Points: points}, nil
}

func normalizeTrendDays(days int) int {
	if days <= 0 {
		return defaultTrendDays
	}
	if days > maxTrendDays {
		return maxTrendDays
	}
	return days
}

func maxInt(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func limitItems(items []OverviewItem, n int) []OverviewItem {
	if len(items) > n {
		return items[:n]
	}
	return items
}
