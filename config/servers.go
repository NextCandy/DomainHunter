package config

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// WhoisServer WHOIS服务器配置
type WhoisServer struct {
	Server string `json:"server"` // 服务器地址
	Port   int    `json:"port"`   // 端口
}

// RDAPServer RDAP服务器配置
type RDAPServer struct {
	Server string `json:"server"` // 服务器地址
}

// TLDServers TLD服务器配置
type TLDServers struct {
	Whois WhoisServer `json:"whois"`
	RDAP  RDAPServer  `json:"rdap"`
}

// DetectionPatterns 检测模式配置
type DetectionPatterns struct {
	AvailablePatterns     []string `json:"available_patterns"`
	ReservedPatterns      []string `json:"reserved_patterns"`
	RedemptionPatterns    []string `json:"redemption_patterns"`
	PendingDeletePatterns []string `json:"pending_delete_patterns"`
	ExpiredPatterns       []string `json:"expired_patterns"`
	HoldPatterns          []string `json:"hold_patterns"`
	TransferLockPatterns  []string `json:"transfer_lock_patterns"`
	RegisteredPatterns    []string `json:"registered_patterns"`
	GracePatterns         []string `json:"grace_patterns"`
}

var (
	serversConfig  map[string]TLDServers
	patternsConfig DetectionPatterns
	configMutex    sync.RWMutex
	configLoadMu   sync.Mutex
	configLoaded   bool
)

const defaultRDAPBootstrapURL = "https://data.iana.org/rdap/dns.json"

type rdapBootstrapFile struct {
	Services [][]json.RawMessage `json:"services"`
}

// LoadServerConfigs 加载服务器配置（从嵌入的文件读取）
func LoadServerConfigs() error {
	return loadServerConfigs(false)
}

// ReloadServerConfigs 强制重新读取内置配置并刷新 IANA bootstrap。
func ReloadServerConfigs() error {
	return loadServerConfigs(true)
}

func loadServerConfigs(force bool) error {
	configLoadMu.Lock()
	defer configLoadMu.Unlock()

	configMutex.RLock()
	loaded := configLoaded
	configMutex.RUnlock()
	if loaded && !force {
		return nil
	}

	// 读取嵌入的 servers.json 文件
	sData, err := GetEmbeddedFile("servers.json")
	if err != nil {
		return fmt.Errorf("读取 servers.json 失败: %v", err)
	}
	var loadedServers map[string]TLDServers
	if err := json.Unmarshal(sData, &loadedServers); err != nil {
		return fmt.Errorf("解析 servers.json 失败: %v", err)
	}

	// 读取嵌入的 detection_patterns.json 文件
	pData, err := GetEmbeddedFile("detection_patterns.json")
	if err != nil {
		return fmt.Errorf("读取 detection_patterns.json 失败: %v", err)
	}
	var loadedPatterns DetectionPatterns
	if err := json.Unmarshal(pData, &loadedPatterns); err != nil {
		return fmt.Errorf("解析 detection_patterns.json 失败: %v", err)
	}

	if len(loadedPatterns.AvailablePatterns) == 0 {
		return fmt.Errorf("detection_patterns.json 中 available_patterns 为空，配置无效")
	}

	// 静态 servers.json 作为离线兜底；启动时优先合并 IANA 最新 RDAP
	// bootstrap。IANA 不可达时不阻塞服务启动，现有静态映射仍可工作。
	if err := mergeRDAPBootstrap(loadedServers); err != nil {
		log.Printf("Config: IANA RDAP bootstrap unavailable, using static mappings: %v", err)
	}

	configMutex.Lock()
	serversConfig = loadedServers
	patternsConfig = loadedPatterns
	configLoaded = true
	configMutex.Unlock()
	return nil
}

func mergeRDAPBootstrap(servers map[string]TLDServers) error {
	endpoint := strings.TrimSpace(os.Getenv("DOMAINHUNTER_RDAP_BOOTSTRAP_URL"))
	if endpoint == "" {
		endpoint = defaultRDAPBootstrapURL
	}

	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Get(endpoint)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	var bootstrap rdapBootstrapFile
	if err := json.Unmarshal(body, &bootstrap); err != nil {
		return err
	}

	merged := 0
	for _, service := range bootstrap.Services {
		if len(service) != 2 {
			continue
		}
		var tlds []string
		var urls []string
		if err := json.Unmarshal(service[0], &tlds); err != nil {
			continue
		}
		if err := json.Unmarshal(service[1], &urls); err != nil || len(urls) == 0 {
			continue
		}
		baseURL := strings.TrimRight(strings.TrimSpace(urls[0]), "/")
		if baseURL == "" {
			continue
		}
		for _, rawTLD := range tlds {
			tld := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(rawTLD), "."))
			if tld == "" {
				continue
			}
			entry := servers[tld]
			entry.RDAP.Server = baseURL
			servers[tld] = entry
			merged++
		}
	}

	if merged == 0 {
		return fmt.Errorf("bootstrap contains no usable services")
	}
	log.Printf("Config: merged %d IANA RDAP TLD mappings", merged)
	return nil
}

func isConfigLoaded() bool {
	configMutex.RLock()
	defer configMutex.RUnlock()
	return configLoaded
}

// GetWhoisServerByTLD 根据TLD获取WHOIS服务器
func GetWhoisServerByTLD(domain string) (WhoisServer, bool) {
	if !isConfigLoaded() {
		if err := LoadServerConfigs(); err != nil {
			log.Printf("Config: failed to load server configs: %v", err)
			return WhoisServer{}, false
		}
	}

	configMutex.RLock()
	defer configMutex.RUnlock()

	key := findBestTLD(domain)
	if key == "" {
		log.Printf("Config: no TLD match found for domain=%s", domain)
		return WhoisServer{}, false
	}

	server := serversConfig[key].Whois
	// WHOIS服务器已找到

	if server.Server == "" {
		log.Printf("Config: empty WHOIS server for domain=%s tld=%s", domain, key)
		return WhoisServer{}, false
	}

	return server, true
}

// GetRDAPServerByTLD 根据TLD获取RDAP服务器
func GetRDAPServerByTLD(domain string) (RDAPServer, bool) {
	if !isConfigLoaded() {
		if err := LoadServerConfigs(); err != nil {
			return RDAPServer{}, false
		}
	}

	configMutex.RLock()
	defer configMutex.RUnlock()

	key := findBestTLD(domain)
	if key == "" {
		return RDAPServer{}, false
	}

	srv := serversConfig[key].RDAP
	if strings.TrimSpace(srv.Server) == "" {
		return RDAPServer{}, false
	}
	return srv, true
}

// GetDetectionPatterns 获取检测模式
func GetDetectionPatterns() DetectionPatterns {
	if !isConfigLoaded() {
		if err := LoadServerConfigs(); err != nil {
			log.Printf("Config: failed to load detection patterns: %v", err)
		}
	}

	configMutex.RLock()
	defer configMutex.RUnlock()

	// 检测模式已加载

	return patternsConfig
}

// GetSupportedTLDs 获取支持的TLD列表
func GetSupportedTLDs() []string {
	if !isConfigLoaded() {
		if err := LoadServerConfigs(); err != nil {
			return []string{}
		}
	}

	configMutex.RLock()
	defer configMutex.RUnlock()

	tlds := make([]string, 0, len(serversConfig))
	for tld := range serversConfig {
		tlds = append(tlds, tld)
	}

	return tlds
}

// FindBestTLD 对外暴露最长匹配
func FindBestTLD(domain string) string {
	if !isConfigLoaded() {
		if err := LoadServerConfigs(); err != nil {
			return ""
		}
	}
	configMutex.RLock()
	defer configMutex.RUnlock()
	return findBestTLD(domain)
}

// findBestTLD 在配置中匹配最长后缀
func findBestTLD(domain string) string {
	domain = strings.ToLower(domain)
	best := ""
	for tld := range serversConfig {
		lt := strings.ToLower(tld)
		if domain == lt || strings.HasSuffix(domain, "."+lt) {
			if len(lt) > len(best) {
				best = lt
			}
		}
	}
	return best
}
