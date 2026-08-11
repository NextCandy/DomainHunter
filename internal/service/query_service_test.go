package service

import (
	"context"
	"testing"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/notification"
	"DomainHunter/internal/query"
	"DomainHunter/internal/repository"
)

func TestNextIntervalKeepsConfiguredBaseForNormalStatuses(t *testing.T) {
	base := 7 * time.Minute
	for _, status := range []domain.Status{
		domain.StatusRegistered, domain.StatusAvailable, domain.StatusGrace,
		domain.StatusRedemption, domain.StatusPendingDelete, domain.StatusUnknown,
		domain.StatusTransferLocked, domain.StatusHold, domain.StatusExpired,
	} {
		if got := NextInterval(status, base, 0); got != base {
			t.Fatalf("%s 应使用配置的检查间隔 %v，实际 %v", status, base, got)
		}
	}
}

func TestNextIntervalBacksOffOnError(t *testing.T) {
	base := time.Minute
	if got := NextInterval(domain.StatusError, base, 1); got != 5*time.Minute {
		t.Fatalf("首次失败应 5 分钟后重试，实际 %v", got)
	}
	if got := NextInterval(domain.StatusError, base, 2); got != 15*time.Minute {
		t.Fatalf("第二次失败应 15 分钟后重试，实际 %v", got)
	}
	if got := NextInterval(domain.StatusError, base, 9); got != time.Hour {
		t.Fatalf("持续失败应退避到 1 小时，实际 %v", got)
	}
}

func TestNextIntervalSkippedIsLowFrequency(t *testing.T) {
	if got := NextInterval(domain.StatusSkipped, time.Minute, 0); got != 24*time.Hour {
		t.Fatalf("skipped 应低频重试，实际 %v", got)
	}
}

func TestTrimRawRespectsRetentionMode(t *testing.T) {
	raw := "0123456789"

	if got := trimRaw(raw, repository.Retention{RawMode: repository.RawModeNever}, true); got != "" {
		t.Fatalf("never 模式不应保存原始报文，实际 %q", got)
	}
	if got := trimRaw(raw, repository.Retention{RawMode: repository.RawModeChangeOnly}, false); got != "" {
		t.Fatalf("change_only 模式在状态未变化时不应保存，实际 %q", got)
	}
	if got := trimRaw(raw, repository.Retention{RawMode: repository.RawModeChangeOnly, RawMaxBytes: 100}, true); got != raw {
		t.Fatalf("状态变化时应保存原始报文，实际 %q", got)
	}
	got := trimRaw(raw, repository.Retention{RawMode: repository.RawModeAlways, RawMaxBytes: 4}, false)
	if got != "0123\n...(已截断)" {
		t.Fatalf("超长报文应被截断，实际 %q", got)
	}
}

func TestValidateName(t *testing.T) {
	valid := []string{"example.com", "a-b.example.co.uk", "xn--fiqs8s.cn"}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Fatalf("%s 应当合法: %v", name, err)
		}
	}

	invalid := []string{"", "nodot", "-bad.com", "bad-.com", "example.123", "a b.com"}
	for _, name := range invalid {
		if err := ValidateName(name); err == nil {
			t.Fatalf("%q 应当被判为非法", name)
		}
	}
}

func TestSimplifyError(t *testing.T) {
	if got := simplifyError("WHOIS连接失败: connection refused"); got != "connection refused" {
		t.Fatalf("错误前缀未去除: %q", got)
	}
	if got := simplifyError("RDAP查询失败: timeout"); got != "timeout" {
		t.Fatalf("错误前缀未去除: %q", got)
	}
}

func TestSuppressesLegacyTransferLockNotification(t *testing.T) {
	if !suppressLegacyTransferNotification(domain.StatusTransferLocked, domain.StatusRegistered) {
		t.Fatal("旧 transfer_locked 回归 registered 应抑制通知")
	}
	if suppressLegacyTransferNotification(domain.StatusRegistered, domain.StatusAvailable) {
		t.Fatal("普通状态变化不应被抑制")
	}
}

type notificationSuppressionRepo struct {
	repository.DomainRepository
	lastReads int
}

func (r *notificationSuppressionRepo) LastNotifiedStatus(context.Context, string) (string, error) {
	r.lastReads++
	return "", nil
}

func (r *notificationSuppressionRepo) SetLastNotifiedStatus(context.Context, string, string) error {
	return nil
}

func TestMaybeNotifySuppressesLegacyTransferLockTransition(t *testing.T) {
	repo := &notificationSuppressionRepo{}
	service := &QueryService{
		domains:  repo,
		notifier: notification.NewManager(nil),
	}
	record := &domain.Domain{Name: "locked.example", Notify: true}
	outcome := query.Outcome{
		Info: &domain.Info{Name: record.Name, Status: domain.StatusRegistered},
		Winner: query.Result{
			Domain: record.Name, Status: domain.StatusRegistered,
			Confidence: domain.ConfidenceHigh,
		},
	}

	service.maybeNotify(context.Background(), record, domain.StatusTransferLocked, outcome)
	if repo.lastReads != 0 {
		t.Fatal("旧 transfer_locked 恢复 registered 时应在去重查询前抑制通知")
	}
}
