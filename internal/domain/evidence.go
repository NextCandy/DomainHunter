package domain

import "time"

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
