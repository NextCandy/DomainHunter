package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/logger"
	"DomainHunter/internal/notification"
	"DomainHunter/internal/repository"
)

const notificationDigestDayLayout = "2006-01-02"

// DigestSender 是每日摘要所需的最小通知发送能力。
// notification.Manager 实现该接口；测试可以注入同步可观测的替身。
type DigestSender interface {
	SendDigest(ctx context.Context, event notification.Event) error
}

// NotificationDigestOptions 控制定时器轮询与摘要日期使用的时区。
type NotificationDigestOptions struct {
	PollInterval time.Duration
	Location     *time.Location
}

func (o NotificationDigestOptions) normalized() NotificationDigestOptions {
	if o.PollInterval <= 0 {
		o.PollInterval = time.Minute
	}
	if o.Location == nil {
		o.Location = time.Local
	}
	return o
}

// NotificationDigestService 每日摘要调度器。
//
// 它独立于域名查询 Scheduler 运行，发送失败或数据库写入失败时不更新
// last_sent_at，下一轮会继续重试；成功后同一自然日不会再次发送。
type NotificationDigestService struct {
	config       repository.NotificationDigestRepository
	observations repository.ObservationAnalyticsRepository
	sender       DigestSender
	opts         NotificationDigestOptions
	log          *logger.Logger

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	runMu   sync.Mutex
}

// NewNotificationDigestService 创建每日摘要调度器。
func NewNotificationDigestService(
	config repository.NotificationDigestRepository,
	observations repository.ObservationAnalyticsRepository,
	sender DigestSender,
	options ...NotificationDigestOptions,
) *NotificationDigestService {
	var opts NotificationDigestOptions
	if len(options) > 0 {
		opts = options[0]
	}
	return &NotificationDigestService{
		config:       config,
		observations: observations,
		sender:       sender,
		opts:         opts.normalized(),
		log:          logger.Component("notification-digest"),
	}
}

// Start 启动独立的每日摘要轮询协程。
func (s *NotificationDigestService) Start(parent context.Context) {
	if s == nil {
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
	s.cancel = cancel
	s.running = true
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.loop(ctx)
	}()
}

// Stop 停止摘要轮询，并等待正在进行的摘要发送结束。
func (s *NotificationDigestService) Stop() {
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

func (s *NotificationDigestService) loop(ctx context.Context) {
	// 启动时先检查一次，避免恰好在服务启动前错过轮询窗口。
	if err := s.RunOnce(ctx, time.Now()); err != nil && ctx.Err() == nil {
		s.log.Warn(logger.Fields{"error": err.Error()}, "每日通知摘要首轮检查失败")
	}

	ticker := time.NewTicker(s.opts.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			if err := s.RunOnce(ctx, now); err != nil && ctx.Err() == nil {
				s.log.Warn(logger.Fields{"error": err.Error()}, "每日通知摘要发送失败，将在下一轮重试")
			}
		case <-ctx.Done():
			return
		}
	}
}

// RunOnce 在给定时间检查并至多发送一次前一天摘要。
// now 主要用于测试与处理调度边界；传入零值时使用当前时间。
func (s *NotificationDigestService) RunOnce(ctx context.Context, now time.Time) error {
	if s == nil {
		return errors.New("每日通知摘要服务未初始化")
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()

	if s.config == nil || s.observations == nil || s.sender == nil {
		return errors.New("每日通知摘要依赖未初始化")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if now.IsZero() {
		now = time.Now()
	}
	localNow := now.In(s.opts.Location)

	digest, err := s.config.GetDigest(ctx)
	if err != nil {
		return fmt.Errorf("读取通知摘要配置失败: %w", err)
	}
	if digest == nil {
		return errors.New("通知摘要配置为空")
	}
	if !digest.Enabled {
		return nil
	}
	if digest.Hour < 0 || digest.Hour > 23 || digest.Minute < 0 || digest.Minute > 59 {
		return fmt.Errorf("通知摘要时间无效: %02d:%02d", digest.Hour, digest.Minute)
	}

	scheduledAt := time.Date(localNow.Year(), localNow.Month(), localNow.Day(),
		digest.Hour, digest.Minute, 0, 0, s.opts.Location)
	// 摘要属于前一天，必须等今天的配置时间到达；跨午夜不会提前发送。
	if localNow.Before(scheduledAt) {
		return nil
	}
	targetDay := localNow.AddDate(0, 0, -1).Format(notificationDigestDayLayout)
	// last_sent_at 记录实际发送时间（今天），而摘要内容属于昨天；因此按本次
	// 调度日去重，而不是拿发送日与 targetDay 直接比较。
	lastSentLocal := digest.LastSentAt.In(s.opts.Location)
	if !digest.LastSentAt.IsZero() &&
		lastSentLocal.Format(notificationDigestDayLayout) == localNow.Format(notificationDigestDayLayout) &&
		!lastSentLocal.Before(scheduledAt) {
		return nil
	}

	changes, err := s.observations.ChangesBetween(ctx, targetDay, targetDay)
	if err != nil {
		return fmt.Errorf("读取 %s 状态变化失败: %w", targetDay, err)
	}
	if len(changes) > 0 {
		event := notification.Event{
			Type:      "notification_digest",
			Timestamp: localNow,
			Batch:     make([]notification.Event, 0, len(changes)),
		}
		for _, change := range changes {
			oldStatus := string(change.OldStatus)
			currentStatus := string(change.Observation.Status)
			message := ""
			if oldStatus != "" {
				message = domain.GetStatusChangeMessage(change.Observation.Domain,
					change.OldStatus, change.Observation.Status)
			}
			event.Batch = append(event.Batch, notification.Event{
				Type:      "status_change",
				Domain:    change.Observation.Domain,
				Status:    currentStatus,
				OldStatus: oldStatus,
				Message:   message,
				Timestamp: change.Observation.ObservedAt,
			})
		}
		if err := s.sender.SendDigest(ctx, event); err != nil {
			return fmt.Errorf("发送 %s 通知摘要失败: %w", targetDay, err)
		}
	}

	// 无变化也记录已处理，避免每分钟重复扫描同一天；发送失败则不会走到这里。
	if err := s.config.MarkDigestSent(ctx, localNow); err != nil {
		return fmt.Errorf("记录通知摘要发送时间失败: %w", err)
	}
	return nil
}
