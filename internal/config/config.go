// Package config 组装应用配置。
//
// 配置的唯一持久化位置仍然是 SQLite 的 app_settings 表（与重构前一致），
// 但本包不再直接依赖存储实现，而是通过 repository.SettingsRepository 接口读写。
// 部署级开关（Cookie Secure、CORS、日志格式）额外支持环境变量覆盖。
package config

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"DomainHunter/internal/repository"
)

// Config 应用配置
type Config struct {
	Server   ServerConfig   `json:"server"`
	SMTP     SMTPConfig     `json:"smtp"`
	Telegram TelegramConfig `json:"telegram"`
	Bark     BarkConfig     `json:"bark"`
	Feishu   FeishuConfig   `json:"feishu"`
	Webhook  WebhookConfig  `json:"webhook"`
	Monitor  MonitorConfig  `json:"monitor"`
	Log      LogConfig      `json:"log"`
	Security SecurityConfig `json:"security"`
	History  HistoryConfig  `json:"history"`

	// QueryPolicy 查询策略的原始 JSON（app_settings.query_policy）
	QueryPolicy string `json:"query_policy"`
}

// ServerConfig 服务与账号配置
type ServerConfig struct {
	Port     string `json:"port"`
	Username string `json:"username"`
	// Password 旧版明文密码。仅用于首次登录时迁移成哈希，之后不再参与校验。
	Password string `json:"-"`
	// PasswordHash bcrypt 哈希，登录校验的唯一依据（存在时）
	PasswordHash string `json:"-"`
	// SessionSecret 用于"记住登录"令牌签名，首次启动自动生成
	SessionSecret string `json:"-"`
}

// SMTPConfig 邮件通知配置
type SMTPConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	From     string `json:"from"`
	To       string `json:"to"`
	Enabled  bool   `json:"enabled"`
}

// TelegramConfig Telegram 通知配置
type TelegramConfig struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
	Enabled  bool   `json:"enabled"`
}

// BarkConfig Bark 推送配置。
//
// URL 是完整的推送地址（自建服务器或 api.day.app），已包含设备 key，
// 例如 https://bark.example.com/AbCdEf123。
type BarkConfig struct {
	URL     string `json:"url"`
	Group   string `json:"group"`
	Sound   string `json:"sound"`
	Level   string `json:"level"`
	Icon    string `json:"icon"`
	Enabled bool   `json:"enabled"`
}

// FeishuConfig 飞书自定义机器人配置。
// Secret 为可选的签名校验密钥（在机器人安全设置里开启"签名校验"时填写）。
type FeishuConfig struct {
	Webhook string `json:"webhook"`
	Secret  string `json:"secret"`
	Enabled bool   `json:"enabled"`
}

// WebhookConfig 通用 Webhook 配置。
// 配置 Secret 后会带上 X-DomainHunter-Signature 头（sha256 HMAC）。
type WebhookConfig struct {
	URL     string `json:"url"`
	Secret  string `json:"secret"`
	Enabled bool   `json:"enabled"`
}

// MonitorConfig 监控配置
type MonitorConfig struct {
	CheckInterval   time.Duration `json:"check_interval"`
	ConcurrentLimit int           `json:"concurrent_limit"`
	Timeout         time.Duration `json:"timeout"`
	CacheDuration   time.Duration `json:"cache_duration"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level string `json:"level"`
	File  string `json:"file"`
}

// SecurityConfig 安全相关配置
type SecurityConfig struct {
	// CookieSecure 取值 auto / true / false。auto 表示按请求是否 HTTPS
	//（含反向代理的 X-Forwarded-Proto）自动决定。
	CookieSecure string `json:"cookie_secure"`
	// CookieSameSite 取值 lax / strict / none
	CookieSameSite string `json:"cookie_same_site"`
	// CORSOrigins 允许的跨域来源；为空表示不输出任何 CORS 头（同源使用）
	CORSOrigins []string `json:"cors_origins"`
	// CSRFEnabled 是否对写操作启用 CSRF 校验
	CSRFEnabled bool `json:"csrf_enabled"`
}

// HistoryConfig 历史数据保留策略
type HistoryConfig struct {
	RetentionDays int `json:"retention_days"`
	MaxPerDomain  int `json:"max_per_domain"`
	// HeartbeatHours 状态没有变化时，两条观测之间至少间隔多少小时。
	// 每次查询都记一条会让数据库涨得毫无必要：817 个域名 10 分钟一轮
	// 就是每天 11 万行，而其中绝大多数内容完全相同。
	HeartbeatHours int    `json:"heartbeat_hours"`
	RawMode        string `json:"raw_mode"`
	RawMaxBytes    int    `json:"raw_max_bytes"`
}

// HeartbeatInterval 返回心跳观测的最小间隔；<=0 表示每次查询都记录
func (h HistoryConfig) HeartbeatInterval() time.Duration {
	if h.HeartbeatHours <= 0 {
		return 0
	}
	return time.Duration(h.HeartbeatHours) * time.Hour
}

// Retention 转换为存储层保留策略
func (h HistoryConfig) Retention() repository.Retention {
	return repository.Retention{
		Days:         h.RetentionDays,
		MaxPerDomain: h.MaxPerDomain,
		RawMaxBytes:  h.RawMaxBytes,
		RawMode:      h.RawMode,
	}
}

// 设置键名。前 4 组与重构前完全一致，新增键都带默认值，缺失时自动回填。
const (
	KeyServerPort       = "server_port"
	KeyServerUsername   = "server_username"
	KeyServerPassword   = "server_password"
	KeyPasswordHash     = "server_password_hash"
	KeySessionSecret    = "session_secret"
	KeySMTPHost         = "smtp_host"
	KeySMTPPort         = "smtp_port"
	KeySMTPUser         = "smtp_user"
	KeySMTPPass         = "smtp_pass"
	KeySMTPFrom         = "smtp_from"
	KeySMTPTo           = "smtp_to"
	KeySMTPEnabled      = "smtp_enabled"
	KeyTelegramToken    = "telegram_bot_token"
	KeyTelegramChatID   = "telegram_chat_id"
	KeyTelegramEnabled  = "telegram_enabled"
	KeyBarkURL          = "bark_url"
	KeyBarkGroup        = "bark_group"
	KeyBarkSound        = "bark_sound"
	KeyBarkLevel        = "bark_level"
	KeyBarkIcon         = "bark_icon"
	KeyBarkEnabled      = "bark_enabled"
	KeyFeishuWebhook    = "feishu_webhook"
	KeyFeishuSecret     = "feishu_secret"
	KeyFeishuEnabled    = "feishu_enabled"
	KeyWebhookURL       = "webhook_url"
	KeyWebhookSecret    = "webhook_secret"
	KeyWebhookEnabled   = "webhook_enabled"
	KeyCheckInterval    = "monitor_check_interval"
	KeyConcurrentLimit  = "monitor_concurrent_limit"
	KeyTimeout          = "monitor_timeout"
	KeyCacheDuration    = "monitor_cache_duration"
	KeyLogLevel         = "log_level"
	KeyQueryPolicy      = "query_policy"
	KeyHistoryDays      = "history_retention_days"
	KeyHistoryMax       = "history_max_per_domain"
	KeyHistoryRawMode   = "history_raw_mode"
	KeyHistoryRawBytes  = "history_raw_max_bytes"
	KeyHistoryHeartbeat = "history_heartbeat_hours"
	KeyCookieSecure     = "security_cookie_secure"
	KeyCookieSameSite   = "security_cookie_same_site"
	KeyCORSOrigins      = "security_cors_origins"
	KeyCSRFEnabled      = "security_csrf_enabled"
)

// Defaults 返回默认配置
func Defaults() *Config {
	cfg := &Config{}
	cfg.Server.Port = "8080"
	cfg.Server.Username = "domainhunter"
	cfg.Server.Password = "domainhunter123"

	cfg.Monitor.CheckInterval = 5 * time.Minute
	cfg.Monitor.ConcurrentLimit = 50
	cfg.Monitor.Timeout = 30 * time.Second
	cfg.Monitor.CacheDuration = 1 * time.Hour

	cfg.Log.Level = "info"

	cfg.Bark.Group = "DomainHunter"

	cfg.Security.CookieSecure = "auto"
	cfg.Security.CookieSameSite = "lax"
	cfg.Security.CSRFEnabled = true

	defaults := repository.DefaultRetention()
	cfg.History.RetentionDays = defaults.Days
	cfg.History.MaxPerDomain = defaults.MaxPerDomain
	cfg.History.RawMode = defaults.RawMode
	cfg.History.HeartbeatHours = 6
	cfg.History.RawMaxBytes = defaults.RawMaxBytes
	return cfg
}

// Load 从设置仓储加载配置，缺失的键会写回默认值
func Load(ctx context.Context, repo repository.SettingsRepository) (*Config, error) {
	cfg := Defaults()

	settings, err := repo.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}

	if missing := missingDefaults(cfg, settings); len(missing) > 0 {
		if err := repo.Upsert(ctx, missing); err != nil {
			return nil, fmt.Errorf("回填默认配置失败: %w", err)
		}
		for k, v := range missing {
			settings[k] = v
		}
	}

	apply(cfg, settings)
	applyEnvOverrides(cfg)

	if updates := normalize(cfg); len(updates) > 0 {
		if err := repo.Upsert(ctx, updates); err != nil {
			return nil, fmt.Errorf("修正配置失败: %w", err)
		}
	}
	return cfg, nil
}

func apply(cfg *Config, settings map[string]string) {
	get := func(key string, fn func(string)) {
		if v, ok := settings[key]; ok {
			fn(strings.TrimSpace(v))
		}
	}
	getInt := func(key string, fn func(int)) {
		get(key, func(v string) {
			if n, err := strconv.Atoi(v); err == nil {
				fn(n)
			}
		})
	}

	get(KeyServerPort, func(v string) { cfg.Server.Port = v })
	get(KeyServerUsername, func(v string) { cfg.Server.Username = v })
	get(KeyServerPassword, func(v string) { cfg.Server.Password = v })
	get(KeyPasswordHash, func(v string) { cfg.Server.PasswordHash = v })
	get(KeySessionSecret, func(v string) { cfg.Server.SessionSecret = v })

	get(KeySMTPHost, func(v string) { cfg.SMTP.Host = v })
	getInt(KeySMTPPort, func(n int) { cfg.SMTP.Port = n })
	get(KeySMTPUser, func(v string) { cfg.SMTP.User = v })
	get(KeySMTPPass, func(v string) { cfg.SMTP.Password = v })
	get(KeySMTPFrom, func(v string) { cfg.SMTP.From = v })
	get(KeySMTPTo, func(v string) { cfg.SMTP.To = v })
	get(KeySMTPEnabled, func(v string) { cfg.SMTP.Enabled = ParseBool(v) })

	get(KeyTelegramToken, func(v string) { cfg.Telegram.BotToken = v })
	get(KeyTelegramChatID, func(v string) { cfg.Telegram.ChatID = v })
	get(KeyTelegramEnabled, func(v string) { cfg.Telegram.Enabled = ParseBool(v) })

	get(KeyBarkURL, func(v string) { cfg.Bark.URL = v })
	get(KeyBarkGroup, func(v string) { cfg.Bark.Group = v })
	get(KeyBarkSound, func(v string) { cfg.Bark.Sound = v })
	get(KeyBarkLevel, func(v string) { cfg.Bark.Level = v })
	get(KeyBarkIcon, func(v string) { cfg.Bark.Icon = v })
	get(KeyBarkEnabled, func(v string) { cfg.Bark.Enabled = ParseBool(v) })

	get(KeyFeishuWebhook, func(v string) { cfg.Feishu.Webhook = v })
	get(KeyFeishuSecret, func(v string) { cfg.Feishu.Secret = v })
	get(KeyFeishuEnabled, func(v string) { cfg.Feishu.Enabled = ParseBool(v) })

	get(KeyWebhookURL, func(v string) { cfg.Webhook.URL = v })
	get(KeyWebhookSecret, func(v string) { cfg.Webhook.Secret = v })
	get(KeyWebhookEnabled, func(v string) { cfg.Webhook.Enabled = ParseBool(v) })

	getInt(KeyCheckInterval, func(n int) { cfg.Monitor.CheckInterval = time.Duration(n) * time.Second })
	getInt(KeyConcurrentLimit, func(n int) { cfg.Monitor.ConcurrentLimit = n })
	getInt(KeyTimeout, func(n int) { cfg.Monitor.Timeout = time.Duration(n) * time.Second })
	getInt(KeyCacheDuration, func(n int) { cfg.Monitor.CacheDuration = time.Duration(n) * time.Second })

	get(KeyLogLevel, func(v string) { cfg.Log.Level = v })
	get(KeyQueryPolicy, func(v string) { cfg.QueryPolicy = v })

	getInt(KeyHistoryDays, func(n int) { cfg.History.RetentionDays = n })
	getInt(KeyHistoryMax, func(n int) { cfg.History.MaxPerDomain = n })
	get(KeyHistoryRawMode, func(v string) { cfg.History.RawMode = v })
	getInt(KeyHistoryRawBytes, func(n int) { cfg.History.RawMaxBytes = n })
	getInt(KeyHistoryHeartbeat, func(n int) { cfg.History.HeartbeatHours = n })

	get(KeyCookieSecure, func(v string) { cfg.Security.CookieSecure = v })
	get(KeyCookieSameSite, func(v string) { cfg.Security.CookieSameSite = v })
	get(KeyCORSOrigins, func(v string) { cfg.Security.CORSOrigins = splitCSV(v) })
	get(KeyCSRFEnabled, func(v string) { cfg.Security.CSRFEnabled = ParseBool(v) })
}

// applyEnvOverrides 部署级开关允许用 DOMAINHUNTER_* 环境变量覆盖数据库中的值。
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("DOMAINHUNTER_COOKIE_SECURE"); strings.TrimSpace(v) != "" {
		cfg.Security.CookieSecure = v
	}
	if v := os.Getenv("DOMAINHUNTER_COOKIE_SAMESITE"); strings.TrimSpace(v) != "" {
		cfg.Security.CookieSameSite = v
	}
	if v := os.Getenv("DOMAINHUNTER_CORS_ORIGINS"); strings.TrimSpace(v) != "" {
		cfg.Security.CORSOrigins = splitCSV(v)
	}
	if v := os.Getenv("DOMAINHUNTER_CSRF_ENABLED"); strings.TrimSpace(v) != "" {
		cfg.Security.CSRFEnabled = ParseBool(v)
	}
	if v := os.Getenv("DOMAINHUNTER_PORT"); strings.TrimSpace(v) != "" {
		cfg.Server.Port = v
	}
	if v := os.Getenv("DOMAINHUNTER_LOG_LEVEL"); strings.TrimSpace(v) != "" {
		cfg.Log.Level = v
	}
}

func missingDefaults(cfg *Config, settings map[string]string) map[string]string {
	defaults := map[string]string{
		KeyServerPort:       cfg.Server.Port,
		KeyServerUsername:   cfg.Server.Username,
		KeyServerPassword:   cfg.Server.Password,
		KeySMTPHost:         cfg.SMTP.Host,
		KeySMTPPort:         strconv.Itoa(cfg.SMTP.Port),
		KeySMTPUser:         cfg.SMTP.User,
		KeySMTPPass:         cfg.SMTP.Password,
		KeySMTPFrom:         cfg.SMTP.From,
		KeySMTPTo:           cfg.SMTP.To,
		KeySMTPEnabled:      strconv.FormatBool(cfg.SMTP.Enabled),
		KeyTelegramToken:    cfg.Telegram.BotToken,
		KeyTelegramChatID:   cfg.Telegram.ChatID,
		KeyTelegramEnabled:  strconv.FormatBool(cfg.Telegram.Enabled),
		KeyBarkURL:          cfg.Bark.URL,
		KeyBarkGroup:        cfg.Bark.Group,
		KeyBarkSound:        cfg.Bark.Sound,
		KeyBarkLevel:        cfg.Bark.Level,
		KeyBarkIcon:         cfg.Bark.Icon,
		KeyBarkEnabled:      strconv.FormatBool(cfg.Bark.Enabled),
		KeyFeishuWebhook:    cfg.Feishu.Webhook,
		KeyFeishuSecret:     cfg.Feishu.Secret,
		KeyFeishuEnabled:    strconv.FormatBool(cfg.Feishu.Enabled),
		KeyWebhookURL:       cfg.Webhook.URL,
		KeyWebhookSecret:    cfg.Webhook.Secret,
		KeyWebhookEnabled:   strconv.FormatBool(cfg.Webhook.Enabled),
		KeyCheckInterval:    strconv.Itoa(int(cfg.Monitor.CheckInterval.Seconds())),
		KeyConcurrentLimit:  strconv.Itoa(cfg.Monitor.ConcurrentLimit),
		KeyTimeout:          strconv.Itoa(int(cfg.Monitor.Timeout.Seconds())),
		KeyCacheDuration:    strconv.Itoa(int(cfg.Monitor.CacheDuration.Seconds())),
		KeyLogLevel:         cfg.Log.Level,
		KeyHistoryDays:      strconv.Itoa(cfg.History.RetentionDays),
		KeyHistoryMax:       strconv.Itoa(cfg.History.MaxPerDomain),
		KeyHistoryRawMode:   cfg.History.RawMode,
		KeyHistoryRawBytes:  strconv.Itoa(cfg.History.RawMaxBytes),
		KeyHistoryHeartbeat: strconv.Itoa(cfg.History.HeartbeatHours),
		KeyCookieSecure:     cfg.Security.CookieSecure,
		KeyCookieSameSite:   cfg.Security.CookieSameSite,
		KeyCSRFEnabled:      strconv.FormatBool(cfg.Security.CSRFEnabled),
	}

	missing := map[string]string{}
	for k, v := range defaults {
		if _, ok := settings[k]; !ok {
			missing[k] = v
		}
	}
	return missing
}

// normalize 把空值/非法值修正为默认值，并返回需要写回数据库的部分
func normalize(cfg *Config) map[string]string {
	updates := map[string]string{}

	if cfg.Server.Username == "" {
		cfg.Server.Username = "domainhunter"
		updates[KeyServerUsername] = cfg.Server.Username
	}
	if cfg.Server.Port == "" {
		cfg.Server.Port = "8080"
		updates[KeyServerPort] = cfg.Server.Port
	}
	if cfg.Server.Password == "" && cfg.Server.PasswordHash == "" {
		cfg.Server.Password = "domainhunter123"
		updates[KeyServerPassword] = cfg.Server.Password
	}
	if cfg.SMTP.Port == 0 {
		cfg.SMTP.Port = 587
		updates[KeySMTPPort] = strconv.Itoa(cfg.SMTP.Port)
	}
	if cfg.Monitor.CheckInterval <= 0 {
		cfg.Monitor.CheckInterval = 5 * time.Minute
		updates[KeyCheckInterval] = strconv.Itoa(int(cfg.Monitor.CheckInterval.Seconds()))
	}
	if cfg.Monitor.ConcurrentLimit <= 0 {
		cfg.Monitor.ConcurrentLimit = 50
		updates[KeyConcurrentLimit] = strconv.Itoa(cfg.Monitor.ConcurrentLimit)
	}
	if cfg.Monitor.Timeout <= 0 {
		cfg.Monitor.Timeout = 30 * time.Second
		updates[KeyTimeout] = strconv.Itoa(int(cfg.Monitor.Timeout.Seconds()))
	}
	if cfg.Monitor.CacheDuration <= 0 {
		cfg.Monitor.CacheDuration = time.Hour
		updates[KeyCacheDuration] = strconv.Itoa(int(cfg.Monitor.CacheDuration.Seconds()))
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
		updates[KeyLogLevel] = cfg.Log.Level
	}
	if cfg.History.RawMode != repository.RawModeAlways &&
		cfg.History.RawMode != repository.RawModeNever &&
		cfg.History.RawMode != repository.RawModeChangeOnly {
		cfg.History.RawMode = repository.RawModeChangeOnly
		updates[KeyHistoryRawMode] = cfg.History.RawMode
	}
	if cfg.History.RawMaxBytes <= 0 {
		cfg.History.RawMaxBytes = 16 * 1024
		updates[KeyHistoryRawBytes] = strconv.Itoa(cfg.History.RawMaxBytes)
	}
	switch strings.ToLower(cfg.Security.CookieSecure) {
	case "auto", "true", "false":
	default:
		cfg.Security.CookieSecure = "auto"
		updates[KeyCookieSecure] = cfg.Security.CookieSecure
	}
	switch strings.ToLower(cfg.Security.CookieSameSite) {
	case "lax", "strict", "none":
	default:
		cfg.Security.CookieSameSite = "lax"
		updates[KeyCookieSameSite] = cfg.Security.CookieSameSite
	}
	return updates
}

// Validate 校验配置
func (cfg *Config) Validate() error {
	if cfg.Server.Username == "" {
		return fmt.Errorf("服务器用户名不能为空")
	}
	if cfg.Server.Password == "" && cfg.Server.PasswordHash == "" {
		return fmt.Errorf("服务器密码不能为空")
	}
	if cfg.Monitor.CheckInterval < 5*time.Second {
		return fmt.Errorf("检查间隔不能小于5秒")
	}
	if cfg.Monitor.ConcurrentLimit <= 0 {
		return fmt.Errorf("并发限制必须大于0")
	}
	if cfg.Monitor.Timeout <= 0 {
		return fmt.Errorf("查询超时时间必须大于0")
	}
	return nil
}

// NotificationEnabled 是否启用了任一通知渠道
func (cfg *Config) NotificationEnabled() bool {
	return cfg.SMTP.Enabled || cfg.Telegram.Enabled ||
		cfg.Bark.Enabled || cfg.Feishu.Enabled || cfg.Webhook.Enabled
}

// Clone 返回配置的浅拷贝，便于热更新时替换整份配置而不产生数据竞争
func (cfg *Config) Clone() *Config {
	copied := *cfg
	copied.Security.CORSOrigins = append([]string(nil), cfg.Security.CORSOrigins...)
	return &copied
}

// ParseBool 解析布尔配置
func ParseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
