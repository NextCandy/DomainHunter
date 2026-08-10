package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"DomainHunter/logger"
)

// WhoisFallbackClient calls the local whois-domain-lookup service only for
// explicitly enabled TLDs. It is intentionally opt-in so a service outage
// cannot change the behavior of unrelated domains.
type WhoisFallbackClient struct {
	mu      sync.RWMutex
	baseURL string
	tlds    map[string]struct{}
	timeout time.Duration
}

type whoisFallbackResponse struct {
	Code int                `json:"code"`
	Msg  string             `json:"msg"`
	Data *whoisFallbackData `json:"data"`
}

type whoisFallbackData struct {
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

// NewWhoisFallbackClient creates a client configured from environment
// variables. DOMAINHUNTER_WHOIS_FALLBACK_URL is empty by default. The legacy
// PUFF_WHOIS_FALLBACK_* names remain accepted so an existing deployment can
// be upgraded without changing its environment in the same step.
func NewWhoisFallbackClient(timeout time.Duration) *WhoisFallbackClient {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}

	if configured := firstEnv("DOMAINHUNTER_WHOIS_FALLBACK_TIMEOUT", "PUFF_WHOIS_FALLBACK_TIMEOUT"); configured != "" {
		if seconds, err := strconv.Atoi(configured); err == nil && seconds > 0 && seconds <= 120 {
			timeout = time.Duration(seconds) * time.Second
		}
	}

	return &WhoisFallbackClient{
		baseURL: strings.TrimSpace(firstEnv("DOMAINHUNTER_WHOIS_FALLBACK_URL", "PUFF_WHOIS_FALLBACK_URL")),
		tlds:    parseFallbackTLDs(firstEnv("DOMAINHUNTER_WHOIS_FALLBACK_TLDS", "PUFF_WHOIS_FALLBACK_TLDS")),
		timeout: timeout,
	}
}

func firstEnv(primary, legacy string) string {
	if value := strings.TrimSpace(os.Getenv(primary)); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv(legacy))
}

func parseFallbackTLDs(raw string) map[string]struct{} {
	if strings.TrimSpace(raw) == "" {
		raw = "im,do"
	}

	result := make(map[string]struct{})
	for _, item := range strings.Split(raw, ",") {
		tld := strings.ToLower(strings.Trim(strings.TrimSpace(item), "."))
		if tld != "" {
			result[tld] = struct{}{}
		}
	}
	return result
}

func (c *WhoisFallbackClient) UpdateTimeout(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	c.mu.Lock()
	c.timeout = timeout
	c.mu.Unlock()
}

func (c *WhoisFallbackClient) EnabledForTLD(tld string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.baseURL == "" {
		return false
	}
	_, ok := c.tlds[strings.ToLower(strings.Trim(strings.TrimSpace(tld), "."))]
	return ok
}

func (c *WhoisFallbackClient) snapshot() (string, time.Duration) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL, c.timeout
}

func (c *WhoisFallbackClient) Query(domain string) (*whoisFallbackResponse, error) {
	baseURL, timeout := c.snapshot()
	endpoint, err := fallbackEndpoint(baseURL, domain)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("创建备用 WHOIS 请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	response, err := GetProxyHTTPClient(timeout).Do(req)
	if err != nil {
		return nil, fmt.Errorf("备用 WHOIS 请求失败: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("读取备用 WHOIS 响应失败: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("备用 WHOIS 返回 HTTP %d", response.StatusCode)
	}

	var result whoisFallbackResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析备用 WHOIS 响应失败: %w", err)
	}
	if result.Code != 0 {
		if result.Msg == "" {
			result.Msg = "备用 WHOIS 查询失败"
		}
		return nil, fmt.Errorf("%s", result.Msg)
	}
	if result.Data == nil {
		return nil, fmt.Errorf("备用 WHOIS 响应缺少 data")
	}
	return &result, nil
}

func fallbackEndpoint(rawBaseURL, domain string) (string, error) {
	if strings.TrimSpace(rawBaseURL) == "" {
		return "", fmt.Errorf("备用 WHOIS URL 未配置")
	}

	parsed, err := url.Parse(rawBaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("备用 WHOIS URL 无效")
	}

	path := strings.TrimRight(parsed.Path, "/")
	if path == "" || path == "/" {
		parsed.Path = "/api/"
	} else if strings.HasSuffix(path, "/api") {
		parsed.Path = path + "/"
	} else {
		parsed.Path = path + "/api/"
	}

	query := parsed.Query()
	query.Set("domain", domain)
	query.Set("whois", "1")
	query.Set("rdap", "1")
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func (d *DomainChecker) tryWhoisFallbackQuery(domain, tld string) *DomainInfo {
	d.mu.RLock()
	fallbackClient := d.fallbackClient
	whoisClient := d.whoisClient
	d.mu.RUnlock()
	if fallbackClient == nil || !fallbackClient.EnabledForTLD(tld) {
		return nil
	}

	response, err := fallbackClient.Query(domain)
	if err != nil {
		logger.Warn("备用 WHOIS 查询失败 domain=%s tld=%s err=%v", domain, tld, err)
		return &DomainInfo{
			Name:         domain,
			Status:       StatusError,
			QueryMethod:  "whois-fallback",
			ErrorMessage: err.Error(),
			LastChecked:  time.Now(),
		}
	}

	data := response.Data
	raw := strings.TrimSpace(data.WhoisData)
	if raw == "" {
		raw = strings.TrimSpace(data.RDAPData)
	}

	info := &DomainInfo{
		Name:        domain,
		Registrar:   data.Registrar,
		CreatedDate: parseFallbackDate(data.CreationDate),
		ExpiryDate:  parseFallbackDate(data.ExpirationDate),
		UpdatedDate: parseFallbackDate(data.UpdatedDate),
		NameServers: append([]string(nil), data.NameServers...),
		QueryMethod: "whois-fallback",
		WhoisRaw:    raw,
		LastChecked: time.Now(),
	}
	if containsReservedRegistrationText(raw) {
		info.Status = StatusUnknown
		info.ErrorMessage = "备用 WHOIS 返回保留或禁止注册信息"
		return info
	}

	switch {
	case data.Registered != nil && *data.Registered:
		info.Status = StatusRegistered
	case data.Reserved != nil && *data.Reserved:
		info.Status = StatusUnknown
		info.ErrorMessage = "备用 WHOIS 返回保留或禁止注册状态"
	case data.Unknown != nil && *data.Unknown:
		info.Status = StatusUnknown
		info.ErrorMessage = "备用 WHOIS 未能确定域名状态"
	case data.Registered != nil && !*data.Registered && data.Reserved != nil && !*data.Reserved && data.Unknown != nil && !*data.Unknown:
		// Only a complete explicit non-reserved, non-unknown response may
		// become available. Missing flags never become available.
		info.Status = StatusAvailable
	default:
		info.Status = StatusUnknown
		info.ErrorMessage = "备用 WHOIS 未提供完整域名状态"
		if raw != "" && whoisClient != nil {
			parsed := whoisClient.ParseWhoisResponse(domain, raw)
			parsed.QueryMethod = "whois-fallback"
			parsed.WhoisRaw = raw
			parsed.LastChecked = info.LastChecked
			// 原始文本可以帮助识别已注册/未知，但不能绕过结构化
			// 状态完整性要求重新得到 available。
			if parsed.Status == StatusAvailable {
				parsed.Status = StatusUnknown
				parsed.ErrorMessage = "备用 WHOIS 状态字段不完整，无法确认可注册"
			}
			return parsed
		}
	}

	return info
}

func parseFallbackDate(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, format := range formats {
		if parsed, err := time.Parse(format, raw); err == nil {
			return &parsed
		}
	}
	return nil
}
