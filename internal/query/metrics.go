package query

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

var metricBucketBounds = [...]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// Metrics 保存查询侧的轻量 Prometheus 指标，不引入额外运行时依赖。
type Metrics struct {
	mu        sync.RWMutex
	providers map[string]*providerMetrics
	count     atomic.Uint64
	seconds   atomic.Uint64
}

type providerMetrics struct {
	requests atomic.Uint64
	errors   atomic.Uint64
	// duration is stored as nanoseconds to retain sub-second precision.
	duration atomic.Uint64
	buckets  [len(metricBucketBounds)]atomic.Uint64
}

// NewMetrics 创建指标采集器。
func NewMetrics() *Metrics { return &Metrics{providers: make(map[string]*providerMetrics)} }

// Observe 记录一次 Provider 查询。
func (m *Metrics) Observe(provider string, duration time.Duration, failed bool) {
	if m == nil {
		return
	}
	m.mu.RLock()
	pm := m.providers[provider]
	m.mu.RUnlock()
	if pm == nil {
		m.mu.Lock()
		pm = m.providers[provider]
		if pm == nil {
			pm = &providerMetrics{}
			m.providers[provider] = pm
		}
		m.mu.Unlock()
	}
	pm.requests.Add(1)
	pm.duration.Add(uint64(duration))
	seconds := duration.Seconds()
	for i, bound := range metricBucketBounds {
		if seconds <= bound {
			pm.buckets[i].Add(1)
		}
	}
	if failed {
		pm.errors.Add(1)
	}
	m.count.Add(1)
	m.seconds.Add(uint64(duration))
}

// ProviderSnapshot 返回稳定排序的指标快照。
func (m *Metrics) ProviderSnapshot() map[string]MetricSnapshot {
	out := make(map[string]MetricSnapshot)
	if m == nil {
		return out
	}
	m.mu.RLock()
	keys := make([]string, 0, len(m.providers))
	for key := range m.providers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		pm := m.providers[key]
		snapshot := MetricSnapshot{
			Requests: pm.requests.Load(), Errors: pm.errors.Load(),
			DurationSeconds: float64(pm.duration.Load()) / float64(time.Second),
		}
		for i := range metricBucketBounds {
			snapshot.Buckets[i] = pm.buckets[i].Load()
		}
		out[key] = snapshot
	}
	m.mu.RUnlock()
	return out
}

// MetricSnapshot 是单个 Provider 的查询指标。
type MetricSnapshot struct {
	Requests        uint64
	Errors          uint64
	DurationSeconds float64
	Buckets         [len(metricBucketBounds)]uint64
}

// DurationBucketBounds 返回 histogram bucket 上界。
func DurationBucketBounds() []float64 {
	return append([]float64(nil), metricBucketBounds[:]...)
}

// Snapshot 返回总查询数与累计耗时。
func (m *Metrics) Snapshot() (uint64, float64) {
	if m == nil {
		return 0, 0
	}
	return m.count.Load(), float64(m.seconds.Load()) / float64(time.Second)
}
