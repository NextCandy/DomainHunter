// Package rdap 实现 RDAP 查询源。
//
// 安全要点（与重构前一致，不可放宽）：
//   - HTTP 404 只有在响应体是合法的 RDAP 错误对象、且 errorCode 匹配时才被采纳；
//     HTML 错误页、空响应、代理路由 404 一律作为错误进入重试
//   - 即使是合法 404，也必须出现明确的"域名不存在"语义才判定可注册；
//     出现保留 / 禁止注册语义时保持未知
//   - 空 RDAP 对象不能推断为可注册
package rdap

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/httpx"
	"DomainHunter/internal/query"
	"DomainHunter/internal/query/detect"
	"DomainHunter/internal/registry"
)

const maxBodyBytes = 4 << 20

// userAgent 使用常见浏览器标识：部分注册局（如 ch/li）会拒绝非浏览器请求
const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// Response RDAP 响应结构
type Response struct {
	ObjectClassName string       `json:"objectClassName"`
	Handle          string       `json:"handle"`
	LDHName         string       `json:"ldhName"`
	Status          []string     `json:"status"`
	Entities        []Entity     `json:"entities"`
	Events          []Event      `json:"events"`
	NameServers     []NameServer `json:"nameservers"`
	ErrorCode       int          `json:"errorCode,omitempty"`
	Title           string       `json:"title,omitempty"`
	Description     []string     `json:"description,omitempty"`
}

// Entity RDAP 实体
type Entity struct {
	ObjectClassName string   `json:"objectClassName"`
	Handle          string   `json:"handle"`
	Roles           []string `json:"roles"`
	VCardArray      []any    `json:"vcardArray,omitempty"`
}

// Event RDAP 事件
type Event struct {
	EventAction string    `json:"eventAction"`
	EventDate   time.Time `json:"eventDate"`
}

// NameServer RDAP 名称服务器
type NameServer struct {
	ObjectClassName string `json:"objectClassName"`
	LDHName         string `json:"ldhName"`
}

// Provider RDAP 查询源
type Provider struct {
	mu      sync.RWMutex
	timeout time.Duration
}

// New 创建 RDAP Provider
func New(timeout time.Duration) *Provider {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Provider{timeout: timeout}
}

// Name 实现 query.Provider
func (p *Provider) Name() string { return query.ProviderRDAP }

// UpdateTimeout 实现 query.TimeoutAware
func (p *Provider) UpdateTimeout(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	p.mu.Lock()
	p.timeout = timeout
	p.mu.Unlock()
}

func (p *Provider) currentTimeout() time.Duration {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.timeout
}

// Supports RDAP 是通用查询源，永远参与计划；没有端点时在 Query 内返回 skipped
func (p *Provider) Supports(context.Context, query.Request) bool { return true }

// Query 执行一次 RDAP 查询
func (p *Provider) Query(ctx context.Context, req query.Request) query.Result {
	started := time.Now()

	server, ok := registry.RDAPServerFor(req.Domain)
	if !ok {
		return query.Result{
			Domain:     req.Domain,
			Status:     domain.StatusSkipped,
			Provider:   p.Name(),
			Confidence: domain.ConfidenceLow,
			Note:       fmt.Sprintf("TLD %s 没有 RDAP 服务器，等待 WHOIS 或备用源", req.TLD),
			StartedAt:  started,
			FinishedAt: time.Now(),
		}
	}

	resp, raw, err := p.fetch(ctx, req.Domain, server.Server)
	if err != nil {
		finished := time.Now()
		return query.Result{
			Domain:     req.Domain,
			Status:     domain.StatusError,
			Provider:   p.Name(),
			Confidence: domain.ConfidenceLow,
			Raw:        raw,
			Err:        query.WrapError(p.Name(), fmt.Errorf("RDAP查询失败: %w", err)),
			StartedAt:  started,
			FinishedAt: finished,
			Latency:    finished.Sub(started),
		}
	}

	result := ParseResponse(req.Domain, resp, raw)
	result.StartedAt = started
	result.FinishedAt = time.Now()
	result.Latency = result.FinishedAt.Sub(started)
	return result
}

func (p *Provider) fetch(ctx context.Context, name, serverURL string) (*Response, string, error) {
	if strings.TrimSpace(serverURL) == "" {
		return nil, "", query.NewError(query.KindNotSupported, query.ProviderRDAP, "RDAP服务器地址为空")
	}
	endpoint := strings.TrimSuffix(serverURL, "/") + "/domain/" + name

	timeout := p.currentTimeout()
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", err
	}
	httpReq.Header.Set("Accept", "application/rdap+json")
	httpReq.Header.Set("User-Agent", userAgent)

	resp, err := httpx.Client(timeout).Do(httpReq)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, "", err
	}
	raw := string(body)

	switch {
	case resp.StatusCode == http.StatusNotFound:
		parsed, err := parseErrorBody(body, http.StatusNotFound)
		if err != nil {
			return nil, raw, err
		}
		return parsed, raw, nil

	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, raw, query.NewError(query.KindRateLimited, query.ProviderRDAP, "RDAP服务器限流(429)，请稍后重试")

	case resp.StatusCode != http.StatusOK:
		return nil, raw, query.NewError(query.KindUnavailable, query.ProviderRDAP, "HTTP请求失败，状态码: %d", resp.StatusCode)
	}

	var parsed Response
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, raw, query.NewError(query.KindParse, query.ProviderRDAP, "解析JSON响应失败: %v", err)
	}
	return &parsed, raw, nil
}

func parseErrorBody(body []byte, expectedCode int) (*Response, error) {
	var response Response
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, query.NewError(query.KindParse, query.ProviderRDAP,
			"RDAP HTTP %d 响应不是有效 JSON: %v", expectedCode, err)
	}
	if response.ErrorCode != expectedCode {
		return nil, query.NewError(query.KindUnknownResponse, query.ProviderRDAP,
			"RDAP HTTP %d 响应缺少匹配的 errorCode", expectedCode)
	}
	return &response, nil
}

// ParseResponse 把 RDAP 响应解析为统一结果
func ParseResponse(name string, resp *Response, raw string) query.Result {
	result := query.Result{
		Domain:     name,
		Provider:   query.ProviderRDAP,
		Raw:        raw,
		Confidence: domain.ConfidenceMedium,
	}
	if resp == nil {
		result.Status = domain.StatusError
		result.Err = query.NewError(query.KindUnknownResponse, query.ProviderRDAP, "RDAP响应为空")
		result.Confidence = domain.ConfidenceLow
		return result
	}

	if resp.ErrorCode == http.StatusNotFound {
		fields := append([]string{resp.Title}, resp.Description...)
		switch {
		case detect.ContainsReserved(raw, fields...):
			result.Status = domain.StatusUnknown
			result.Confidence = domain.ConfidenceLow
			result.Note = "RDAP 返回保留或禁止注册信息"
		case !detect.ContainsNotFound(raw, fields...):
			result.Status = domain.StatusUnknown
			result.Confidence = domain.ConfidenceLow
			result.Note = "RDAP 404 未提供明确的域名不存在语义"
		default:
			result.Status = domain.StatusAvailable
			result.Confidence = domain.ConfidenceHigh
		}
		return result
	}

	result.EPPStatuses = append([]string(nil), resp.Status...)
	result.Status = ParseStatus(resp.Status)
	result.Registrar = parseRegistrar(resp.Entities)
	applyEvents(resp.Events, &result)
	result.NameServers = parseNameServers(resp.NameServers)

	if result.Status == domain.StatusUnknown {
		desc := strings.ToLower(strings.Join(resp.Description, " "))
		title := strings.ToLower(resp.Title)
		if detect.ContainsReserved(raw, title, desc) {
			result.Note = "RDAP 返回保留或禁止注册信息"
		} else if detect.ContainsNotFound(raw, title, desc) {
			result.Status = domain.StatusAvailable
		}
	}

	hasRegistrationData := hasRegistrationData(result, resp)

	// 判定为可注册但存在注册信息 —— 误判，纠正为已注册。
	if result.Status == domain.StatusAvailable && hasRegistrationData {
		result.Status = domain.StatusRegistered
		result.Note = "RDAP 响应中存在注册信息，可注册判定已纠正为已注册"
	}

	// 状态仍未知且有注册信息时才补判为已注册，
	// 不覆盖已经识别出的宽限期/赎回期/待删除等特殊状态。
	if result.Status == domain.StatusUnknown && hasRegistrationData {
		result.Status = domain.StatusRegistered
	}

	switch {
	case result.Status == domain.StatusUnknown:
		result.Confidence = domain.ConfidenceLow
	case hasRegistrationData:
		// RDAP 是结构化数据源，拿到注册商/事件即视为高可信
		result.Confidence = domain.ConfidenceHigh
	}
	return result
}

func hasRegistrationData(result query.Result, resp *Response) bool {
	hasRegistrar := result.Registrar != "" && !strings.Contains(result.Registrar, "不支持")
	return hasRegistrar ||
		result.ExpiryAt != nil ||
		result.CreatedAt != nil ||
		len(resp.Events) > 0 ||
		len(result.NameServers) > 0
}

// ParseStatus 把 RDAP status 数组映射为业务状态
func ParseStatus(statuses []string) domain.Status {
	if len(statuses) == 0 {
		return domain.StatusUnknown
	}
	set := make(map[string]bool, len(statuses))
	for _, status := range statuses {
		set[strings.ToLower(strings.TrimSpace(status))] = true
	}

	for status := range set {
		if strings.Contains(status, "hold") {
			return domain.StatusHold
		}
		if strings.Contains(status, "transfer") &&
			(strings.Contains(status, "prohibited") || strings.Contains(status, "locked")) {
			return domain.StatusTransferLocked
		}
	}
	for status := range set {
		if strings.Contains(status, "reserved") || strings.Contains(status, "prohibited") ||
			strings.Contains(status, "not allowed") {
			return domain.StatusUnknown
		}
	}
	if set["redemption period"] || set["redemptionperiod"] {
		return domain.StatusRedemption
	}
	if set["pending delete"] || set["pendingdelete"] {
		return domain.StatusPendingDelete
	}
	if set["renew period"] || set["auto renew period"] || set["expired"] {
		return domain.StatusGrace
	}
	return domain.StatusRegistered
}

func parseRegistrar(entities []Entity) string {
	for _, entity := range entities {
		for _, role := range entity.Roles {
			if strings.EqualFold(role, "registrar") {
				if org := extractOrgFromVCard(entity.VCardArray); org != "" {
					return org
				}
				return entity.Handle
			}
		}
	}
	return ""
}

func extractOrgFromVCard(vcard []any) string {
	if len(vcard) < 2 {
		return ""
	}
	properties, ok := vcard[1].([]any)
	if !ok {
		return ""
	}
	for _, prop := range properties {
		propArray, ok := prop.([]any)
		if !ok || len(propArray) < 4 {
			continue
		}
		propName, ok := propArray[0].(string)
		if !ok {
			continue
		}
		switch strings.ToLower(propName) {
		case "org", "fn":
			if value, ok := propArray[3].(string); ok && strings.TrimSpace(value) != "" {
				return value
			}
		}
	}
	return ""
}

func applyEvents(events []Event, result *query.Result) {
	for i := range events {
		event := events[i]
		switch strings.ToLower(event.EventAction) {
		case "registration":
			result.CreatedAt = &event.EventDate
		case "expiration", "soft expiration":
			result.ExpiryAt = &event.EventDate
		case "last changed", "last update of rdap database":
			result.UpdatedAt = &event.EventDate
		}
	}
}

func parseNameServers(nameservers []NameServer) []string {
	var out []string
	seen := map[string]bool{}
	for _, ns := range nameservers {
		if ns.LDHName == "" {
			continue
		}
		lower := strings.TrimSuffix(strings.ToLower(ns.LDHName), ".")
		if !seen[lower] {
			out = append(out, lower)
			seen[lower] = true
		}
	}
	return out
}
