package core

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"DomainHunter/config"
	"DomainHunter/logger"
)

// DomainChecker 域名检查器
type DomainChecker struct {
	mu             sync.RWMutex
	whoisClient    *WhoisClient
	rdapClient     *RDAPClient
	fallbackClient *WhoisFallbackClient
	config         *config.Config
}

// NewDomainChecker 创建新的域名检查器
func NewDomainChecker(cfg *config.Config) *DomainChecker {
	return &DomainChecker{
		whoisClient:    NewWhoisClient(cfg.Monitor.Timeout),
		rdapClient:     NewRDAPClient(cfg.Monitor.Timeout),
		fallbackClient: NewWhoisFallbackClient(cfg.Monitor.Timeout),
		config:         cfg,
	}
}

// UpdateConfig 更新配置（用于热重载）
func (d *DomainChecker) UpdateConfig(cfg *config.Config) {
	if cfg == nil {
		return
	}

	// 查询请求可能正在并发执行，替换无状态 client 而不是直接修改其
	// timeout 字段，避免 data race 和 http.Client 配置竞态。
	d.mu.Lock()
	d.config = cfg
	d.whoisClient = NewWhoisClient(cfg.Monitor.Timeout)
	d.rdapClient = NewRDAPClient(cfg.Monitor.Timeout)
	fallbackClient := d.fallbackClient
	d.mu.Unlock()
	if fallbackClient != nil {
		fallbackClient.UpdateTimeout(cfg.Monitor.Timeout)
	}
}

func (d *DomainChecker) fallbackEnabledForTLD(tld string) bool {
	d.mu.RLock()
	client := d.fallbackClient
	d.mu.RUnlock()
	return client != nil && client.EnabledForTLD(tld)
}

// CheckDomain 检查单个域名
func (d *DomainChecker) CheckDomain(domain string) *DomainInfo {
	domain = strings.ToLower(strings.TrimSpace(domain))

	// 获取TLD（最长后缀匹配）
	tld := config.FindBestTLD(domain)
	if tld == "" {
		return &DomainInfo{
			Name:         domain,
			Status:       StatusSkipped,
			ErrorMessage: "没有匹配的查询配置，已跳过",
			LastChecked:  time.Now(),
		}
	}

	// 首先尝试RDAP查询。对已配置备用服务的后缀，即使 RDAP 返回了
	// “可注册”，也要进入复核链路，避免注册局的保留策略页面被误判。
	rdapInfo := d.tryRDAPQuery(domain, tld)
	validateNativeAvailability := rdapInfo != nil && rdapInfo.Status == StatusAvailable &&
		d.fallbackEnabledForTLD(tld)
	if rdapInfo != nil && isDefinitiveStatus(rdapInfo.Status) && !validateNativeAvailability {
		return rdapInfo
	}

	// RDAP失败，尝试WHOIS查询
	whoisInfo := d.tryWhoisQuery(domain, tld)
	validateNativeAvailability = validateNativeAvailability || (whoisInfo != nil && whoisInfo.Status == StatusAvailable &&
		d.fallbackEnabledForTLD(tld))
	if whoisInfo != nil && isDefinitiveStatus(whoisInfo.Status) && !validateNativeAvailability {
		return whoisInfo
	}

	// 对明确配置的TLD，原生查询失败或给出“可注册”时调用外部
	// WHOIS 服务复核。备用服务失败时不保留未经复核的 available，
	// 避免把注册局策略页、超时或错误响应误判为可注册。
	if fallbackInfo := d.tryWhoisFallbackQuery(domain, tld); fallbackInfo != nil {
		// 备用源既然已明确启用，其错误应进入重试/错误路径；不能被
		// 原生 unknown 吞掉，否则短暂的备用服务故障会被长期缓存。
		if fallbackInfo.Status == StatusError {
			return fallbackInfo
		}
		if isDefinitiveStatus(fallbackInfo.Status) || fallbackInfo.Status == StatusUnknown || validateNativeAvailability {
			return fallbackInfo
		}
	}

	if whoisInfo != nil && whoisInfo.Status == StatusUnknown {
		return whoisInfo
	}
	if rdapInfo != nil && rdapInfo.Status == StatusUnknown {
		return rdapInfo
	}
	if (rdapInfo == nil || rdapInfo.Status == StatusSkipped) && (whoisInfo == nil || whoisInfo.Status == StatusSkipped) {
		return &DomainInfo{
			Name:         domain,
			Status:       StatusSkipped,
			ErrorMessage: "没有可用的 RDAP/WHOIS 查询源，已跳过",
			LastChecked:  time.Now(),
		}
	}

	// 都失败了
	errorMessage := "RDAP 和 WHOIS 查询失败"
	if whoisInfo != nil && whoisInfo.ErrorMessage != "" {
		errorMessage = whoisInfo.ErrorMessage
	} else if rdapInfo != nil && rdapInfo.ErrorMessage != "" {
		errorMessage = rdapInfo.ErrorMessage
	}
	return &DomainInfo{
		Name:         domain,
		Status:       StatusError,
		ErrorMessage: errorMessage,
		LastChecked:  time.Now(),
	}
}

func isDefinitiveStatus(status DomainStatus) bool {
	switch status {
	case StatusAvailable, StatusRegistered, StatusGrace, StatusRedemption, StatusPendingDelete, StatusExpired, StatusTransferLocked, StatusHold:
		return true
	default:
		return false
	}
}

// ValidateDomain 验证域名格式
func (dc *DomainChecker) ValidateDomain(domain string) error {
	domain = strings.TrimSpace(domain)

	if domain == "" {
		return fmt.Errorf("域名不能为空")
	}

	// 基本长度检查
	if len(domain) > 253 {
		return fmt.Errorf("域名长度不能超过253个字符")
	}

	// 检查是否包含无效字符
	for _, char := range domain {
		if !((char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '.' || char == '-') {
			return fmt.Errorf("域名包含无效字符: %c", char)
		}
	}

	// 分割域名部分
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return fmt.Errorf("域名必须包含至少一个点")
	}

	// 检查每个部分
	for i, part := range parts {
		if part == "" {
			return fmt.Errorf("域名部分不能为空")
		}

		if len(part) > 63 {
			return fmt.Errorf("域名部分长度不能超过63个字符")
		}

		// 不能以连字符开始或结束
		if strings.HasPrefix(part, "-") || strings.HasSuffix(part, "-") {
			return fmt.Errorf("域名部分不能以连字符开始或结束: %s", part)
		}

		// 最后一部分（TLD）不能全是数字
		if i == len(parts)-1 {
			allDigits := true
			for _, char := range part {
				if char < '0' || char > '9' {
					allDigits = false
					break
				}
			}
			if allDigits {
				return fmt.Errorf("顶级域名不能全是数字")
			}
		}
	}

	return nil
}

// tryRDAPQuery 尝试RDAP查询
func (d *DomainChecker) tryRDAPQuery(domain, tld string) *DomainInfo {
	d.mu.RLock()
	client := d.rdapClient
	d.mu.RUnlock()
	if client == nil {
		return &DomainInfo{
			Name:         domain,
			Status:       StatusError,
			ErrorMessage: "RDAP客户端未初始化",
			LastChecked:  time.Now(),
		}
	}

	server, exists := config.GetRDAPServerByTLD(domain)
	if !exists {
		return &DomainInfo{
			Name:         domain,
			Status:       StatusSkipped,
			ErrorMessage: fmt.Sprintf("TLD %s 没有 RDAP 服务器，等待 WHOIS 或备用源", tld),
			LastChecked:  time.Now(),
		}
	}

	// 使用新的QueryRDAPWithRaw方法同时获取解析后的数据和原始JSON
	rdapResp, rawJSON, err := client.QueryRDAPWithRaw(domain, server.Server)
	if err != nil {
		return &DomainInfo{
			Name:         domain,
			Status:       StatusError,
			ErrorMessage: fmt.Sprintf("RDAP查询失败: %v", err),
			LastChecked:  time.Now(),
		}
	}

	// 解析RDAP响应并传入原始JSON数据
	return client.ParseRDAPResponse(domain, rdapResp, rawJSON)
}

// tryWhoisQuery 尝试WHOIS查询（不带重试，重试由外层CheckDomainsWithCallback处理）
func (d *DomainChecker) tryWhoisQuery(domain, tld string) *DomainInfo {
	d.mu.RLock()
	client := d.whoisClient
	d.mu.RUnlock()
	if client == nil {
		return &DomainInfo{
			Name:         domain,
			Status:       StatusError,
			ErrorMessage: "WHOIS客户端未初始化",
			LastChecked:  time.Now(),
		}
	}

	server, exists := config.GetWhoisServerByTLD(domain)
	if !exists {
		logger.Debug("Domain checker: no WHOIS server found for domain=%s tld=%s", domain, tld)
		return &DomainInfo{
			Name:         domain,
			Status:       StatusSkipped,
			ErrorMessage: fmt.Sprintf("TLD %s 没有 WHOIS 服务器", tld),
			LastChecked:  time.Now(),
		}
	}

	// 单次WHOIS查询，不在此处重试
	response, err := client.QueryWhois(domain, server.Server, server.Port)
	if err != nil {
		logger.Debug("Domain checker: WHOIS query failed for domain=%s err=%v", domain, err)
		return &DomainInfo{
			Name:         domain,
			Status:       StatusError,
			ErrorMessage: fmt.Sprintf("WHOIS连接失败: %v", err),
			LastChecked:  time.Now(),
		}
	}

	// WHOIS查询成功
	result := client.ParseWhoisResponse(domain, response)
	// 保存原始WHOIS数据
	result.WhoisRaw = response
	logger.Debug("Domain checker: WHOIS parsed result for domain=%s status=%s", domain, result.Status)

	// 如果结果是unknown且响应很短，可能是网络问题
	if result.Status == StatusUnknown && len(response) < 50 {
		logger.Debug("Domain checker: WHOIS got unknown status with short response for domain=%s", domain)
		return &DomainInfo{
			Name:         domain,
			Status:       StatusError,
			ErrorMessage: "WHOIS响应过短，可能是网络问题",
			LastChecked:  time.Now(),
		}
	}

	return result
}

// GetSupportedTLDs 获取支持的TLD列表
func (d *DomainChecker) GetSupportedTLDs() []string {
	return config.GetSupportedTLDs()
}
