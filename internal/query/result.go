package query

import (
	"strings"
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

	// LifecycleEvidence 表示查询源是否返回了明确的生命周期字段（例如
	// renewPeriod、redemptionPeriod 或 pendingDelete）。.im 的公开 WHOIS
	// 只提供到期日时，不能把“到期日已过”当成宽限期证据。
	LifecycleEvidence bool

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

// NormalizeIMLifecycle 将缺少明确生命周期字段的 .im 阶段结果稳定为
// registered。官方 .im 公共 WHOIS 不公开注册日期，部分响应只包含到期日；
// 如果仅凭这个日期推断 grace/redemption/pending-delete，会在查询源之间产生
// “已注册 ↔ 宽限期”的来回跳变，并重复触发提醒。
//
// 这里不伪造注册日期，也不改变明确返回的生命周期状态；只有“有到期日、无
// 创建日、无明确生命周期字段”的不完整证据才会被保守处理。
func NormalizeIMLifecycle(result Result) Result {
	if !isIMDomain(result.Domain) || result.CreatedAt != nil || result.ExpiryAt == nil || result.LifecycleEvidence {
		return result
	}

	switch result.Status {
	case domain.StatusGrace, domain.StatusRedemption, domain.StatusPendingDelete, domain.StatusExpired:
		result.Status = domain.StatusRegistered
		result.Confidence = domain.ConfidenceMedium
		result.Note = ".im 官方公开 WHOIS 未提供注册日期或明确生命周期字段，按已注册处理"
	}
	return result
}

func isIMDomain(name string) bool {
	name = domain.Normalize(name)
	return strings.HasSuffix(name, ".im")
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
