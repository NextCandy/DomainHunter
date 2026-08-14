package query

import (
	"testing"
	"time"

	"DomainHunter/internal/domain"
)

func TestNormalizeIMLifecycleDoesNotInferGraceFromExpiryOnly(t *testing.T) {
	expiry := time.Now().Add(-24 * time.Hour)
	result := NormalizeIMLifecycle(Result{
		Domain:   "thank.im",
		Status:   domain.StatusGrace,
		ExpiryAt: &expiry,
	})

	if result.Status != domain.StatusRegistered {
		t.Fatalf(".im 只有到期日时不应推断为宽限期，实际 %s", result.Status)
	}
	if result.CreatedAt != nil {
		t.Fatal("不得伪造 .im 注册日期")
	}
	if result.Confidence != domain.ConfidenceMedium {
		t.Fatalf("不完整生命周期证据应为 medium，实际 %s", result.Confidence)
	}
}

func TestNormalizeIMLifecycleKeepsExplicitStage(t *testing.T) {
	expiry := time.Now().Add(-24 * time.Hour)
	result := NormalizeIMLifecycle(Result{
		Domain:            "thank.im",
		Status:            domain.StatusGrace,
		ExpiryAt:          &expiry,
		LifecycleEvidence: true,
	})

	if result.Status != domain.StatusGrace {
		t.Fatalf(".im 有明确生命周期证据时应保留宽限期，实际 %s", result.Status)
	}
}

func TestNormalizeIMLifecycleDoesNotAffectOtherTLDs(t *testing.T) {
	expiry := time.Now().Add(-24 * time.Hour)
	result := NormalizeIMLifecycle(Result{
		Domain:   "thank.com",
		Status:   domain.StatusGrace,
		ExpiryAt: &expiry,
	})

	if result.Status != domain.StatusGrace {
		t.Fatalf("非 .im 后缀不应改变生命周期状态，实际 %s", result.Status)
	}
}
