// Package whois 实现注册局 WHOIS(43 端口) 查询源。
//
// 解析逻辑与重构前保持一致：状态识别顺序、注册商/日期/NS 的正则集合、以及
// "判定为可注册但同时存在注册信息则纠正为已注册"的兜底校验都逐条保留。
package whois

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/httpx"
	"DomainHunter/internal/query"
	"DomainHunter/internal/query/detect"
	"DomainHunter/internal/registry"
)

const maxResponseBytes = 100 * 1024

// Provider 注册局 WHOIS 查询源
type Provider struct {
	mu      sync.RWMutex
	timeout time.Duration
}

// New 创建 WHOIS Provider
func New(timeout time.Duration) *Provider {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Provider{timeout: timeout}
}

// Name 实现 query.Provider
func (p *Provider) Name() string { return query.ProviderWhois }

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

// Supports WHOIS 是通用查询源，永远参与计划；具体后缀没有服务器时在
// Query 内返回 skipped，这样详情页仍能看到"为什么没有 WHOIS 结果"。
func (p *Provider) Supports(context.Context, query.Request) bool { return true }

// Query 执行一次 WHOIS 查询
func (p *Provider) Query(ctx context.Context, req query.Request) query.Result {
	started := time.Now()

	server, ok := registry.WhoisServerFor(req.Domain)
	if !ok {
		return query.Result{
			Domain:     req.Domain,
			Status:     domain.StatusSkipped,
			Provider:   p.Name(),
			Confidence: domain.ConfidenceLow,
			Note:       fmt.Sprintf("TLD %s 没有 WHOIS 服务器", req.TLD),
			StartedAt:  started,
			FinishedAt: time.Now(),
		}
	}

	response, err := p.dial(ctx, req.Domain, server.Server, server.Port)
	if err != nil {
		return p.fail(req.Domain, started, query.WrapError(p.Name(), fmt.Errorf("WHOIS连接失败: %w", err)))
	}

	result := Parse(req.Domain, response)
	result.Provider = p.Name()
	result.Raw = response
	result.StartedAt = started
	result.FinishedAt = time.Now()
	result.Latency = result.FinishedAt.Sub(started)

	// 响应过短且无法判定状态，多半是网络问题而不是"查不到"，
	// 必须走错误重试而不是把它当成结论缓存下来。
	if result.Status == domain.StatusUnknown && len(response) < 50 {
		return p.fail(req.Domain, started,
			query.NewError(query.KindUnknownResponse, p.Name(), "WHOIS响应过短，可能是网络问题"))
	}
	return result
}

func (p *Provider) fail(name string, started time.Time, err error) query.Result {
	finished := time.Now()
	return query.Result{
		Domain:     name,
		Status:     domain.StatusError,
		Provider:   p.Name(),
		Confidence: domain.ConfidenceLow,
		Err:        query.WrapError(p.Name(), err),
		StartedAt:  started,
		FinishedAt: finished,
		Latency:    finished.Sub(started),
	}
}

func (p *Provider) dial(ctx context.Context, name, server string, port int) (string, error) {
	if port <= 0 {
		port = 43
	}
	timeout := p.currentTimeout()
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := httpx.DialContext(dialCtx, "tcp", net.JoinHostPort(server, fmt.Sprintf("%d", port)))
	if err != nil {
		return "", err
	}
	defer conn.Close()

	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)

	if _, err := conn.Write([]byte(name + "\r\n")); err != nil {
		return "", fmt.Errorf("发送查询请求失败: %w", err)
	}

	response := make([]byte, 0, 4096)
	buffer := make([]byte, 4096)
	for {
		n, err := conn.Read(buffer)
		if n > 0 {
			response = append(response, buffer[:n]...)
		}
		if err != nil {
			if len(response) > 0 {
				break
			}
			return "", fmt.Errorf("读取响应失败: %w", err)
		}
		if len(response) > maxResponseBytes {
			break
		}
	}
	if len(response) == 0 {
		return "", fmt.Errorf("WHOIS查询返回空响应")
	}
	return string(response), nil
}

// Parse 解析 WHOIS 文本响应。
//
// 被 WHOIS.LS 与本地备用服务复用：它们拿到的同样是注册局的文本响应。
func Parse(name, response string) query.Result {
	result := query.Result{
		Domain:     name,
		Provider:   query.ProviderWhois,
		Raw:        response,
		Confidence: domain.ConfidenceMedium,
	}

	lower := strings.ToLower(response)
	result.Status = parseStatus(lower)
	result.Registrar = parseRegistrar(response)
	result.EPPStatuses = parseEPPStatuses(response)

	result.CreatedAt = parseDate(response, []string{"creation date", "created", "registered"})
	result.ExpiryAt = parseDate(response, []string{"expiry date", "expires", "expiration date", "registry expiry date"})
	result.UpdatedAt = parseDate(response, []string{"updated date", "last updated", "modified"})

	applyUnsupportedDataMessages(&result, extractTLD(name), response)
	result.NameServers = parseNameServers(response)

	// 判定为可注册但同时存在注册信息 —— 说明是误判，纠正为已注册。
	if result.Status == domain.StatusAvailable && hasRegistrationEvidence(result) {
		result.Status = domain.StatusRegistered
		result.Note = "响应中存在注册信息，可注册判定已纠正为已注册"
	}

	// 状态仍未知但存在注册信息时，才补判为已注册；
	// 不覆盖已经识别出的宽限期/赎回期/待删除等特殊状态。
	if result.Status == domain.StatusUnknown && hasRegistrationEvidence(result) {
		result.Status = domain.StatusRegistered
	}

	if result.Status == domain.StatusUnknown {
		result.Confidence = domain.ConfidenceLow
	}
	return result
}

func hasRegistrationEvidence(result query.Result) bool {
	registrar := strings.ToLower(strings.TrimSpace(result.Registrar))
	hasRegistrar := registrar != "" && !strings.Contains(registrar, "不支持")
	return hasRegistrar ||
		result.CreatedAt != nil ||
		result.ExpiryAt != nil ||
		len(result.NameServers) > 0
}

// parseStatus 从小写化后的响应文本判定状态
func parseStatus(response string) domain.Status {
	// 限流 / 封禁 / 服务不可用一律视为查询错误，绝不能当成结论。
	for _, marker := range []string{
		"number of allowed queries exceeded", "query limit", "rate limit", "too many requests",
		"blacklisted", "blocked", "access denied",
		"service unavailable", "temporarily unavailable", "server error",
	} {
		if strings.Contains(response, marker) {
			return domain.StatusError
		}
	}

	patterns := registry.Patterns()

	// 保留 / 禁止注册必须优先于"未找到"，否则
	// "Domain Cannot Be Registered" 会被误报成可注册。
	if matchAny(response, patterns.ReservedPatterns) {
		return domain.StatusUnknown
	}
	// 只有明确的"未找到记录"语义才能判定可注册。
	if matchAny(response, patterns.AvailablePatterns) {
		return domain.StatusAvailable
	}
	if matchAny(response, patterns.GracePatterns) {
		return domain.StatusGrace
	}
	if matchAny(response, patterns.RedemptionPatterns) {
		return domain.StatusRedemption
	}
	if matchAny(response, patterns.PendingDeletePatterns) {
		return domain.StatusPendingDelete
	}
	// Hold 仍然是主状态；转移锁定只作为附加 EPP 证据，主状态应为 registered。
	if matchAny(response, patterns.HoldPatterns) {
		return domain.StatusHold
	}
	if matchAny(response, patterns.ExpiredPatterns) && isExpired(response) {
		return domain.StatusGrace
	}
	if matchAny(response, patterns.RegisteredPatterns) {
		return domain.StatusRegistered
	}
	return domain.StatusUnknown
}

func matchAny(response string, patterns []string) bool {
	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}
		if strings.Contains(response, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

// ReservedText 暴露给其他 Provider 复用的保留域名判定
func ReservedText(raw string, fields ...string) bool { return detect.ContainsReserved(raw, fields...) }

var registrarPatterns = compileAll([]string{
	`(?i)registrar:\s*(.+)`,
	`(?i)registrar organization:\s*(.+)`,
	`(?i)sponsoring registrar:\s*(.+)`,
	`(?i)Registrar Name:\s*(.+)`,
	`(?i)Organization:\s*(.+)`,
	// .jp（日文与英文）
	`(?i)\[Name\]\s*(.+)`,
	`(?i)\[登録者名\]\s*(.+)`,
	// .kr
	`(?i)등록대행자\s*:\s*(.+)`,
	`(?i)Authorized Agency\s*:\s*(.+)`,
	// .ax / .fi（字段名后跟多个点）
	`(?i)registrar\.+:\s*(.+)`,
	// .bn / .rs（独立一行）
	`(?i)^Registrar:\s*(.+)`,
	// .tr
	`(?i)Organization Name:\s*(.+)`,
	// .kz
	`(?i)Current Registar:\s*(.+)`,
	`(?i)Registar created:\s*(.+)`,
	// .tg
	`(?i)Registrar:\.+(.+)`,
	// .lu
	`(?i)registrar-name:\s*(.+)`,
	// .lv（分区块）
	`(?i)\[Registrar\][\s\S]*?Name:\s*(.+)`,
})

func parseRegistrar(response string) string {
	for _, re := range registrarPatterns {
		matches := re.FindStringSubmatch(response)
		if len(matches) > 1 {
			registrar := strings.TrimSpace(matches[1])
			if idx := strings.Index(registrar, "("); idx != -1 {
				registrar = strings.TrimSpace(registrar[:idx])
			}
			return registrar
		}
	}
	return ""
}

var eppStatusRe = regexp.MustCompile(`(?i)(?:domain\s+)?status:\s*([^\r\n]+)`)

// parseEPPStatuses 收集响应里出现的 EPP 状态，作为详情页的补充证据。
// 它不参与状态判定，只是让用户能看到 "clientTransferProhibited" 这类原始信息。
func parseEPPStatuses(response string) []string {
	matches := eppStatusRe.FindAllStringSubmatch(response, -1)
	seen := map[string]bool{}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		value := strings.TrimSpace(m[1])
		if idx := strings.Index(value, "https://"); idx > 0 {
			value = strings.TrimSpace(value[:idx])
		}
		if idx := strings.Index(value, "http://"); idx > 0 {
			value = strings.TrimSpace(value[:idx])
		}
		if value == "" || seen[strings.ToLower(value)] {
			continue
		}
		seen[strings.ToLower(value)] = true
		out = append(out, value)
		if len(out) >= 12 {
			break
		}
	}
	return out
}

var spaceRe = regexp.MustCompile(`\s+`)
var tzSuffixRe = regexp.MustCompile(`\s*\([^)]+\)\s*$`)
var ordinalRe = regexp.MustCompile(`(\d+)(st|nd|rd|th)\b`)

func parseDate(response string, keywords []string) *time.Time {
	for _, keyword := range keywords {
		re := regexp.MustCompile(fmt.Sprintf(`(?i)%s:\s*([^\r\n]+)`, regexp.QuoteMeta(keyword)))
		if matches := re.FindStringSubmatch(response); len(matches) > 1 {
			if date := ParseDateTime(strings.TrimSpace(matches[1])); date != nil {
				return date
			}
		}
	}
	for _, re := range specialDatePatterns(keywords) {
		if matches := re.FindStringSubmatch(response); len(matches) > 1 {
			if date := ParseDateTime(strings.TrimSpace(matches[1])); date != nil {
				return date
			}
		}
	}
	return nil
}

// ParseDateTime 解析各注册局五花八门的日期写法，无法识别时返回 nil。
// 绝不猜测或补齐日期 —— 例如 .im 公开响应没有创建日期时必须保持为空。
func ParseDateTime(dateStr string) *time.Time {
	dateStr = strings.TrimSpace(dateStr)
	if dateStr == "" {
		return nil
	}
	dateStr = spaceRe.ReplaceAllString(dateStr, " ")
	dateStr = tzSuffixRe.ReplaceAllString(dateStr, "")
	if strings.Contains(dateStr, " at ") {
		dateStr = strings.ReplaceAll(dateStr, " at ", " ")
		dateStr = ordinalRe.ReplaceAllString(dateStr, "$1")
	}

	for _, format := range dateFormats {
		if date, err := time.Parse(format, dateStr); err == nil {
			return &date
		}
	}
	return nil
}

var dateFormats = []string{
	"2006-01-02T15:04:05Z",
	"2006-01-02T15:04:05.000Z",
	"2006-01-02T15:04:05+07:00",
	"2006-01-02T15:04:05-07:00",
	"2006-01-02 15:04:05",
	"2006-01-02",
	"2006/01/02",
	"2006/01/02 15:04:05",
	"2006. 01. 02.",
	"2006. 01. 02",
	"02-01-2006",
	"2-1-2006",
	"02-1-2006",
	"2-01-2006",
	"02-Jan-2006",
	"January 02 2006",
	"Jan 02 2006",
	"02/01/2006",
	"02/01/2006 15:04:05",
	"01/02/2006",
	"2006.01.02",
	"2006.1.2",
	"02.01.2006",
	"2006-01-02 15:04:05 MST",
	"2006-01-02 15:04:05 -0700",
	"2006-01-02 15:04:05 -07:00",
	"2006-01-02 15:04:05 +07:00",
	"January 2 2006",
	"Jan 2 2006",
	"Mon Jan 2 15:04:05 2006",
	"2006-01-02 15:04:05 (MST+0:00)",
	"2006-01-02 15:04:05 (GMT+0:00)",
	"2006.01.02 15:04:05",
	"02-Jan-2006 15:04:05 UTC",
	"2006-01-02T15:04:05.999999Z",
	"2006-Jan-02",
	"2006-Jan-02.",
	"Monday 2nd Jan 2006",
	"2nd January 2006",
	"2nd January 2006 at 15:04:05.000",
	"02-01-2006 15:04:05 GMT+1",
	"02-Jan-2006 00:00:00",
	"02-Jan-2006 15:04:05",
	"2.1.2006 15:04:05",
	"02.01.2006 15:04:05",
	"2006-01-02 15:04:05.999999",
	"2006-01-02T15:04:05.999999+00:00",
	"20060102 15:04:05",
	"2 January 2006 15:04:05.000",
	"02 January 2006 15:04:05.000",
}

func specialDatePatterns(keywords []string) []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, keyword := range keywords {
		switch strings.ToLower(keyword) {
		case "creation date", "created", "registered":
			out = append(out, createdPatterns...)
		case "expiry date", "expires", "expiration date", "registry expiry date":
			out = append(out, expiryPatterns...)
		case "updated date", "last updated", "modified":
			out = append(out, updatedPatterns...)
		}
	}
	return out
}

var createdPatterns = compileAll([]string{
	`(?i)created:\s*([^\r\n]+)`,
	`(?i)Registration Time:\s*([^\r\n]+)`,
	`(?i)Domain Name Commencement Date:\s*([^\r\n]+)`,
	`(?i)\[登録年月日\]\s*([^\r\n]+)`,
	`(?i)등록일\s*:\s*([^\r\n]+)`,
	`(?i)Registered Date\s*:\s*([^\r\n]+)`,
	`(?i)Changed:\s*([^\r\n]+)`,
	`(?i)Last Modified:\s*([^\r\n]+)`,
	`(?i)created\.+:\s*([^\r\n]+)`,
	`(?i)Creation Date:\s+([^\r\n]+)`,
	`(?i)Creation date:\s*([^\r\n]+)`,
	`(?i)registered:\s*([^\r\n]+)`,
	`(?i)Record created:\s*([^\r\n]+)`,
	`(?i)Domain created:\s*([^\r\n]+)`,
	`(?i)Created On:\s*([^\r\n]+)`,
	`(?i)Registration date:\s*([^\r\n]+)`,
	`(?i)Data de Registo:\s*([^\r\n]+)`,
	`(?i)Registered On:\s*([^\r\n]+)`,
	`(?i)Created:\s*([^\r\n]+)`,
	`(?i)Date de création:\s*([^\r\n]+)`,
	`(?i)Activation:\.+([^\r\n]+)`,
	`(?i)Created on\.+:\s*([^\r\n]+)`,
	`(?i)Registered on\s+([^\r\n]+)`,
	`(?i)assigned:\s*([^\r\n]+)`,
	`(?i)Creation date\.+:\s*([^\r\n]+)`,
	`(?i)Record created on\s+([^\r\n]+)`,
})

var expiryPatterns = compileAll([]string{
	`(?i)paid-till:\s*([^\r\n]+)`,
	`(?i)free-date:\s*([^\r\n]+)`,
	`(?i)Expiration Time:\s*([^\r\n]+)`,
	`(?i)Expiry Date:\s*([^\r\n]+)`,
	`(?i)\[有効期限\]\s*([^\r\n]+)`,
	`(?i)사용 종료일\s*:\s*([^\r\n]+)`,
	`(?i)Expiration Date\s*:\s*([^\r\n]+)`,
	`(?i)expires\.+:\s*([^\r\n]+)`,
	`(?i)Expiration Date:\s+([^\r\n]+)`,
	`(?i)Expiration date:\s*([^\r\n]+)`,
	`(?i)expire:\s*([^\r\n]+)`,
	`(?i)expires:\s*([^\r\n]+)`,
	`(?i)Expire Date:\s*([^\r\n]+)`,
	`(?i)Record expires on:\s*([^\r\n]+)`,
	`(?i)renewal date:\s*([^\r\n]+)`,
	`(?i)Data de Expiração:\s*([^\r\n]+)`,
	`(?i)Expires On:\s*([^\r\n]+)`,
	`(?i)Registry Expiry Date:\s*([^\r\n]+)`,
	`(?i)Valid Until:\s*([^\r\n]+)`,
	`(?i)Date d'expiration:\s*([^\r\n]+)`,
	`(?i)Expiration:\.+([^\r\n]+)`,
	`(?i)Expiry:\s*([^\r\n]+)`,
	`(?i)Expires on\.+:\s*([^\r\n]+)`,
	`(?i)Renewal date:\s*([^\r\n]+)`,
	`(?i)validity:\s*([^\r\n]+)`,
	`(?i)Record expires on\s+([^\r\n]+)`,
})

var updatedPatterns = compileAll([]string{
	`(?i)\[最終更新\]\s*([^\r\n]+)`,
	`(?i)최근 정보 변경일\s*:\s*([^\r\n]+)`,
	`(?i)Last Updated Date\s*:\s*([^\r\n]+)`,
	`(?i)Changed:\s*([^\r\n]+)`,
	`(?i)Last Modified:\s*([^\r\n]+)`,
	`(?i)modified\.+:\s*([^\r\n]+)`,
	`(?i)Modified Date:\s+([^\r\n]+)`,
	`(?i)changed:\s*([^\r\n]+)`,
	`(?i)Last Update:\s*([^\r\n]+)`,
	`(?i)Last modified\s*:\s*([^\r\n]+)`,
	`(?i)Last Updated On:\s*([^\r\n]+)`,
	`(?i)Modification date:\s*([^\r\n]+)`,
	`(?i)Updated:\s*([^\r\n]+)`,
	`(?i)Dernière modification:\s*([^\r\n]+)`,
})

var nameServerPatterns = compileAll([]string{
	`(?i)name server:\s*([^\r\n]+)`,
	`(?i)nameserver:\s*([^\r\n]+)`,
	`(?i)nserver:\s*([^\r\n]+)`,
	`(?i)dns:\s*([^\r\n]+)`,
	`(?i)Name Servers:\s*\n\s+([^\r\n]+)`,
	`(?i)nserver\.+:\s*([^\r\n]+)`,
	`(?i)(?:Primary|Secondary) server\.+:\s*([^\r\n]+)`,
	`(?i)Domain name servers:[\s\S]*?([a-z0-9\-\.]+\.(?:com|net|org|cloudflare\.com|ns\.cloudflare\.com))`,
	`(?i)^DNS:\s*([^\r\n]+)`,
	`(?i)Name Server \(DB\):\.+([^\r\n]+)`,
	`(?i)Name servers:\s*\n\s+([^\r\n]+)`,
	`(?i)^nserver:\s*([^\r\n]+)`,
	`(?i)\[Nservers\][\s\S]*?Nserver:\s*([^\r\n]+)`,
})

func parseNameServers(response string) []string {
	var nameServers []string
	seen := map[string]bool{}
	for _, re := range nameServerPatterns {
		for _, match := range re.FindAllStringSubmatch(response, -1) {
			if len(match) < 2 {
				continue
			}
			ns := strings.TrimSpace(strings.ToLower(match[1]))
			ns = strings.Split(ns, " ")[0]
			ns = strings.Split(ns, "\t")[0]
			ns = strings.TrimSpace(strings.TrimSuffix(ns, "[ok]"))
			ns = strings.TrimSuffix(ns, ".")
			if ns != "" && !seen[ns] && strings.Contains(ns, ".") {
				nameServers = append(nameServers, ns)
				seen[ns] = true
			}
		}
	}
	return nameServers
}

var expiryCheckPatterns = compileAll([]string{
	`(?i)expiry date:\s*([^\r\n]+)`,
	`(?i)expires:\s*([^\r\n]+)`,
	`(?i)expiration date:\s*([^\r\n]+)`,
	`(?i)registry expiry date:\s*([^\r\n]+)`,
})

func isExpired(response string) bool {
	for _, re := range expiryCheckPatterns {
		if matches := re.FindStringSubmatch(response); len(matches) > 1 {
			if date := ParseDateTime(strings.TrimSpace(matches[1])); date != nil {
				return date.Before(time.Now())
			}
		}
	}
	return false
}

func extractTLD(name string) string {
	parts := strings.Split(name, ".")
	if len(parts) >= 2 {
		return strings.ToLower(parts[len(parts)-1])
	}
	return ""
}

// applyUnsupportedDataMessages 对确实不提供注册商信息的后缀补充提示文案，
// 避免前端把"注册局不公开"误显示成"查询失败"。
func applyUnsupportedDataMessages(result *query.Result, tld, response string) {
	if result.Status == domain.StatusAvailable || result.Status == domain.StatusError {
		return
	}
	if result.Registrar != "" {
		return
	}
	switch tld {
	case "de":
		result.Registrar = "该后缀不支持注册商信息"
	case "jp":
		if !strings.Contains(response, "[Name]") && !strings.Contains(response, "GMO") {
			result.Registrar = "该后缀不支持注册商信息"
		}
	default:
		if !hasRegistrarInfo(response, tld) {
			result.Registrar = "该后缀不支持注册商信息"
		}
	}
}

func hasRegistrarInfo(response, tld string) bool {
	switch tld {
	case "de":
		return false
	case "cn", "com.cn", "net.cn", "org.cn":
		return strings.Contains(response, "Sponsoring Registrar") ||
			strings.Contains(response, "sponsoring registrar")
	case "jp":
		return strings.Contains(response, "[Name]") || strings.Contains(response, "GMO")
	case "kr":
		return strings.Contains(response, "등록대행자") || strings.Contains(response, "Authorized Agency")
	case "hk", "au":
		return strings.Contains(response, "Registrar Name")
	case "ru":
		return strings.Contains(response, "registrar:")
	default:
		return true
	}
}

func compileAll(patterns []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		out = append(out, regexp.MustCompile(p))
	}
	return out
}
