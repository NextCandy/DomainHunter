package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/notification"
	"DomainHunter/internal/repository"
)

type digestConfigStub struct {
	digest   repository.NotificationDigest
	marked   []time.Time
	getCalls int
}

func (r *digestConfigStub) GetDigest(context.Context) (*repository.NotificationDigest, error) {
	r.getCalls++
	digest := r.digest
	return &digest, nil
}

func (r *digestConfigStub) MarkDigestSent(_ context.Context, when time.Time) error {
	r.digest.LastSentAt = when
	r.marked = append(r.marked, when)
	return nil
}

type digestAnalyticsStub struct {
	changes []repository.ObservationChange
	fromDay string
	toDay   string
}

func (r *digestAnalyticsStub) DailyStatusCounts(context.Context, string, string) ([]repository.DailyStatusCount, error) {
	return nil, nil
}

func (r *digestAnalyticsStub) ChangesBetween(_ context.Context, fromDay, toDay string) ([]repository.ObservationChange, error) {
	r.fromDay, r.toDay = fromDay, toDay
	return append([]repository.ObservationChange(nil), r.changes...), nil
}

type digestSenderStub struct {
	events []notification.Event
	err    error
}

func (s *digestSenderStub) SendDigest(_ context.Context, event notification.Event) error {
	if s.err != nil {
		return s.err
	}
	s.events = append(s.events, event)
	return nil
}

func digestChange(name string, oldStatus, status domain.Status, observedAt time.Time) repository.ObservationChange {
	return repository.ObservationChange{
		Observation: domain.Observation{Domain: name, Status: status, ObservedAt: observedAt, Changed: true},
		OldStatus:   oldStatus,
	}
}

func TestNotificationDigestSendsPreviousDayOnceAtConfiguredLocalTime(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	config := &digestConfigStub{digest: repository.NotificationDigest{Enabled: true, Hour: 8, Minute: 0}}
	analytics := &digestAnalyticsStub{changes: []repository.ObservationChange{
		digestChange("example.com", domain.StatusRegistered, domain.StatusAvailable,
			time.Date(2026, 8, 10, 12, 0, 0, 0, location)),
	}}
	sender := &digestSenderStub{}
	service := NewNotificationDigestService(config, analytics, sender,
		NotificationDigestOptions{Location: location})
	now := time.Date(2026, 8, 11, 8, 0, 0, 0, location)

	if err := service.RunOnce(context.Background(), now); err != nil {
		t.Fatalf("到点发送摘要失败: %v", err)
	}
	if len(sender.events) != 1 || len(sender.events[0].Batch) != 1 {
		t.Fatalf("应发送一条包含变化的摘要: %+v", sender.events)
	}
	if analytics.fromDay != "2026-08-10" || analytics.toDay != "2026-08-10" {
		t.Fatalf("摘要应查询前一天，实际 %s 到 %s", analytics.fromDay, analytics.toDay)
	}
	if len(config.marked) != 1 || !config.marked[0].Equal(now) {
		t.Fatalf("发送成功后应持久化 last_sent_at: %+v", config.marked)
	}

	if err := service.RunOnce(context.Background(), now.Add(time.Minute)); err != nil {
		t.Fatalf("重复检查不应失败: %v", err)
	}
	if len(sender.events) != 1 || len(config.marked) != 1 {
		t.Fatal("同一前一天摘要不应重复发送或重复标记")
	}
}

func TestNotificationDigestDoesNotSendBeforeScheduleOrAcrossMidnight(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	config := &digestConfigStub{digest: repository.NotificationDigest{Enabled: true, Hour: 23, Minute: 30}}
	analytics := &digestAnalyticsStub{changes: []repository.ObservationChange{
		digestChange("example.com", domain.StatusRegistered, domain.StatusAvailable, time.Now()),
	}}
	sender := &digestSenderStub{}
	service := NewNotificationDigestService(config, analytics, sender,
		NotificationDigestOptions{Location: location})

	beforeMidnight := time.Date(2026, 8, 11, 23, 29, 0, 0, location)
	if err := service.RunOnce(context.Background(), beforeMidnight); err != nil {
		t.Fatalf("配置时间前检查失败: %v", err)
	}
	if len(sender.events) != 0 || len(config.marked) != 0 {
		t.Fatal("配置时间前不应发送或标记摘要")
	}

	atMidnight := time.Date(2026, 8, 12, 0, 5, 0, 0, location)
	if err := service.RunOnce(context.Background(), atMidnight); err != nil {
		t.Fatalf("跨午夜检查失败: %v", err)
	}
	if len(sender.events) != 0 || len(config.marked) != 0 {
		t.Fatal("跨午夜后但未到当天配置时间，不应提前发送前一天摘要")
	}
}

func TestNotificationDigestRetriesSendFailureWithoutMarking(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	config := &digestConfigStub{digest: repository.NotificationDigest{Enabled: true, Hour: 8, Minute: 0}}
	analytics := &digestAnalyticsStub{changes: []repository.ObservationChange{
		digestChange("example.com", domain.StatusRegistered, domain.StatusAvailable, time.Now()),
	}}
	sender := &digestSenderStub{err: errors.New("channel down")}
	service := NewNotificationDigestService(config, analytics, sender,
		NotificationDigestOptions{Location: location})
	now := time.Date(2026, 8, 11, 8, 0, 0, 0, location)

	if err := service.RunOnce(context.Background(), now); err == nil {
		t.Fatal("通知渠道失败时应返回错误")
	}
	if len(config.marked) != 0 {
		t.Fatal("发送失败时不得写入 last_sent_at")
	}

	sender.err = nil
	if err := service.RunOnce(context.Background(), now.Add(time.Minute)); err != nil {
		t.Fatalf("失败后的下一轮应重试成功: %v", err)
	}
	if len(sender.events) != 1 || len(config.marked) != 1 {
		t.Fatalf("重试成功后应只发送并标记一次: events=%d marked=%d", len(sender.events), len(config.marked))
	}
}
