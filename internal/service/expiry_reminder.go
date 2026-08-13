package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/logger"
	"DomainHunter/internal/notification"
	"DomainHunter/internal/repository"
)

// ExpiryMilestones 是用户可配置/静音的四档到期提醒。
var ExpiryMilestones = []int{60, 30, 7, 1}

// ExpiryReminderService 扫描监控域名的到期日并发出幂等提醒。
//
// 到期提醒只读取已有 domain_results，不会触发新的 WHOIS/RDAP 请求；
// 因此启动扫描不会改变查询链路或给注册局增加流量。
type ExpiryReminderService struct {
	domains  repository.DomainRepository
	results  repository.ResultRepository
	dedup    repository.ExpiryReminderRepository
	history  repository.NotificationRepository
	notifier *notification.Manager
	interval time.Duration
	location *time.Location
	log      *logger.Logger
	mu       sync.Mutex
	running  bool
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

func NewExpiryReminderService(
	domains repository.DomainRepository,
	results repository.ResultRepository,
	dedup repository.ExpiryReminderRepository,
	history repository.NotificationRepository,
	notifier *notification.Manager,
	interval time.Duration,
) *ExpiryReminderService {
	if interval <= 0 {
		interval = time.Hour
	}
	return &ExpiryReminderService{
		domains: domains, results: results, dedup: dedup, history: history,
		notifier: notifier, interval: interval, location: time.Local,
		log: logger.Component("expiry-reminder"),
	}
}

func (s *ExpiryReminderService) Start(parent context.Context) {
	if s == nil || s.domains == nil || s.results == nil || s.dedup == nil {
		return
	}
	if parent == nil {
		parent = context.Background()
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.running, s.cancel = true, cancel
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if primed, err := s.PrimeOnce(ctx, time.Now()); err != nil && ctx.Err() == nil {
			s.log.Warn(logger.Fields{"error": err.Error()}, "到期提醒首轮扫描失败")
		} else if primed > 0 {
			s.log.Info(logger.Fields{"count": primed}, "到期提醒已静默初始化现有提醒窗口")
		}
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				if _, err := s.RunOnce(ctx, now); err != nil && ctx.Err() == nil {
					s.log.Warn(logger.Fields{"error": err.Error()}, "到期提醒扫描失败")
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (s *ExpiryReminderService) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
}

// PrimeOnce 在服务启动时只建立去重基线，不发送历史到期提醒。
// 这样重启或升级不会把已有的 60/30/7/1 天窗口一次性推送到所有渠道。
func (s *ExpiryReminderService) PrimeOnce(ctx context.Context, now time.Time) (int, error) {
	return s.scan(ctx, now, false)
}

// RunOnce 扫描一次并返回本轮新建的提醒数量。
func (s *ExpiryReminderService) RunOnce(ctx context.Context, now time.Time) (int, error) {
	return s.scan(ctx, now, true)
}

func (s *ExpiryReminderService) scan(ctx context.Context, now time.Time, notify bool) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if now.IsZero() {
		now = time.Now()
	}
	entries, err := s.domains.List(ctx, true)
	if err != nil {
		return 0, fmt.Errorf("读取到期提醒域名失败: %w", err)
	}
	results, err := s.results.LoadAll(ctx)
	if err != nil {
		return 0, fmt.Errorf("读取到期提醒结果失败: %w", err)
	}
	created := 0
	for _, entry := range entries {
		if !entry.Notify {
			continue // 单域名“忽略”通过 domains.notify 实现
		}
		info, ok := results[domain.Normalize(entry.Name)]
		if !ok || info.ExpiryDate == nil {
			continue
		}
		days := daysUntil(info.ExpiryDate, now, s.location)
		milestone, ok := expiryMilestone(days)
		if !ok {
			continue
		}
		expiryAt := *info.ExpiryDate
		sent, err := s.dedup.Sent(ctx, entry.Name, expiryAt, milestone)
		if err != nil {
			return created, err
		}
		if sent {
			continue
		}
		if !notify {
			if err := s.dedup.MarkSent(ctx, entry.Name, expiryAt, milestone, now); err != nil {
				return created, err
			}
			created++
			continue
		}
		event := notification.Event{
			Type: "expiry", Domain: entry.Name,
			Status:    fmt.Sprintf("expiry_%d", milestone),
			Message:   fmt.Sprintf("域名将在 %d 天后到期（到期日 %s）", milestone, expiryAt.In(s.location).Format("2006-01-02")),
			Timestamp: now,
		}
		if s.history != nil {
			if err := s.history.SaveEvent(ctx, event.Domain, event.Status, "", notification.EventCategory(event)); err != nil {
				return created, err
			}
		}
		if s.notifier != nil {
			s.notifier.Submit(event)
		}
		if err := s.dedup.MarkSent(ctx, entry.Name, expiryAt, milestone, now); err != nil {
			return created, err
		}
		created++
	}
	return created, nil
}

func daysUntil(expiry *time.Time, now time.Time, location *time.Location) int {
	if expiry == nil {
		return -1
	}
	localExpiry := expiry.In(location)
	localNow := now.In(location)
	expiryDay := time.Date(localExpiry.Year(), localExpiry.Month(), localExpiry.Day(), 0, 0, 0, 0, location)
	nowDay := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	return int(expiryDay.Sub(nowDay) / (24 * time.Hour))
}

func expiryMilestone(days int) (int, bool) {
	if days < 0 {
		return 0, false
	}
	switch {
	case days <= 1:
		return 1, true
	case days <= 7:
		return 7, true
	case days <= 30:
		return 30, true
	case days <= 60:
		return 60, true
	default:
		return 0, false
	}
}
