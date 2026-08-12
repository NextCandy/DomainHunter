// Package ai provides a deliberately conservative AI fallback for domain
// lookups. It is only reached after every authoritative network source has
// failed or returned an ambiguous answer. The AI can add an explanatory note,
// but it is never allowed to create an available conclusion.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/query"
)

const maxBodyBytes = 1 << 20

type Provider struct {
	mu      sync.RWMutex
	baseURL string
	apiKey  string
	model   string
	timeout time.Duration
	resolve Resolver
}

// Resolver returns the currently selected default AI profile. DomainHunter's
// profile store is owned by P1, so the fallback reads it through this small
// callback and follows profile switches without importing the P1 package.
type Resolver func(context.Context) (baseURL, apiKey, model string, enabled bool, err error)

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// New creates the fallback from the same environment-backed default AI
// settings used by the P1 valuation worker.
func New(timeout time.Duration) *Provider {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	baseURL := strings.TrimRight(os.Getenv("DOMAINHUNTER_AI_BASE_URL"), "/")
	if baseURL == "" {
		baseURL = "https://opencode.ai/zen/v1"
	}
	model := strings.TrimSpace(os.Getenv("DOMAINHUNTER_AI_MODEL"))
	if model == "" {
		model = "deepseek-v4-flash-free"
	}
	return &Provider{
		baseURL: baseURL,
		apiKey:  strings.TrimSpace(os.Getenv("DOMAINHUNTER_AI_API_KEY")),
		model:   model,
		timeout: timeout,
	}
}

// NewWithResolver uses the persisted default profile and falls back to the
// environment-backed defaults only if no resolver is supplied.
func NewWithResolver(timeout time.Duration, resolver Resolver) *Provider {
	p := New(timeout)
	p.resolve = resolver
	return p
}

func (p *Provider) Name() string { return query.ProviderAIFallback }

func (p *Provider) Supports(context.Context, query.Request) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.resolve != nil {
		return true
	}
	return p.baseURL != "" && p.apiKey != "" && p.model != ""
}

func (p *Provider) UpdateTimeout(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	p.mu.Lock()
	p.timeout = timeout
	p.mu.Unlock()
}

func (p *Provider) snapshot(ctx context.Context) (string, string, string, bool, time.Duration, error) {
	p.mu.RLock()
	resolve := p.resolve
	baseURL, apiKey, model, timeout := p.baseURL, p.apiKey, p.model, p.timeout
	p.mu.RUnlock()
	if resolve != nil {
		resolvedURL, resolvedKey, resolvedModel, enabled, err := resolve(ctx)
		return strings.TrimRight(strings.TrimSpace(resolvedURL), "/"), strings.TrimSpace(resolvedKey), strings.TrimSpace(resolvedModel), enabled, timeout, err
	}
	return baseURL, apiKey, model, true, timeout, nil
}

func (p *Provider) currentConfig(ctx context.Context) (string, string, string, bool, time.Duration, error) {
	return p.snapshot(ctx)
}

func (p *Provider) Query(ctx context.Context, req query.Request) query.Result {
	started := time.Now()
	baseURL, apiKey, model, enabled, timeout, resolveErr := p.snapshot(ctx)
	if resolveErr != nil {
		return p.unknown(req.Domain, started, "默认 AI 配置读取失败")
	}
	if !enabled || baseURL == "" || apiKey == "" || model == "" {
		return p.unknown(req.Domain, started, "默认 AI 未配置，无法进行研究性兜底")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return p.unknown(req.Domain, started, "默认 AI 地址未通过安全校验")
	}

	payload := map[string]any{
		"model":       model,
		"temperature": 0,
		"max_tokens":  160,
		"messages": []map[string]string{
			{"role": "system", "content": "你是域名查询故障兜底助手。只输出 JSON：{\"assessment\":\"registered|available|unknown\",\"note\":\"一句话\"}。没有注册局证据时 assessment 必须是 unknown，绝不把推测当作可注册。"},
			{"role": "user", "content": fmt.Sprintf("域名：%s\n所有权威查询源均未给出可采信的确定状态。请仅总结为什么保持未知，不要猜测。", req.Domain)},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return p.unknown(req.Domain, started, "默认 AI 请求编码失败")
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(requestCtx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return p.unknown(req.Domain, started, "默认 AI 请求创建失败")
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: safeDial}}
	resp, err := client.Do(httpReq)
	if err != nil {
		return p.unknown(req.Domain, started, "默认 AI 兜底请求失败")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil || resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return p.unknown(req.Domain, started, "默认 AI 兜底未返回有效响应")
	}
	var envelope chatResponse
	if err := json.Unmarshal(data, &envelope); err != nil || len(envelope.Choices) == 0 {
		return p.unknown(req.Domain, started, "默认 AI 兜底响应格式不完整")
	}
	note := parseNote(envelope.Choices[0].Message.Content)
	return p.unknown(req.Domain, started, "AI 兜底："+note)
}

func (p *Provider) unknown(name string, started time.Time, note string) query.Result {
	finished := time.Now()
	return query.Result{Domain: name, Status: domain.StatusUnknown, Provider: p.Name(), Confidence: domain.ConfidenceLow, Note: note, StartedAt: started, FinishedAt: finished, Latency: finished.Sub(started)}
}

func parseNote(content string) string {
	var parsed struct {
		Assessment string `json:"assessment"`
		Note       string `json:"note"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(content)), &parsed) == nil && strings.TrimSpace(parsed.Note) != "" {
		note := strings.TrimSpace(parsed.Note)
		if len(note) > 240 {
			note = note[:240]
		}
		return note
	}
	return "未获得可采信的注册局证据，保持未知"
}

func safeDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			continue
		}
		conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, fmt.Errorf("AI 目标地址没有可用公网 IP")
}
