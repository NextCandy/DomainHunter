// Package registry 维护 TLD → WHOIS/RDAP 服务端点的映射，以及 WHOIS 文本的
// 状态识别词表。
//
// 数据来源有两层：
//  1. 内置的 servers.json / detection_patterns.json（离线兜底，随二进制嵌入）
//  2. IANA 官方 RDAP bootstrap（https://data.iana.org/rdap/dns.json，动态补充）
//
// IANA 不可达时不阻塞启动，内置静态映射继续生效。
package registry

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

//go:embed detection_patterns.json servers.json
var configFiles embed.FS

// WhoisServer WHOIS 服务器配置
type WhoisServer struct {
	Server string `json:"server"`
	Port   int    `json:"port"`
}

// RDAPServer RDAP 服务器配置
type RDAPServer struct {
	Server string `json:"server"`
}

// TLDServers 单个 TLD 的查询端点
type TLDServers struct {
	Whois WhoisServer `json:"whois"`
	RDAP  RDAPServer  `json:"rdap"`
}

// DetectionPatterns WHOIS 文本状态识别词表
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

const defaultRDAPBootstrapURL = "https://data.iana.org/rdap/dns.json"

type rdapBootstrapFile struct {
	Services [][]json.RawMessage `json:"services"`
}

var (
	mu             sync.RWMutex
	loadMu         sync.Mutex
	serversConfig  map[string]TLDServers
	patternsConfig DetectionPatterns
	loaded         bool
	bootstrapStat  BootstrapStatus
)

// BootstrapStatus 记录最近一次 IANA bootstrap 合并结果，供 /health 展示
type BootstrapStatus struct {
	Attempted  bool      `json:"attempted"`
	OK         bool      `json:"ok"`
	Merged     int       `json:"merged"`
	Error      string    `json:"error,omitempty"`
	LastUpdate time.Time `json:"last_update,omitempty"`
	Source     string    `json:"source,omitempty"`
}

// GetEmbeddedFile 读取嵌入的配置文件内容
func GetEmbeddedFile(filename string) ([]byte, error) {
	return configFiles.ReadFile(filename)
}

// Load 加载内置配置并尝试合并 IANA bootstrap（已加载则直接返回）
func Load() error { return load(false) }

// Reload 强制重新读取内置配置并刷新 IANA bootstrap
func Reload() error { return load(true) }

func load(force bool) error {
	loadMu.Lock()
	defer loadMu.Unlock()

	mu.RLock()
	already := loaded
	mu.RUnlock()
	if already && !force {
		return nil
	}

	sData, err := GetEmbeddedFile("servers.json")
	if err != nil {
		return fmt.Errorf("读取 servers.json 失败: %w", err)
	}
	var loadedServers map[string]TLDServers
	if err := json.Unmarshal(sData, &loadedServers); err != nil {
		return fmt.Errorf("解析 servers.json 失败: %w", err)
	}

	pData, err := GetEmbeddedFile("detection_patterns.json")
	if err != nil {
		return fmt.Errorf("读取 detection_patterns.json 失败: %w", err)
	}
	var loadedPatterns DetectionPatterns
	if err := json.Unmarshal(pData, &loadedPatterns); err != nil {
		return fmt.Errorf("解析 detection_patterns.json 失败: %w", err)
	}
	if len(loadedPatterns.AvailablePatterns) == 0 {
		return fmt.Errorf("detection_patterns.json 中 available_patterns 为空，配置无效")
	}

	status := mergeRDAPBootstrap(loadedServers)

	mu.Lock()
	serversConfig = loadedServers
	patternsConfig = loadedPatterns
	bootstrapStat = status
	loaded = true
	mu.Unlock()
	return nil
}

func mergeRDAPBootstrap(servers map[string]TLDServers) BootstrapStatus {
	endpoint := strings.TrimSpace(os.Getenv("DOMAINHUNTER_RDAP_BOOTSTRAP_URL"))
	if endpoint == "" {
		endpoint = strings.TrimSpace(os.Getenv("PUFF_RDAP_BOOTSTRAP_URL"))
	}
	if endpoint == "" {
		endpoint = defaultRDAPBootstrapURL
	}
	status := BootstrapStatus{Attempted: true, Source: endpoint}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		status.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return status
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		status.Error = err.Error()
		return status
	}
	var bootstrap rdapBootstrapFile
	if err := json.Unmarshal(body, &bootstrap); err != nil {
		status.Error = err.Error()
		return status
	}

	merged := 0
	for _, service := range bootstrap.Services {
		if len(service) != 2 {
			continue
		}
		var tlds, urls []string
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
		status.Error = "bootstrap 未包含可用服务"
		return status
	}
	status.OK = true
	status.Merged = merged
	status.LastUpdate = time.Now()
	return status
}

func ensureLoaded() bool {
	mu.RLock()
	ok := loaded
	mu.RUnlock()
	if ok {
		return true
	}
	return load(false) == nil
}

// Bootstrap 返回最近一次 IANA bootstrap 状态
func Bootstrap() BootstrapStatus {
	ensureLoaded()
	mu.RLock()
	defer mu.RUnlock()
	return bootstrapStat
}

// WhoisServerFor 返回域名对应的 WHOIS 服务器
func WhoisServerFor(name string) (WhoisServer, bool) {
	if !ensureLoaded() {
		return WhoisServer{}, false
	}
	mu.RLock()
	defer mu.RUnlock()

	key := findBestTLD(name)
	if key == "" {
		return WhoisServer{}, false
	}
	server := serversConfig[key].Whois
	if strings.TrimSpace(server.Server) == "" {
		return WhoisServer{}, false
	}
	if server.Port <= 0 {
		server.Port = 43
	}
	return server, true
}

// RDAPServerFor 返回域名对应的 RDAP 服务器
func RDAPServerFor(name string) (RDAPServer, bool) {
	if !ensureLoaded() {
		return RDAPServer{}, false
	}
	mu.RLock()
	defer mu.RUnlock()

	key := findBestTLD(name)
	if key == "" {
		return RDAPServer{}, false
	}
	srv := serversConfig[key].RDAP
	if strings.TrimSpace(srv.Server) == "" {
		return RDAPServer{}, false
	}
	return srv, true
}

// Patterns 返回 WHOIS 文本识别词表
func Patterns() DetectionPatterns {
	ensureLoaded()
	mu.RLock()
	defer mu.RUnlock()
	return patternsConfig
}

// SupportedTLDs 返回全部已知 TLD
func SupportedTLDs() []string {
	if !ensureLoaded() {
		return nil
	}
	mu.RLock()
	defer mu.RUnlock()
	tlds := make([]string, 0, len(serversConfig))
	for tld := range serversConfig {
		tlds = append(tlds, tld)
	}
	return tlds
}

// FindBestTLD 用最长后缀匹配确定域名所属 TLD（如 a.com.cn → com.cn）
func FindBestTLD(name string) string {
	if !ensureLoaded() {
		return ""
	}
	mu.RLock()
	defer mu.RUnlock()
	return findBestTLD(name)
}

func findBestTLD(name string) string {
	name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	best := ""
	for tld := range serversConfig {
		lt := strings.ToLower(tld)
		if name == lt || strings.HasSuffix(name, "."+lt) {
			if len(lt) > len(best) {
				best = lt
			}
		}
	}
	return best
}
