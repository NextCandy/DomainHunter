// Package whodat adapts the normalized JSON response from lissy93/who-dat.
//
// who-dat is used twice in the DomainHunter chain: the authenticated instance
// running on the Raspberry Pi, and the user's Vercel deployment at rdap.re.
// Both expose the same /v1/whois/{domain} contract, so the provider is kept
// configurable while the source identity remains visible in evidence.
package whodat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/httpx"
	"DomainHunter/internal/query"
	"DomainHunter/internal/query/detect"
)

const maxBodyBytes = 4 << 20

// Provider queries a who-dat-compatible HTTP endpoint.
type Provider struct {
	mu      sync.RWMutex
	name    string
	baseURL string
	apiKey  string
	timeout time.Duration
}

type response struct {
	Query        string       `json:"query"`
	Domain       string       `json:"domain"`
	IsRegistered *bool        `json:"isRegistered"`
	Registrar    registrar    `json:"registrar"`
	Status       []string     `json:"status"`
	Nameservers  []nameserver `json:"nameservers"`
	Dates        dates        `json:"dates"`
	Meta         responseMeta `json:"meta"`
}

type registrar struct {
	Name *string `json:"name"`
}

type nameserver struct {
	Name string `json:"name"`
}

type dates struct {
	Created *time.Time `json:"created"`
	Updated *time.Time `json:"updated"`
	Expires *time.Time `json:"expires"`
}

type responseMeta struct {
	Source    string    `json:"source"`
	Server    *string   `json:"server"`
	FetchedAt time.Time `json:"fetchedAt"`
	Cached    bool      `json:"cached"`
}

// NewPi creates the authenticated Raspberry Pi source.
func NewPi(timeout time.Duration) *Provider {
	baseURL := strings.TrimSpace(os.Getenv("DOMAINHUNTER_WHO_DAT_URL"))
	apiKey := strings.TrimSpace(os.Getenv("DOMAINHUNTER_WHO_DAT_API_KEY"))
	return New(query.ProviderWhoDat, baseURL, apiKey, timeout)
}

// NewVercel creates the user's public Vercel who-dat source.
func NewVercel(timeout time.Duration) *Provider {
	baseURL := strings.TrimSpace(os.Getenv("DOMAINHUNTER_VERCEL_WHO_DAT_URL"))
	if baseURL == "" {
		baseURL = "https://rdap.re"
	}
	return New(query.ProviderVercelWhoDat, baseURL, "", timeout)
}

// New creates a named who-dat provider.
func New(name, baseURL, apiKey string, timeout time.Duration) *Provider {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Provider{
		name:    strings.TrimSpace(name),
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:  strings.TrimSpace(apiKey),
		timeout: timeout,
	}
}

// Name implements query.Provider.
func (p *Provider) Name() string { return p.name }

// UpdateTimeout implements query.TimeoutAware.
func (p *Provider) UpdateTimeout(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	p.mu.Lock()
	p.timeout = timeout
	p.mu.Unlock()
}

// Supports is true when an endpoint is configured. TLD support is determined by
// the endpoint itself so a 501 response can safely advance the fallback chain.
func (p *Provider) Supports(context.Context, query.Request) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.baseURL != ""
}

func (p *Provider) snapshot() (string, string, time.Duration) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.baseURL, p.apiKey, p.timeout
}

// Query performs one normalized who-dat lookup.
func (p *Provider) Query(ctx context.Context, req query.Request) query.Result {
	started := time.Now()
	baseURL, apiKey, timeout := p.snapshot()
	endpoint, err := endpoint(baseURL, req.Domain)
	if err != nil {
		return p.failure(req.Domain, started, err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return p.failure(req.Domain, started, err)
	}
	httpReq.Header.Set("Accept", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := httpx.Client(timeout).Do(httpReq)
	if err != nil {
		return p.failure(req.Domain, started, fmt.Errorf("who-dat 请求失败: %w", err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return p.failure(req.Domain, started, fmt.Errorf("读取 who-dat 响应失败: %w", err))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		kind := query.KindUnavailable
		if resp.StatusCode == http.StatusTooManyRequests {
			kind = query.KindRateLimited
		}
		return p.failure(req.Domain, started, query.NewError(kind, p.Name(), "who-dat 返回 HTTP %d", resp.StatusCode))
	}

	var parsed response
	if err := json.Unmarshal(body, &parsed); err != nil {
		return p.failureWithRaw(req.Domain, started, string(body), query.NewError(query.KindParse, p.Name(), "解析 who-dat 响应失败: %v", err))
	}
	if parsed.IsRegistered == nil || strings.TrimSpace(parsed.Domain) == "" {
		return p.failureWithRaw(req.Domain, started, string(body), query.NewError(query.KindUnknownResponse, p.Name(), "who-dat 响应缺少 isRegistered 或 domain"))
	}

	result := query.Result{
		Domain:      req.Domain,
		Provider:    p.Name(),
		Registrar:   stringValue(parsed.Registrar.Name),
		CreatedAt:   parsed.Dates.Created,
		UpdatedAt:   parsed.Dates.Updated,
		ExpiryAt:    parsed.Dates.Expires,
		NameServers: nameserverNames(parsed.Nameservers),
		EPPStatuses: append([]string(nil), parsed.Status...),
		Raw:         string(body),
		Confidence:  domain.ConfidenceHigh,
		StartedAt:   started,
	}
	if *parsed.IsRegistered {
		result.Status = statusFromWhoDat(parsed.Status)
		result.LifecycleEvidence = hasLifecycleStatus(parsed.Status)
	} else {
		if detect.ContainsReserved(string(body), strings.Join(parsed.Status, " "), stringValue(parsed.Registrar.Name)) {
			result.Status = domain.StatusUnknown
			result.Confidence = domain.ConfidenceLow
			result.Note = "who-dat 返回保留或禁止注册信息"
		} else {
			result.Status = domain.StatusAvailable
		}
	}
	result.FinishedAt = time.Now()
	result.Latency = result.FinishedAt.Sub(started)
	if parsed.Meta.Cached {
		result.Note = "who-dat 返回缓存结果"
	}
	return query.NormalizeIMLifecycle(result)
}

func (p *Provider) failure(name string, started time.Time, err error) query.Result {
	return p.failureWithRaw(name, started, "", err)
}

func (p *Provider) failureWithRaw(name string, started time.Time, raw string, err error) query.Result {
	finished := time.Now()
	return query.Result{
		Domain:     name,
		Status:     domain.StatusError,
		Provider:   p.Name(),
		Confidence: domain.ConfidenceLow,
		Raw:        raw,
		Err:        query.WrapError(p.Name(), err),
		StartedAt:  started,
		FinishedAt: finished,
		Latency:    finished.Sub(started),
	}
}

func endpoint(baseURL, name string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", query.NewError(query.KindNotSupported, query.ProviderWhoDat, "who-dat URL 无效")
	}
	return strings.TrimRight(parsed.String(), "/") + "/v1/whois/" + url.PathEscape(strings.TrimSpace(name)), nil
}

func nameserverNames(items []nameserver) []string {
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		name := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(item.Name)), ".")
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func statusFromWhoDat(statuses []string) domain.Status {
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

func hasLifecycleStatus(statuses []string) bool {
	for _, raw := range statuses {
		key := strings.ToLower(strings.NewReplacer(" ", "", "_", "", "-", "").Replace(raw))
		switch {
		case strings.Contains(key, "hold"),
			strings.Contains(key, "redemption"),
			strings.Contains(key, "pendingdelete"),
			strings.Contains(key, "expired"),
			strings.Contains(key, "renewperiod"),
			strings.Contains(key, "grace"):
			return true
		}
	}
	return false
}
