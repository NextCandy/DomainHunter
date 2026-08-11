package query

import (
	"time"

	"DomainHunter/internal/domain"
)

// Request 一次域名查询请求
type Request struct {
	Domain string // 已归一化的域名
	TLD    string // registry.FindBestTLD 匹配到的最长后缀
}

// Result 所有 Provider 统一返回的结果结构。
//
// RDAP、WHOIS、WHOIS.LS 和备用服务不再各自返回不同风格的数据：它们都产出
// Result，由 Engine 按 Policy 合成最终结论。
type Result struct {
	Domain string
	Status domain.Status

	Registrar string

	CreatedAt *time.Time
	UpdatedAt *time.Time
	ExpiryAt  *time.Time

	NameServers []string
	EPPStatuses []string

	Provider   string
	Confidence domain.Confidence
	Raw        string
	Note       string

	Err error

	StartedAt  time.Time
	FinishedAt time.Time
	Latency    time.Duration
}

// Definitive 判断该结果是否是可直接采用的明确结论
func (r Result) Definitive() bool {
	return r.Err == nil && domain.IsDefinitive(r.Status)
}

// HasRegistrationEvidence 判断结果中是否存在"已被注册"的实证
func (r Result) HasRegistrationEvidence() bool {
	info := r.ToInfo()
	return info.HasRegistrationEvidence()
}

// ToInfo 把 Result 转换为对外的状态快照
func (r Result) ToInfo() *domain.Info {
	info := &domain.Info{
		Name:        r.Domain,
		Status:      r.Status,
		Registrar:   r.Registrar,
		CreatedDate: r.CreatedAt,
		ExpiryDate:  r.ExpiryAt,
		UpdatedDate: r.UpdatedAt,
		NameServers: r.NameServers,
		EPPStatuses: r.EPPStatuses,
		QueryMethod: r.Provider,
		WhoisRaw:    r.Raw,
		Confidence:  r.Confidence,
		LastChecked: r.FinishedAt,
	}
	if info.LastChecked.IsZero() {
		info.LastChecked = time.Now()
	}
	if r.Err != nil {
		info.ErrorMessage = r.Err.Error()
	} else if r.Note != "" && !domain.IsDefinitive(r.Status) {
		info.ErrorMessage = r.Note
	}
	return info
}

// Evidence 把 Result 折叠成一条查询证据
func (r Result) Evidence() domain.Evidence {
	ev := domain.Evidence{
		Provider:   r.Provider,
		Status:     r.Status,
		Confidence: r.Confidence,
		Latency:    r.Latency,
		LatencyMS:  r.Latency.Milliseconds(),
		Note:       r.Note,
		QueriedAt:  r.StartedAt,
	}
	if ev.QueriedAt.IsZero() {
		ev.QueriedAt = time.Now()
	}
	if r.Err != nil {
		ev.Error = r.Err.Error()
		if ev.Status == "" {
			ev.Status = domain.StatusError
		}
	}
	if ev.Confidence == "" {
		ev.Confidence = domain.ConfidenceLow
	}
	return ev
}

// errorResult 构造一个错误结果
func errorResult(provider, name string, started time.Time, err error) Result {
	finished := time.Now()
	return Result{
		Domain:     name,
		Status:     domain.StatusError,
		Provider:   provider,
		Confidence: domain.ConfidenceLow,
		Err:        WrapError(provider, err),
		StartedAt:  started,
		FinishedAt: finished,
		Latency:    finished.Sub(started),
	}
}
