package domain

import "time"

// ReviewReason 是独立于生命周期状态的数据质量复核原因。
// 它不能改变 Status，也不能被当成“新的注册状态”。
type ReviewReason string

const (
	ReviewQueryError             ReviewReason = "query_error"
	ReviewUnknownStatus          ReviewReason = "unknown_status"
	ReviewLowConfidence          ReviewReason = "low_confidence"
	ReviewDropStatusFutureExpiry ReviewReason = "drop_status_future_expiry"
	ReviewProviderConflict       ReviewReason = "provider_conflict"
	ReviewStaleEvidence          ReviewReason = "stale_evidence"
)

// ReviewState 是返回给列表和详情页的后端裁决；浏览器不得自行根据日期计算。
type ReviewState struct {
	Required    bool           `json:"required"`
	Reasons     []ReviewReason `json:"reasons"`
	Severity    string         `json:"severity,omitempty"`
	Explanation string         `json:"explanation,omitempty"`
}

// FutureExpiryTolerance 是掉落状态与未来到期日期之间允许的最小时间窗口。
// 集中定义，避免 handler 与前端各自使用魔法数字。
const FutureExpiryTolerance = 48 * time.Hour

// StaleEvidenceThreshold prevents an old positive result from being treated as
// a fresh opportunity. Zero LastChecked is handled by the unknown-status rule.
const StaleEvidenceThreshold = 7 * 24 * time.Hour

func IsDropStatus(status Status) bool {
	switch status {
	case StatusPendingDelete, StatusRedemption, StatusExpired, StatusGrace:
		return true
	default:
		return false
	}
}

// BuildReviewState 只读取当前快照和已返回的证据，不改变生命周期状态。
func BuildReviewState(info *Info, now time.Time) *ReviewState {
	if info == nil {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	reasons := make([]ReviewReason, 0, 3)
	add := func(reason ReviewReason) {
		for _, existing := range reasons {
			if existing == reason {
				return
			}
		}
		reasons = append(reasons, reason)
	}
	if info.Status == StatusError {
		add(ReviewQueryError)
	}
	if info.Status == StatusUnknown || info.Status == StatusSkipped {
		add(ReviewUnknownStatus)
	}
	if info.Confidence == "" || info.Confidence == ConfidenceLow {
		add(ReviewLowConfidence)
	}
	if IsDropStatus(info.Status) && info.ExpiryDate != nil && info.ExpiryDate.After(now.Add(FutureExpiryTolerance)) {
		add(ReviewDropStatusFutureExpiry)
	}
	if !info.LastChecked.IsZero() && info.LastChecked.Before(now) && now.Sub(info.LastChecked) > StaleEvidenceThreshold {
		add(ReviewStaleEvidence)
	}
	seen := map[Status]bool{}
	for _, evidence := range info.Evidence {
		if evidence.Status == "" {
			continue
		}
		seen[evidence.Status] = true
	}
	if len(seen) > 1 && seen[StatusAvailable] && seen[StatusRegistered] {
		add(ReviewProviderConflict)
	}
	state := &ReviewState{Required: len(reasons) > 0, Reasons: reasons}
	if len(reasons) == 0 {
		return state
	}
	state.Severity = "warning"
	if info.Status == StatusError || info.Status == StatusUnknown {
		state.Severity = "critical"
	}
	state.Explanation = "查询事实或证据需要复核；这不是新的域名生命周期状态。"
	return state
}

// Confidence 表示结论的可信度。
//
// 它不是"域名有多可能可注册"，而是"这个结论有多少证据支撑"：
//   - High   多个查询源互相印证，或单个查询源返回了结构化的权威数据
//   - Medium 单一查询源给出明确结论，但没有交叉验证
//   - Low    只能从文本启发式推断，或结论本身就是 unknown/error
type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

// Rank 返回可信度的可比较权重
func (c Confidence) Rank() int {
	switch c {
	case ConfidenceHigh:
		return 3
	case ConfidenceMedium:
		return 2
	case ConfidenceLow:
		return 1
	default:
		return 0
	}
}

// Evidence 单个查询源对本次查询给出的证据。
//
// 这是"为什么当前状态是这个结果"的可解释性来源，会随 API 一起返回，
// 并按保留策略写入 query_attempts 表。
type Evidence struct {
	Provider   string        `json:"provider"`
	Status     Status        `json:"status"`
	Confidence Confidence    `json:"confidence"`
	Latency    time.Duration `json:"-"`
	LatencyMS  int64         `json:"latency_ms"`
	Error      string        `json:"error,omitempty"`
	Note       string        `json:"note,omitempty"`
	QueriedAt  time.Time     `json:"queried_at"`
}
