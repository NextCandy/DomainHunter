package notification

import (
	"context"
	"errors"
	"testing"
	"time"

	"DomainHunter/internal/repository"
)

type digestTestNotifier struct {
	enabled bool
	events  []Event
	err     error
}

func (n *digestTestNotifier) Name() string               { return "test" }
func (n *digestTestNotifier) Enabled() bool              { return n.enabled }
func (n *digestTestNotifier) Test(context.Context) error { return nil }
func (n *digestTestNotifier) Send(_ context.Context, event Event) error {
	if n.err != nil {
		return n.err
	}
	n.events = append(n.events, event)
	return nil
}

func TestNotificationRuleFiltersBeforeSubmission(t *testing.T) {
	manager := NewManager(nil)
	manager.SetRules([]repository.NotificationRule{{
		Name: "available-only", Enabled: true, Statuses: []string{"available"}, PerDomain: true,
	}})

	if manager.Allows(Event{Type: "status_change", Domain: "example.com", Status: "registered"}) {
		t.Fatal("不匹配状态的通知应被规则过滤")
	}
	if !manager.Allows(Event{Type: "status_change", Domain: "example.com", Status: "available"}) {
		t.Fatal("匹配状态的通知不应被过滤")
	}
}

func TestSendDigestUsesEnabledNotifierAndDigestTemplate(t *testing.T) {
	notifier := &digestTestNotifier{enabled: true}
	manager := NewManager(nil)
	manager.Register(notifier)
	manager.SetTemplates([]repository.NotificationTemplate{{
		Name: "digest", EventType: "notification_digest", Subject: "摘要 {{time}}", Body: "变化 {{message}}", Enabled: true,
	}})

	event := Event{
		Type:      "notification_digest",
		Timestamp: time.Date(2026, 8, 11, 8, 0, 0, 0, time.UTC),
		Batch:     []Event{{Domain: "example.com", Status: "available"}},
	}
	if err := manager.SendDigest(context.Background(), event); err != nil {
		t.Fatalf("发送摘要失败: %v", err)
	}
	if len(notifier.events) != 1 {
		t.Fatalf("应调用启用的通知渠道一次，实际 %d", len(notifier.events))
	}
	got := notifier.events[0]
	if got.Subject != "摘要 2026-08-11 08:00:00" || got.Body != "变化 " {
		t.Fatalf("摘要模板未生效: %+v", got)
	}
}

func TestSendDigestRetriesWhenNoNotifierIsEnabled(t *testing.T) {
	manager := NewManager(nil)
	manager.Register(&digestTestNotifier{enabled: false})
	if err := manager.SendDigest(context.Background(), Event{Type: "notification_digest"}); !errors.Is(err, ErrNoEnabledNotifiers) {
		t.Fatalf("没有启用渠道时应返回可重试错误，实际 %v", err)
	}
}
