// Package whoisls 实现 WHOIS.LS JSON 网关查询源。
//
// 它只对显式配置的后缀生效（部署上是 .im）。WHOIS.LS 返回的是注册局的文本
// 响应，因此复用 whois 包的解析器。.im 的公开响应不含创建日期，DomainHunter
// 保持创建日期为空，绝不猜测或填充伪造日期。
package whoisls

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
	"DomainHunter/internal/query/envcfg"
	"DomainHunter/internal/query/providers/whois"
)

const maxBodyBytes = 1 << 20

type apiResponse struct {
	Error json.RawMessage `json:"error"`
	Data  string          `json:"data"`
}

// Provider WHOIS.LS 查询源
type Provider struct {
	mu      sync.RWMutex
	baseURL string
	tlds    map[string]struct{}
	timeout time.Duration
}

// New 从环境变量创建 Provider。
// DOMAINHUNTER_WHOIS_LS_URL 默认为空；旧的 PUFF_WHOIS_LS_* 变量同样被接受。
func New(timeout time.Duration) *Provider {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	if configured := envcfg.First("DOMAINHUNTER_WHOIS_LS_TIMEOUT", "PUFF_WHOIS_LS_TIMEOUT"); configured != "" {
		if parsed, ok := envcfg.ParseDuration(configured); ok {
			timeout = parsed
		}
	}
	return &Provider{
		baseURL: strings.TrimSpace(envcfg.First("DOMAINHUNTER_WHOIS_LS_URL", "PUFF_WHOIS_LS_URL")),
		tlds:    envcfg.ParseTLDs(envcfg.First("DOMAINHUNTER_WHOIS_LS_TLDS", "PUFF_WHOIS_LS_TLDS"), "im"),
		timeout: timeout,
	}
}

// Name 实现 query.Provider
func (p *Provider) Name() string { return query.ProviderWhoisLS }

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

// Query 执行一次 WHOIS.LS 查询
func (p *Provider) Query(ctx context.Context, req query.Request) query.Result {
	started := time.Now()

	raw, err := p.fetch(ctx, req.Domain)
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

	result := whois.Parse(req.Domain, raw)
	result.Provider = p.Name()
	result.Raw = raw
	result.StartedAt = started
	result.FinishedAt = time.Now()
	result.Latency = result.FinishedAt.Sub(started)

	// 注册局明确回复"未找到"即可判定可注册；含糊或空响应绝不判定可注册。
	if isNotFound(raw) && !result.HasRegistrationEvidence() {
		result.Status = domain.StatusAvailable
		result.Confidence = domain.ConfidenceHigh
		result.Note = ""
	}
	if result.Status == domain.StatusAvailable && result.HasRegistrationEvidence() {
		result.Status = domain.StatusRegistered
		result.Note = "响应中存在注册信息，可注册判定已纠正为已注册"
	}
	return result
}

func (p *Provider) fetch(ctx context.Context, name string) (string, error) {
	baseURL, timeout := p.snapshot()
	endpoint, err := Endpoint(baseURL, name)
	if err != nil {
		return "", err
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("创建 WHOIS.LS 请求失败: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")

	resp, err := httpx.Client(timeout).Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("WHOIS.LS 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", fmt.Errorf("读取 WHOIS.LS 响应失败: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		if resp.StatusCode == http.StatusTooManyRequests {
			return "", query.NewError(query.KindRateLimited, query.ProviderWhoisLS, "WHOIS.LS 返回 HTTP 429")
		}
		return "", query.NewError(query.KindUnavailable, query.ProviderWhoisLS, "WHOIS.LS 返回 HTTP %d", resp.StatusCode)
	}

	var parsed apiResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", query.NewError(query.KindParse, query.ProviderWhoisLS, "解析 WHOIS.LS 响应失败: %v", err)
	}
	if rawBool(parsed.Error) {
		message := strings.TrimSpace(parsed.Data)
		if message == "" {
			message = "WHOIS.LS 查询失败"
		}
		return "", query.NewError(query.KindUnavailable, query.ProviderWhoisLS, "%s", message)
	}
	data := strings.TrimSpace(parsed.Data)
	if data == "" {
		return "", query.NewError(query.KindUnknownResponse, query.ProviderWhoisLS, "WHOIS.LS 响应缺少 data")
	}
	return data, nil
}

// Endpoint 拼接 WHOIS.LS 查询地址
func Endpoint(rawBaseURL, name string) (string, error) {
	if strings.TrimSpace(rawBaseURL) == "" {
		return "", query.NewError(query.KindNotSupported, query.ProviderWhoisLS, "WHOIS.LS URL 未配置")
	}
	parsed, err := url.Parse(rawBaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", query.NewError(query.KindNotSupported, query.ProviderWhoisLS, "WHOIS.LS URL 无效")
	}
	path := strings.TrimRight(parsed.Path, "/")
	if path == "" {
		path = "/json"
	}
	parsed.Path = path + "/" + url.PathEscape(strings.TrimSpace(name))
	parsed.RawPath = ""
	return parsed.String(), nil
}

func rawBool(raw json.RawMessage) bool {
	value := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	return strings.EqualFold(value, "true") || value == "1"
}

// isNotFound 判断响应是否是注册局明确的"域名不存在"回复
func isNotFound(raw string) bool {
	lower := strings.ToLower(raw)
	if !strings.Contains(lower, "domain name:") {
		return false
	}
	for _, phrase := range []string{
		" was not found",
		"domain not found",
		"no match for",
		"no such domain",
		"not been registered",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}
