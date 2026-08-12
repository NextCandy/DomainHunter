package domain

import (
	"testing"
	"time"
)

func TestBuildReviewStateFutureExpiryUsesTolerance(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	within := now.Add(FutureExpiryTolerance)
	info := &Info{Status: StatusPendingDelete, Confidence: ConfidenceHigh, ExpiryDate: &within}
	state := BuildReviewState(info, now)
	if state.Required {
		t.Fatalf("expiry at tolerance boundary should not require review: %#v", state)
	}

	beyond := now.Add(FutureExpiryTolerance + time.Minute)
	info.ExpiryDate = &beyond
	state = BuildReviewState(info, now)
	if !state.Required || !containsReviewReason(state.Reasons, ReviewDropStatusFutureExpiry) {
		t.Fatalf("future expiry should require review: %#v", state)
	}
}

func TestBuildReviewStateDetectsQualityProblemsWithoutChangingStatus(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	checked := now.Add(-StaleEvidenceThreshold - time.Minute)
	info := &Info{
		Status:      StatusAvailable,
		Confidence:  ConfidenceLow,
		LastChecked: checked,
		Evidence: []Evidence{
			{Provider: "rdap", Status: StatusAvailable},
			{Provider: "whois", Status: StatusRegistered},
		},
	}
	state := BuildReviewState(info, now)
	if info.Status != StatusAvailable {
		t.Fatalf("review calculation changed lifecycle status: %s", info.Status)
	}
	for _, reason := range []ReviewReason{ReviewLowConfidence, ReviewProviderConflict, ReviewStaleEvidence} {
		if !containsReviewReason(state.Reasons, reason) {
			t.Fatalf("missing review reason %q: %#v", reason, state)
		}
	}
}

func TestBuildReviewStateUnknownIsCritical(t *testing.T) {
	state := BuildReviewState(&Info{Status: StatusUnknown}, time.Now())
	if !state.Required || state.Severity != "critical" || !containsReviewReason(state.Reasons, ReviewUnknownStatus) {
		t.Fatalf("unknown status should be critical review: %#v", state)
	}
}

func containsReviewReason(reasons []ReviewReason, wanted ReviewReason) bool {
	for _, reason := range reasons {
		if reason == wanted {
			return true
		}
	}
	return false
}
