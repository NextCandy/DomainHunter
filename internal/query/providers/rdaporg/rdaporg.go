// Package rdaporg queries the public rdap.org forwarding service.
//
// It intentionally has its own provider identity even though the payload is
// standard RDAP: the evidence view must show when the registry-direct source
// was unavailable and rdap.org became the fifth network fallback.
package rdaporg

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
	"DomainHunter/internal/query/providers/rdap"
)

const maxBodyBytes = 4 << 20

type Provider struct {
	mu      sync.RWMutex
	baseURL string
	timeout time.Duration
}

// New creates the rdap.org provider. The endpoint is configurable for tests and
// private mirrors, defaulting to the documented public service.
func New(timeout time.Duration) *Provider {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	baseURL := strings.TrimRight(os.Getenv("DOMAINHUNTER_RDAP_ORG_URL"), "/")
	if baseURL == "" {
		baseURL = "https://rdap.org"
	}
	return &Provider{baseURL: baseURL, timeout: timeout}
}

func (p *Provider) Name() string { return query.ProviderRdapOrg }

func (p *Provider) Supports(context.Context, query.Request) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.baseURL != ""
}

func (p *Provider) UpdateTimeout(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	p.mu.Lock()
	p.timeout = timeout
	p.mu.Unlock()
}

func (p *Provider) snapshot() (string, time.Duration) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.baseURL, p.timeout
}

func (p *Provider) Query(ctx context.Context, req query.Request) query.Result {
	started := time.Now()
	baseURL, timeout := p.snapshot()
	base, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || base.Scheme == "" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return p.failure(req.Domain, started, query.NewError(query.KindNotSupported, p.Name(), "rdap.org URL 无效"))
	}
	endpoint := base.String() + "/domain/" + url.PathEscape(req.Domain)
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return p.failure(req.Domain, started, err)
	}
	httpReq.Header.Set("Accept", "application/rdap+json")
	httpReq.Header.Set("User-Agent", "DomainHunter/rdap-org")

	resp, err := httpx.Client(timeout).Do(httpReq)
	if err != nil {
		return p.failure(req.Domain, started, fmt.Errorf("rdap.org 请求失败: %w", err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return p.failure(req.Domain, started, fmt.Errorf("读取 rdap.org 响应失败: %w", err))
	}
	raw := string(body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		kind := query.KindUnavailable
		if resp.StatusCode == http.StatusTooManyRequests {
			kind = query.KindRateLimited
		}
		return p.failureWithRaw(req.Domain, started, raw, query.NewError(kind, p.Name(), "rdap.org 返回 HTTP %d", resp.StatusCode))
	}

	var parsed rdap.Response
	if err := json.Unmarshal(body, &parsed); err != nil {
		return p.failureWithRaw(req.Domain, started, raw, query.NewError(query.KindParse, p.Name(), "解析 rdap.org 响应失败: %v", err))
	}
	if resp.StatusCode == http.StatusNotFound && parsed.ErrorCode != http.StatusNotFound {
		return p.failureWithRaw(req.Domain, started, raw, query.NewError(query.KindUnknownResponse, p.Name(), "rdap.org 404 缺少匹配的 errorCode"))
	}
	if resp.StatusCode == http.StatusOK && parsed.ObjectClassName == "" && parsed.ErrorCode != 0 {
		return p.failureWithRaw(req.Domain, started, raw, query.NewError(query.KindUnknownResponse, p.Name(), "rdap.org 响应不是域名对象"))
	}
	result := rdap.ParseResponse(req.Domain, &parsed, raw)
	result.Provider = p.Name()
	result.StartedAt = started
	result.FinishedAt = time.Now()
	result.Latency = result.FinishedAt.Sub(started)
	return result
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
