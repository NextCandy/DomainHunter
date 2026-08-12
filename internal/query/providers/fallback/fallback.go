// Package fallback 实现本地 whois-domain-lookup 结构化备用查询源。
//
// 它只对显式启用的后缀生效（部署上是 .im 与 .do），这样某次服务故障不会影响
// 其他域名的判定。安全要点：
//   - 只有 registered / reserved / unknown 三个标志全部明确给出且都为 false
//     时，才允许判定可注册；缺字段一律不可注册
//   - .do 响应会给每个隐私字段追加 "| Registry Policy"，这只是隐私标记，
//     明确的 registered=true 优先于该文本，不能被误判为保留域名
package fallback

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/httpx"
	"DomainHunter/internal/query"
	"DomainHunter/internal/query/detect"
	"DomainHunter/internal/query/envcfg"
	"DomainHunter/internal/query/providers/whois"
)

const maxBodyBytes = 1 << 20

type apiResponse struct {
	Code int      `json:"code"`
	Msg  string   `json:"msg"`
	Data *apiData `json:"data"`
}

type apiData struct {
	WhoisData      string            `json:"whoisData"`
	RDAPData       string            `json:"rdapData"`
	Unknown        *bool             `json:"unknown"`
	Registered     *bool             `json:"registered"`
	Reserved       *bool             `json:"reserved"`
	Domain         string            `json:"domain"`
	Registrar      string            `json:"registrar"`
	CreationDate   string            `json:"creationDate"`
	ExpirationDate string            `json:"expirationDate"`
	UpdatedDate    string            `json:"updatedDate"`
	NameServers    []string          `json:"nameServers"`
	Status         []json.RawMessage `json:"status"`
}

// Provider 本地备用 WHOIS 服务查询源
type Provider struct {
	mu      sync.RWMutex
	name    string
	baseURL string
	tlds    map[string]struct{}
	timeout time.Duration
}

// New 从环境变量创建 Provider。
// DOMAINHUNTER_WHOIS_FALLBACK_URL 默认为空；旧的 PUFF_WHOIS_FALLBACK_* 同样有效。
func New(timeout time.Duration) *Provider {
	return NewNamed(query.ProviderFallback, timeout)
}

// NewNamed creates the same adapter with a deployment-specific evidence name.
// New keeps the historical "fallback" name for compatibility with existing
// policy files and tests.
func NewNamed(name string, timeout time.Duration) *Provider {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	if configured := envcfg.First("DOMAINHUNTER_WHOIS_FALLBACK_TIMEOUT", "PUFF_WHOIS_FALLBACK_TIMEOUT"); configured != "" {
		if parsed, ok := envcfg.ParseDuration(configured); ok {
			timeout = parsed
		}
	}
	tlds := envcfg.ParseTLDs(envcfg.First("DOMAINHUNTER_WHOIS_FALLBACK_TLDS", "PUFF_WHOIS_FALLBACK_TLDS"), "im", "do")
	if name == query.ProviderWhoisDomainLookup {
		// The standalone whois-domain-lookup container is the second product-wide
		// fallback. Its API can query more TLDs than the legacy .im/.do opt-in.
		tlds = nil
	}
	return &Provider{
		name:    strings.TrimSpace(name),
		baseURL: strings.TrimSpace(envcfg.First("DOMAINHUNTER_WHOIS_FALLBACK_URL", "PUFF_WHOIS_FALLBACK_URL")),
		tlds:    tlds,
		timeout: timeout,
	}
}

// Name 实现 query.Provider
func (p *Provider) Name() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.name != "" {
		return p.name
	}
	return query.ProviderFallback
}

// UpdateTimeout 实现 query.TimeoutAware
func (p *Provider) UpdateTimeout(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	p.mu.Lock()
	p.timeout = timeout
	p.mu.Unlock()
}

// Supports 只有配置了 URL 且该后缀被显式启用时才参与查询
func (p *Provider) Supports(_ context.Context, req query.Request) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.baseURL == "" {
		return false
	}
	if len(p.tlds) == 0 {
		return true
	}
	_, ok := p.tlds[strings.ToLower(strings.Trim(strings.TrimSpace(req.TLD), "."))]
	return ok
}

// Configured 返回是否配置了服务地址
func (p *Provider) Configured() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.baseURL != ""
}

func (p *Provider) snapshot() (string, time.Duration) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.baseURL, p.timeout
}

// Query 执行一次备用服务查询
func (p *Provider) Query(ctx context.Context, req query.Request) query.Result {
	started := time.Now()

	data, err := p.fetch(ctx, req.Domain)
	if err != nil {
		finished := time.Now()
		return query.Result{
			Domain:     req.Domain,
			Status:     domain.StatusError,
			Provider:   p.Name(),
			Confidence: domain.ConfidenceLow,
			Err:        query.WrapError(p.Name(), err),
			StartedAt:  started,
			FinishedAt: finished,
			Latency:    finished.Sub(started),
		}
	}

	raw := strings.TrimSpace(data.WhoisData)
	if raw == "" {
		raw = strings.TrimSpace(data.RDAPData)
	}

	result := query.Result{
		Domain:      req.Domain,
		Registrar:   data.Registrar,
		CreatedAt:   whois.ParseDateTime(data.CreationDate),
		ExpiryAt:    whois.ParseDateTime(data.ExpirationDate),
		UpdatedAt:   whois.ParseDateTime(data.UpdatedDate),
		NameServers: append([]string(nil), data.NameServers...),
		EPPStatuses: decodeStatuses(data.Status),
		Provider:    p.Name(),
		Raw:         raw,
		Confidence:  domain.ConfidenceMedium,
		StartedAt:   started,
	}
	defer func() {
		result.FinishedAt = time.Now()
		result.Latency = result.FinishedAt.Sub(started)
	}()

	// 隐私标记不是保留域名结论：只有在没有明确 registered=true 时才据文本判定保留。
	if data.Reserved == nil || !*data.Reserved {
		if detect.ContainsReserved(raw) && (data.Registered == nil || !*data.Registered) {
			result.Status = domain.StatusUnknown
			result.Confidence = domain.ConfidenceLow
			result.Note = "备用 WHOIS 返回保留或禁止注册信息"
			return result
		}
	}

	switch {
	case data.Registered != nil && *data.Registered:
		// registered=true 只说明域名有注册记录，不能覆盖 EPP 状态中的
		// redemption/pending-delete 等生命周期状态。whois-domain-lookup
		// 的结构化响应会同时返回这两类字段；优先保留更具体的状态，避免
		// who-dat 暂时超时时把真实的待删除/赎回状态误降成 registered。
		result.Status = statusFromFallback(decodeStatuses(data.Status))
		result.Confidence = domain.ConfidenceHigh

	case data.Reserved != nil && *data.Reserved:
		result.Status = domain.StatusUnknown
		result.Confidence = domain.ConfidenceLow
		result.Note = "备用 WHOIS 返回保留或禁止注册状态"

	case data.Unknown != nil && *data.Unknown:
		result.Status = domain.StatusUnknown
		result.Confidence = domain.ConfidenceLow
		result.Note = "备用 WHOIS 未能确定域名状态"

	case data.Registered != nil && !*data.Registered &&
		data.Reserved != nil && !*data.Reserved &&
		data.Unknown != nil && !*data.Unknown:
		// 三个标志全部明确且为 false，才允许判定可注册。
		result.Status = domain.StatusAvailable
		result.Confidence = domain.ConfidenceHigh

	default:
		result.Status = domain.StatusUnknown
		result.Confidence = domain.ConfidenceLow
		result.Note = "备用 WHOIS 未提供完整域名状态"
		if raw != "" {
			parsed := whois.Parse(req.Domain, raw)
			parsed.Provider = p.Name()
			parsed.Raw = raw
			parsed.StartedAt = started
			// 原始文本可以帮助识别已注册/未知，但不能绕过结构化状态
			// 完整性要求重新得到 available。
			if parsed.Status == domain.StatusAvailable {
				parsed.Status = domain.StatusUnknown
				parsed.Confidence = domain.ConfidenceLow
				parsed.Note = "备用 WHOIS 状态字段不完整，无法确认可注册"
			}
			result = parsed
		}
	}
	return result
}

func (p *Provider) fetch(ctx context.Context, name string) (*apiData, error) {
	baseURL, timeout := p.snapshot()
	endpoint, err := Endpoint(baseURL, name)
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("创建备用 WHOIS 请求失败: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")

	resp, err := httpx.Client(timeout).Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("备用 WHOIS 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("读取备用 WHOIS 响应失败: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, query.NewError(query.KindUnavailable, query.ProviderFallback,
			"备用 WHOIS 返回 HTTP %d", resp.StatusCode)
	}

	var parsed apiResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, query.NewError(query.KindParse, query.ProviderFallback, "解析备用 WHOIS 响应失败: %v", err)
	}
	if parsed.Code != 0 {
		msg := parsed.Msg
		if msg == "" {
			msg = "备用 WHOIS 查询失败"
		}
		return nil, query.NewError(query.KindUnavailable, query.ProviderFallback, "%s", msg)
	}
	if parsed.Data == nil {
		return nil, query.NewError(query.KindUnknownResponse, query.ProviderFallback, "备用 WHOIS 响应缺少 data")
	}
	return parsed.Data, nil
}

// Endpoint 拼接备用服务的查询地址
func Endpoint(rawBaseURL, name string) (string, error) {
	if strings.TrimSpace(rawBaseURL) == "" {
		return "", query.NewError(query.KindNotSupported, query.ProviderFallback, "备用 WHOIS URL 未配置")
	}
	parsed, err := url.Parse(rawBaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", query.NewError(query.KindNotSupported, query.ProviderFallback, "备用 WHOIS URL 无效")
	}

	path := strings.TrimRight(parsed.Path, "/")
	switch {
	case path == "" || path == "/":
		parsed.Path = "/api/"
	case strings.HasSuffix(path, "/api"):
		parsed.Path = path + "/"
	default:
		parsed.Path = path + "/api/"
	}

	q := parsed.Query()
	q.Set("domain", name)
	q.Set("whois", "1")
	q.Set("rdap", "1")
	parsed.RawQuery = q.Encode()
	return parsed.String(), nil
}

func decodeStatuses(raw []json.RawMessage) []string {
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		var s string
		if err := json.Unmarshal(item, &s); err == nil {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal(item, &obj); err == nil {
			for _, key := range []string{"status", "name", "value"} {
				if v, ok := obj[key].(string); ok && strings.TrimSpace(v) != "" {
					out = append(out, strings.TrimSpace(v))
					break
				}
			}
		}
	}
	return out
}

// statusFromFallback maps EPP status values returned by whois-domain-lookup
// to the same lifecycle states used by RDAP and who-dat. A transfer lock by
// itself remains an ordinary registered state.
func statusFromFallback(statuses []string) domain.Status {
	for _, raw := range statuses {
		key := strings.ToLower(strings.NewReplacer(" ", "", "_", "", "-", "").Replace(raw))
		switch {
		case strings.Contains(key, "hold"):
			return domain.StatusHold
		case strings.Contains(key, "redemption"):
			return domain.StatusRedemption
		case strings.Contains(key, "pendingdelete"):
			return domain.StatusPendingDelete
		case strings.Contains(key, "expired"), strings.Contains(key, "renewperiod"):
			return domain.StatusGrace
		}
	}
	return domain.StatusRegistered
}
