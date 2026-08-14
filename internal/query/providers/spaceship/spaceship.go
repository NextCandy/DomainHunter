// Package spaceship implements the optional Spaceship External API fallback.
//
// The API is intentionally used only as a late .im evidence source. It can
// answer whether a name is available, but it does not provide .im creation
// dates or registrar lifecycle stages such as grace/redemption.
package spaceship

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
	"DomainHunter/internal/query/envcfg"
)

const maxBodyBytes = 1 << 20

type availabilityResponse struct {
	Domain      string `json:"domain"`
	Result      string `json:"result"`
	Status      string `json:"status"`
	Available   *bool  `json:"available"`
	IsAvailable *bool  `json:"isAvailable"`
}

// Provider queries the authenticated Spaceship External API.
type Provider struct {
	mu        sync.RWMutex
	baseURL   string
	apiKey    string
	apiSecret string
	tlds      map[string]struct{}
	timeout   time.Duration
}

// New creates a Spaceship provider from environment variables. It is disabled
// unless both API credentials are present.
func New(timeout time.Duration) *Provider {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	if configured := strings.TrimSpace(os.Getenv("DOMAINHUNTER_SPACESHIP_TIMEOUT")); configured != "" {
		if parsed, ok := envcfg.ParseDuration(configured); ok {
			timeout = parsed
		}
	}
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("DOMAINHUNTER_SPACESHIP_API_URL")), "/")
	if baseURL == "" {
		baseURL = "https://spaceship.dev/api/v1"
	}
	return &Provider{
		baseURL:   baseURL,
		apiKey:    strings.TrimSpace(os.Getenv("DOMAINHUNTER_SPACESHIP_API_KEY")),
		apiSecret: strings.TrimSpace(os.Getenv("DOMAINHUNTER_SPACESHIP_API_SECRET")),
		tlds:      envcfg.ParseTLDs(os.Getenv("DOMAINHUNTER_SPACESHIP_TLDS"), "im"),
		timeout:   timeout,
	}
}

// NewWithConfig creates a provider directly for tests and private API mirrors.
func NewWithConfig(baseURL, apiKey, apiSecret string, timeout time.Duration, tlds ...string) *Provider {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	if len(tlds) == 0 {
		tlds = []string{"im"}
	}
	return &Provider{
		baseURL:   strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:    strings.TrimSpace(apiKey),
		apiSecret: strings.TrimSpace(apiSecret),
		tlds:      envcfg.ParseTLDs(strings.Join(tlds, ",")),
		timeout:   timeout,
	}
}

func (p *Provider) Name() string { return query.ProviderSpaceship }

func (p *Provider) Supports(_ context.Context, req query.Request) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.baseURL == "" || p.apiKey == "" || p.apiSecret == "" {
		return false
	}
	_, ok := p.tlds[strings.ToLower(strings.Trim(strings.TrimSpace(req.TLD), "."))]
	return ok
}

func (p *Provider) UpdateTimeout(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	p.mu.Lock()
	p.timeout = timeout
	p.mu.Unlock()
}

func (p *Provider) snapshot() (string, string, string, time.Duration) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.baseURL, p.apiKey, p.apiSecret, p.timeout
}

func (p *Provider) Query(ctx context.Context, req query.Request) query.Result {
	started := time.Now()
	baseURL, apiKey, apiSecret, timeout := p.snapshot()
	endpoint, err := Endpoint(baseURL, req.Domain)
	if err != nil {
		return p.failure(req.Domain, started, err)
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return p.failure(req.Domain, started, query.NewError(query.KindNotSupported, p.Name(), "Spaceship 请求地址无效"))
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("X-API-Key", apiKey)
	httpReq.Header.Set("X-API-Secret", apiSecret)

	resp, err := httpx.Client(timeout).Do(httpReq)
	if err != nil {
		return p.failure(req.Domain, started, query.WrapError(p.Name(), fmt.Errorf("Spaceship 请求失败: %w", err)))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return p.failure(req.Domain, started, query.WrapError(p.Name(), fmt.Errorf("读取 Spaceship 响应失败: %w", err)))
	}
	raw := string(body)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return p.failureWithRaw(req.Domain, started, raw, spaceshipHTTPError(resp.StatusCode))
	}

	var parsed availabilityResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return p.failureWithRaw(req.Domain, started, raw,
			query.NewError(query.KindParse, p.Name(), "解析 Spaceship 响应失败: %v", err))
	}
	result := query.Result{
		Domain:     req.Domain,
		Provider:   p.Name(),
		Raw:        raw,
		Confidence: domain.ConfidenceHigh,
		StartedAt:  started,
	}
	switch availabilityState(parsed) {
	case "available":
		result.Status = domain.StatusAvailable
		result.Note = "Spaceship 官方 API 确认可用；仍需其他查询源确认后才显示可注册"
	case "registered":
		result.Status = domain.StatusRegistered
		result.Note = "Spaceship 官方 API 确认域名不可用；该接口不提供 .im 注册日期或生命周期阶段"
	default:
		result.Status = domain.StatusUnknown
		result.Confidence = domain.ConfidenceLow
		result.Note = "Spaceship 未返回可解析的域名可用性结果"
	}
	result.FinishedAt = time.Now()
	result.Latency = result.FinishedAt.Sub(started)
	return result
}

func Endpoint(rawBaseURL, name string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(rawBaseURL), "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", query.NewError(query.KindNotSupported, query.ProviderSpaceship, "Spaceship API URL 无效")
	}
	if strings.TrimSpace(name) == "" {
		return "", query.NewError(query.KindNotSupported, query.ProviderSpaceship, "域名不能为空")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/domains/" + url.PathEscape(strings.TrimSpace(name)) + "/available"
	parsed.RawPath = ""
	return parsed.String(), nil
}

func availabilityState(response availabilityResponse) string {
	if response.Available != nil {
		if *response.Available {
			return "available"
		}
		return "registered"
	}
	if response.IsAvailable != nil {
		if *response.IsAvailable {
			return "available"
		}
		return "registered"
	}
	value := strings.ToLower(strings.TrimSpace(response.Result))
	if value == "" {
		value = strings.ToLower(strings.TrimSpace(response.Status))
	}
	key := strings.NewReplacer(" ", "", "_", "", "-", "").Replace(value)
	switch key {
	case "available", "free", "canregister":
		return "available"
	case "unavailable", "notavailable", "registered", "taken", "occupied":
		return "registered"
	default:
		return "unknown"
	}
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

func spaceshipHTTPError(status int) error {
	switch status {
	case http.StatusTooManyRequests:
		return query.NewError(query.KindRateLimited, query.ProviderSpaceship, "Spaceship API 返回 HTTP 429")
	case http.StatusUnauthorized, http.StatusForbidden:
		return query.NewError(query.KindNotSupported, query.ProviderSpaceship, "Spaceship API Key/Secret 无效或缺少 domains:read 权限")
	default:
		return query.NewError(query.KindUnavailable, query.ProviderSpaceship, "Spaceship API 返回 HTTP %d", status)
	}
}
