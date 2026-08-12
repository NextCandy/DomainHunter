package query

import (
	"errors"
	"testing"
	"time"
)

func TestHealthTrackerSnapshotIncludesCircuitBreakerSignals(t *testing.T) {
	tracker := NewHealthTracker()
	for i := 0; i < 4; i++ {
		tracker.Record(Result{Provider: ProviderRDAP, Err: errors.New("timeout"), Latency: 120 * time.Millisecond})
	}

	snapshots := tracker.Snapshot([]string{ProviderRDAP})
	if len(snapshots) != 1 {
		t.Fatalf("expected one provider snapshot, got %d", len(snapshots))
	}
	snapshot := snapshots[0]
	if snapshot.State != HealthOffline || snapshot.ErrorRate != 1 || snapshot.ConsecutiveFailures != 4 {
		t.Fatalf("unexpected circuit breaker snapshot: %#v", snapshot)
	}
	if snapshot.P95Latency != 120 || snapshot.StateReason == "" || snapshot.AsOf.IsZero() {
		t.Fatalf("snapshot should expose latency and reason metadata: %#v", snapshot)
	}
	if tracker.HealthyForPlan(ProviderRDAP) {
		t.Fatal("offline provider should be removed from the next query plan")
	}
}

func TestHealthTrackerKeepsProviderWhenItHasRecentSuccess(t *testing.T) {
	tracker := NewHealthTracker()
	tracker.Record(Result{Provider: ProviderRDAP, Err: errors.New("timeout"), Latency: 100 * time.Millisecond})
	tracker.Record(Result{Provider: ProviderRDAP, Latency: 80 * time.Millisecond})

	if !tracker.HealthyForPlan(ProviderRDAP) {
		t.Fatal("a provider with a recent successful result should remain eligible")
	}
	snapshot := tracker.Snapshot([]string{ProviderRDAP})[0]
	if snapshot.State != HealthDegraded || snapshot.ConsecutiveFailures != 0 {
		t.Fatalf("expected degraded state without consecutive failures: %#v", snapshot)
	}
}
