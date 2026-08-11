package query

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// 内置 Provider 名称
const (
	ProviderRDAP     = "rdap"
	ProviderWhois    = "whois"
	ProviderWhoisLS  = "whois_ls"
	ProviderFallback = "fallback"
)

// canonicalOrder 是未做任何配置时的默认查询顺序。
// 先问按 TLD 显式启用的专用源，再问通用的 RDAP / 注册局 WHOIS。
var canonicalOrder = []string{ProviderWhoisLS, ProviderFallback, ProviderRDAP, ProviderWhois}

// optInProviders 是"必须由用户按 TLD 显式启用"的查询源。
// 它们只在被点名的后缀上生效，因此它们的结论天然带有针对性，可以直接采信。
var optInProviders = map[string]bool{ProviderWhoisLS: true, ProviderFallback: true}

// AvailableMode 描述某个查询源报告"可注册"时的采信程度
type AvailableMode string

const (
	// AvailableTrust 直接采信该源的 available 结论
	AvailableTrust AvailableMode = "trust"
	// AvailableConfirm 需要另一个查询源同样报告 available 才能采信
	AvailableConfirm AvailableMode = "confirm"
	// AvailableDistrust 该源的 available 结论一律不采信，降级为未知
	AvailableDistrust AvailableMode = "distrust"
)

// Step 查询计划中的一步
type Step struct {
	Provider  string
	Available AvailableMode
}

// TLDPolicy 单个 TLD 的查询策略
type TLDPolicy struct {
	// Providers 查询顺序；为空时使用默认顺序
	Providers []string `json:"providers,omitempty"`
	// ValidateAvailable 控制"非首选查询源"报告 available 时的采信程度。
	// 支持 true/false，也支持 "trust" / "confirm" / "distrust" 三档字符串。
	// true 等价于 "distrust"，与重构前的行为完全一致。
	ValidateAvailable AvailableSetting `json:"validate_available,omitempty"`
}

// ProviderSetting Provider 级开关
type ProviderSetting struct {
	Enabled *bool `json:"enabled,omitempty"`
}

// Config 可持久化的查询策略配置（存于 app_settings.query_policy 或 JSON 文件）
type Config struct {
	Providers map[string]ProviderSetting `json:"providers,omitempty"`
	Default   *TLDPolicy                 `json:"default,omitempty"`
	TLDs      map[string]TLDPolicy       `json:"tlds,omitempty"`
	// RateLimits 按 "provider" 或 "provider:tld" 设置两次查询之间的最小间隔，
	// 取值为 Go duration 字符串（如 "1s"）。"0" 表示不限速。
	RateLimits map[string]string `json:"rate_limits,omitempty"`
	// RateLimitConcurrency 按 provider 或 provider:tld 限制同时在途的查询数。
	// 为兼容旧配置，rate_limits 仍可使用字符串；新配置可使用
	// {"interval":"1s","concurrency":1}，由 LoadConfig 解析到此字段。
	RateLimitConcurrency map[string]int `json:"-"`
}

// DefaultRateLimits 是未配置时的兜底限速。
//
// whois_ls 与 fallback 指向的是公共网关或单实例本地服务，一次调度里几十个
// 请求同时打过去会让它们从"2 秒返回"退化成"20 秒超时"，反而把本来能查到的
// 域名变成 unknown。这里默认给它们一个很小的间隔，通用的 RDAP/WHOIS 不限速。
var DefaultRateLimits = map[string]time.Duration{
	ProviderWhoisLS:  time.Second,
	ProviderFallback: time.Second,
}

// AvailableSetting 兼容 bool 与三档字符串的配置项
type AvailableSetting struct {
	Set  bool
	Mode AvailableMode
}

// UnmarshalJSON 允许 true / false / "trust" / "confirm" / "distrust"
func (a *AvailableSetting) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		a.Set = true
		if b {
			a.Mode = AvailableDistrust
		} else {
			a.Mode = AvailableTrust
		}
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("validate_available 必须是布尔值或 trust/confirm/distrust: %s", trimmed)
	}
	switch AvailableMode(strings.ToLower(strings.TrimSpace(s))) {
	case AvailableTrust:
		a.Set, a.Mode = true, AvailableTrust
	case AvailableConfirm:
		a.Set, a.Mode = true, AvailableConfirm
	case AvailableDistrust:
		a.Set, a.Mode = true, AvailableDistrust
	default:
		return fmt.Errorf("未知的 validate_available 取值: %s", s)
	}
	return nil
}

// MarshalJSON 输出三档字符串
func (a AvailableSetting) MarshalJSON() ([]byte, error) {
	if !a.Set {
		return []byte("null"), nil
	}
	return json.Marshal(string(a.Mode))
}

// Policy 根据配置为每个域名生成查询计划
type Policy struct {
	mu  sync.RWMutex
	cfg Config
}

// NewPolicy 创建策略；cfg 为零值时使用默认行为
func NewPolicy(cfg Config) *Policy { return &Policy{cfg: cfg} }

// LoadConfig 解析策略 JSON；空串返回零值配置
func LoadConfig(raw string) (Config, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Config{}, nil
	}
	// rate_limits 在 v1 是 map[string]string；v2 允许每项携带
	// interval/concurrency。先把其余字段解析到一个 wire 结构，避免新旧
	// 配置互相破坏。
	var wire struct {
		Providers  map[string]ProviderSetting `json:"providers,omitempty"`
		Default    *TLDPolicy                 `json:"default,omitempty"`
		TLDs       map[string]TLDPolicy       `json:"tlds,omitempty"`
		RateLimits map[string]json.RawMessage `json:"rate_limits,omitempty"`
	}
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		return Config{}, fmt.Errorf("解析查询策略失败: %w", err)
	}
	cfg := Config{
		Providers:            wire.Providers,
		Default:              wire.Default,
		TLDs:                 wire.TLDs,
		RateLimits:           make(map[string]string, len(wire.RateLimits)),
		RateLimitConcurrency: make(map[string]int, len(wire.RateLimits)),
	}
	for key, value := range wire.RateLimits {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			continue
		}
		var interval string
		if err := json.Unmarshal(value, &interval); err == nil {
			cfg.RateLimits[key] = interval
			continue
		}
		var setting struct {
			Interval    string `json:"interval"`
			MinInterval string `json:"min_interval"`
			Concurrency int    `json:"concurrency"`
		}
		if err := json.Unmarshal(value, &setting); err != nil {
			return Config{}, fmt.Errorf("解析查询策略 rate_limits.%s 失败: %w", key, err)
		}
		if setting.Interval == "" {
			setting.Interval = setting.MinInterval
		}
		if setting.Interval != "" {
			cfg.RateLimits[key] = setting.Interval
		}
		// concurrency=0 是显式的"不限并发"，不能与字段缺失混为一谈，
		// 否则会错误继承 fallback/whois_ls 的默认上限 1。
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(value, &fields); err != nil {
			return Config{}, fmt.Errorf("解析查询策略 rate_limits.%s 失败: %w", key, err)
		}
		if rawConcurrency, present := fields["concurrency"]; present {
			if err := json.Unmarshal(rawConcurrency, &setting.Concurrency); err != nil {
				return Config{}, fmt.Errorf("解析查询策略 rate_limits.%s.concurrency 失败: %w", key, err)
			}
			if setting.Concurrency < 0 {
				return Config{}, fmt.Errorf("查询策略 rate_limits.%s.concurrency 不能为负数", key)
			}
			cfg.RateLimitConcurrency[key] = setting.Concurrency
		}
	}
	return cfg, nil
}

// LoadConfigFromEnvFile 读取 DOMAINHUNTER_QUERY_POLICY_FILE 指向的 JSON 文件
func LoadConfigFromEnvFile() (Config, bool, error) {
	path := strings.TrimSpace(os.Getenv("DOMAINHUNTER_QUERY_POLICY_FILE"))
	if path == "" {
		return Config{}, false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, true, fmt.Errorf("读取查询策略文件失败: %w", err)
	}
	cfg, err := LoadConfig(string(data))
	return cfg, true, err
}

// Update 热更新策略配置
func (p *Policy) Update(cfg Config) {
	p.mu.Lock()
	p.cfg = cfg
	p.mu.Unlock()
}

// Config 返回当前配置快照
func (p *Policy) Config() Config {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cfg
}

// ProviderEnabled 判断某个 Provider 是否被配置禁用
func (p *Policy) ProviderEnabled(name string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if setting, ok := p.cfg.Providers[name]; ok && setting.Enabled != nil {
		return *setting.Enabled
	}
	return true
}

// Plan 为一次请求生成有序的查询步骤。
//
// 规则：
//  1. 专用查询源（whois_ls / fallback）是按后缀显式启用的，其 available 结论
//     直接采信。
//  2. TLD 有显式配置时，其余查询源按 validate_available 处理
//     （true / distrust = 不单独采信，confirm = 需要第二个来源印证）。
//  3. 没有显式配置时：该后缀若启用了专用查询源，通用的 RDAP / WHOIS 的
//     available 不被单独采信（与重构前一致）；否则全部采信。
//  4. Supports() 为 false 的 Provider 直接不进入计划。
func (p *Policy) Plan(ctx context.Context, req Request, reg *Registry) []Step {
	p.mu.RLock()
	cfg := p.cfg
	p.mu.RUnlock()

	tld := strings.ToLower(strings.Trim(strings.TrimSpace(req.TLD), "."))

	var (
		names    []string
		explicit *TLDPolicy
	)
	if tp, ok := cfg.TLDs[tld]; ok {
		explicit = &tp
		names = tp.Providers
	} else if cfg.Default != nil && len(cfg.Default.Providers) > 0 {
		explicit = cfg.Default
		names = cfg.Default.Providers
	}
	if len(names) == 0 {
		names = canonicalOrder
	}

	available := make([]string, 0, len(names))
	hasOptIn := false
	for _, name := range names {
		provider, ok := reg.Get(name)
		if !ok || !p.providerEnabled(cfg, name) || !provider.Supports(ctx, req) {
			continue
		}
		available = append(available, name)
		if optInProviders[name] {
			hasOptIn = true
		}
	}

	steps := make([]Step, 0, len(available))
	for _, name := range available {
		mode := AvailableTrust
		switch {
		case optInProviders[name]:
			// 专用查询源是按后缀显式启用的，结论本身就有针对性，直接采信。
		case explicit != nil && explicit.ValidateAvailable.Set:
			mode = explicit.ValidateAvailable.Mode
		case hasOptIn:
			// 该后缀已配置专用查询源时，通用源的 available 不单独采信。
			mode = AvailableDistrust
		}
		steps = append(steps, Step{Provider: name, Available: mode})
	}
	return steps
}

func (p *Policy) providerEnabled(cfg Config, name string) bool {
	if setting, ok := cfg.Providers[name]; ok && setting.Enabled != nil {
		return *setting.Enabled
	}
	return true
}

// IsOptIn 判断 Provider 是否属于"按 TLD 显式启用"的专用源
func IsOptIn(name string) bool { return optInProviders[name] }
