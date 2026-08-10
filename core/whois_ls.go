package core

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

	"DomainHunter/logger"
)

// WhoisLSClient uses the public WHOIS.LS JSON gateway for explicitly enabled
// TLDs. It is kept separate from the local structured fallback because the
// two providers expose different fields: WHOIS.LS is useful for .im expiry
// dates, while the local service supplies structured .do status and dates.
type WhoisLSClient struct {
	mu      sync.RWMutex
	baseURL string
	tlds    map[string]struct{}
	timeout time.Duration
}

type whoisLSResponse struct {
	Error json.RawMessage `json:"error"`
	Data  string          `json:"data"`
}

// NewWhoisLSClient creates a client configured from environment variables.
// DOMAINHUNTER_WHOIS_LS_URL is empty by default; the deployment explicitly
// enables it for .im. Legacy PUFF_WHOIS_LS_* names are accepted as well.
func NewWhoisLSClient(timeout time.Duration) *WhoisLSClient {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}

	if configured := firstEnv("DOMAINHUNTER_WHOIS_LS_TIMEOUT", "PUFF_WHOIS_LS_TIMEOUT"); configured != "" {
		if seconds, err := time.ParseDuration(configured); err == nil && seconds > 0 && seconds <= 120*time.Second {
			timeout = seconds
		} else if seconds, err := time.ParseDuration(configured + "s"); err == nil && seconds > 0 && seconds <= 120*time.Second {
			timeout = seconds
		}
	}

	return &WhoisLSClient{
		baseURL: strings.TrimSpace(firstEnv("DOMAINHUNTER_WHOIS_LS_URL", "PUFF_WHOIS_LS_URL")),
		tlds:    parseFallbackTLDs(firstEnv("DOMAINHUNTER_WHOIS_LS_TLDS", "PUFF_WHOIS_LS_TLDS")),
		timeout: timeout,
	}
}

func (c *WhoisLSClient) UpdateTimeout(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	c.mu.Lock()
	c.timeout = timeout
	c.mu.Unlock()
}

func (c *WhoisLSClient) EnabledForTLD(tld string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.baseURL == "" {
		return false
	}
	_, ok := c.tlds[strings.ToLower(strings.Trim(strings.TrimSpace(tld), "."))]
	return ok
}

func (c *WhoisLSClient) snapshot() (string, time.Duration) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL, c.timeout
}

func (c *WhoisLSClient) Query(domain string) (*whoisLSResponse, error) {
	baseURL, timeout := c.snapshot()
	endpoint, err := whoisLSEndpoint(baseURL, domain)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("创建 WHOIS.LS 请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	response, err := GetProxyHTTPClient(timeout).Do(req)
	if err != nil {
		return nil, fmt.Errorf("WHOIS.LS 请求失败: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("读取 WHOIS.LS 响应失败: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("WHOIS.LS 返回 HTTP %d", response.StatusCode)
	}

	var result whoisLSResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析 WHOIS.LS 响应失败: %w", err)
	}
	if jsonRawBool(result.Error) {
		message := strings.TrimSpace(result.Data)
		if message == "" {
			message = "WHOIS.LS 查询失败"
		}
		return nil, fmt.Errorf("%s", message)
	}
	if strings.TrimSpace(result.Data) == "" {
		return nil, fmt.Errorf("WHOIS.LS 响应缺少 data")
	}
	return &result, nil
}

func whoisLSEndpoint(rawBaseURL, domain string) (string, error) {
	if strings.TrimSpace(rawBaseURL) == "" {
		return "", fmt.Errorf("WHOIS.LS URL 未配置")
	}

	parsed, err := url.Parse(rawBaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("WHOIS.LS URL 无效")
	}

	path := strings.TrimRight(parsed.Path, "/")
	if path == "" {
		path = "/json"
	}
	parsed.Path = path + "/" + url.PathEscape(strings.TrimSpace(domain))
	parsed.RawPath = ""
	return parsed.String(), nil
}

func jsonRawBool(raw json.RawMessage) bool {
	value := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	return strings.EqualFold(value, "true") || value == "1"
}

func (d *DomainChecker) whoisLSEnabledForTLD(tld string) bool {
	d.mu.RLock()
	client := d.whoisLSClient
	d.mu.RUnlock()
	return client != nil && client.EnabledForTLD(tld)
}

func (d *DomainChecker) tryWhoisLSQuery(domain, tld string) *DomainInfo {
	d.mu.RLock()
	client := d.whoisLSClient
	whoisClient := d.whoisClient
	d.mu.RUnlock()
	if client == nil || !client.EnabledForTLD(tld) {
		return nil
	}
	if whoisClient == nil {
		return &DomainInfo{
			Name:         domain,
			Status:       StatusError,
			QueryMethod:  "whois-ls",
			ErrorMessage: "WHOIS 客户端未初始化，无法解析 WHOIS.LS 响应",
			LastChecked:  time.Now(),
		}
	}

	response, err := client.Query(domain)
	if err != nil {
		logger.Warn("WHOIS.LS 查询失败 domain=%s tld=%s err=%v", domain, tld, err)
		return &DomainInfo{
			Name:         domain,
			Status:       StatusError,
			QueryMethod:  "whois-ls",
			ErrorMessage: err.Error(),
			LastChecked:  time.Now(),
		}
	}

	raw := strings.TrimSpace(response.Data)
	info := whoisClient.ParseWhoisResponse(domain, raw)
	info.QueryMethod = "whois-ls"
	info.WhoisRaw = raw
	info.LastChecked = time.Now()

	// WHOIS.LS returns the registry's text response. For .im, an explicit
	// "was not found" response is enough to mark available, but no vague or
	// empty response may become available.
	if whoisLSNotFoundResponse(raw) && !hasWhoisRegistrationEvidence(info) {
		info.Status = StatusAvailable
		info.ErrorMessage = ""
	}
	if info.Status == StatusAvailable && hasWhoisRegistrationEvidence(info) {
		info.Status = StatusRegistered
	}
	return info
}

func whoisLSNotFoundResponse(raw string) bool {
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

func hasWhoisRegistrationEvidence(info *DomainInfo) bool {
	if info == nil {
		return false
	}
	registrar := strings.ToLower(strings.TrimSpace(info.Registrar))
	return (registrar != "" && !strings.Contains(registrar, "不支持")) ||
		info.CreatedDate != nil ||
		info.ExpiryDate != nil ||
		info.UpdatedDate != nil ||
		len(info.NameServers) > 0
}
