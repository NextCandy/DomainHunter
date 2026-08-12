package query

import (
	"sort"
	"sync"
	"time"
)

// HealthState Provider 健康状态
type HealthState string

const (
	HealthUnknown  HealthState = "unknown"
	HealthHealthy  HealthState = "healthy"
	HealthDegraded HealthState = "degraded"
	HealthOffline  HealthState = "offline"
)

// ProviderHealth 单个 Provider 的健康快照
type ProviderHealth struct {
	Provider            string      `json:"provider"`
	State               HealthState `json:"state"`
	AsOf                time.Time   `json:"as_of"`
	Requests            int         `json:"requests"`
	Errors              int         `json:"errors"`
	ErrorRate           float64     `json:"error_rate"`
	AvgLatency          int64       `json:"avg_latency_ms"`
	P50Latency          int64       `json:"p50_latency_ms"`
	P95Latency          int64       `json:"p95_latency_ms"`
	LastError           string      `json:"last_error,omitempty"`
	LastSuccess         *time.Time  `json:"last_success,omitempty"`
	LastFailure         *time.Time  `json:"last_failure,omitempty"`
	ConsecutiveFailures int         `json:"consecutive_failures"`
	StateReason         string      `json:"state_reason,omitempty"`
}

func percentile(values []int64, fraction float64) int64 {
	if len(values) == 0 {
		return 0
	}
	index := int(float64(len(values)-1) * fraction)
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

type healthSample struct {
	at      time.Time
	ok      bool
	latency time.Duration
}

// HealthTracker 用滑动窗口统计每个 Provider 的成功率与延迟。
//
// 只保留最近 window 时间内的样本，单 Provider 最多保留 maxSamples 条，
// 树莓派长期运行也不会累积内存。
type HealthTracker struct {
	mu         sync.Mutex
	window     time.Duration
	maxSamples int
	samples    map[string][]healthSample
	lastErr    map[string]string
	lastOK     map[string]time.Time
	lastFail   map[string]time.Time
}

// NewHealthTracker 创建健康统计器
func NewHealthTracker() *HealthTracker {
	return &HealthTracker{
		window:     30 * time.Minute,
		maxSamples: 100,
		samples:    map[string][]healthSample{},
		lastErr:    map[string]string{},
		lastOK:     map[string]time.Time{},
		lastFail:   map[string]time.Time{},
	}
}

// Record 记录一次查询结果
func (h *HealthTracker) Record(res Result) {
	if h == nil || res.Provider == "" {
		return
	}
	// skipped 表示"该源不适用"，不算成功也不算失败
	if res.Err == nil && res.Status == "skipped" {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()
	ok := res.Err == nil
	list := append(h.samples[res.Provider], healthSample{at: now, ok: ok, latency: res.Latency})
	if len(list) > h.maxSamples {
		list = list[len(list)-h.maxSamples:]
	}
	h.samples[res.Provider] = list

	if ok {
		h.lastOK[res.Provider] = now
		delete(h.lastErr, res.Provider)
	} else {
		h.lastFail[res.Provider] = now
		h.lastErr[res.Provider] = res.Err.Error()
	}
}

// HealthyForPlan lets the policy layer avoid repeatedly preferring a provider
// that is currently offline while retaining the existing evidence rules.
func (h *HealthTracker) HealthyForPlan(name string) bool {
	if h == nil {
		return true
	}
	for _, snapshot := range h.Snapshot([]string{name}) {
		return snapshot.State != HealthOffline
	}
	return true
}

// Snapshot 返回所有已知 Provider 的健康状态
func (h *HealthTracker) Snapshot(names []string) []ProviderHealth {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-h.window)
	out := make([]ProviderHealth, 0, len(names))
	for _, name := range names {
		ph := ProviderHealth{Provider: name, State: HealthUnknown, AsOf: now}

		kept := make([]healthSample, 0, len(h.samples[name]))
		var total time.Duration
		for _, s := range h.samples[name] {
			if s.at.Before(cutoff) {
				continue
			}
			kept = append(kept, s)
			total += s.latency
			ph.Requests++
			if !s.ok {
				ph.Errors++
			}
		}
		h.samples[name] = kept

		if ph.Requests > 0 {
			ph.AvgLatency = (total / time.Duration(ph.Requests)).Milliseconds()
			ph.ErrorRate = float64(ph.Errors) / float64(ph.Requests)
			latencies := make([]int64, 0, len(kept))
			for _, sample := range kept {
				latencies = append(latencies, sample.latency.Milliseconds())
			}
			sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
			ph.P50Latency = percentile(latencies, 0.50)
			ph.P95Latency = percentile(latencies, 0.95)
			switch ratio := float64(ph.Errors) / float64(ph.Requests); {
			case ratio == 0:
				ph.State = HealthHealthy
				ph.StateReason = "最近窗口内无错误"
			case ratio >= 0.8:
				ph.State = HealthOffline
				ph.StateReason = "最近窗口错误率达到 80%"
			default:
				ph.State = HealthDegraded
				ph.StateReason = "最近窗口存在查询错误"
			}
		}
		for i := len(kept) - 1; i >= 0; i-- {
			if kept[i].ok {
				break
			}
			ph.ConsecutiveFailures++
		}
		if msg, ok := h.lastErr[name]; ok {
			ph.LastError = msg
		}
		if t, ok := h.lastOK[name]; ok {
			tt := t
			ph.LastSuccess = &tt
		}
		if t, ok := h.lastFail[name]; ok {
			tt := t
			ph.LastFailure = &tt
		}
		out = append(out, ph)
	}
	return out
}
