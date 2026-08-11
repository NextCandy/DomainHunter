package notification

import (
	"testing"

	"DomainHunter/internal/repository"
)

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
