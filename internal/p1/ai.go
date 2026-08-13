package p1

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"DomainHunter/internal/storage/sqlite"
)

const (
	defaultAIProvider = "openai_compatible"
	defaultAIBaseURL  = "https://api.deepseek.com"
	defaultAIModel    = "deepseek-v4-flash"
	analysisVersion   = "p1-valuation-v1"
)

type AIService struct {
	db      *sqlite.DB
	mu      sync.Mutex
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	wake    chan struct{}
	started bool
}

func NewAIService(db *sqlite.DB) *AIService { return &AIService{db: db, wake: make(chan struct{}, 1)} }

func (s *AIService) Start(parent context.Context) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel, s.started = cancel, true
	s.mu.Unlock()

	settings, err := s.PublicSettings(context.Background())
	workers := 1
	if err == nil && settings.Concurrency > 0 && settings.Concurrency <= 4 {
		workers = settings.Concurrency
	}
	for i := 0; i < workers; i++ {
		s.wg.Add(1)
		go s.worker(ctx)
	}
}

func (s *AIService) Stop() {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *AIService) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func defaultSettings() AISettingsInput {
	return AISettingsInput{Provider: defaultAIProvider, BaseURL: defaultAIBaseURL, Model: defaultAIModel,
		TimeoutSeconds: 30, Concurrency: 1, MaxOutputTokens: 1200, DailyLimit: 0, CacheTTLSeconds: 86400}
}

func normalizeAIProvider(raw string) (string, error) {
	provider := strings.ToLower(strings.TrimSpace(raw))
	switch provider {
	case "":
		return defaultAIProvider, nil
	case "openai compatible", "openai-compatible", "openai_compatible", "openai":
		return "openai_compatible", nil
	case "deepseek":
		return "deepseek", nil
	}
	if len(provider) > 64 {
		return "", fmt.Errorf("Provider 标识不能超过64个字符")
	}
	for _, r := range provider {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '-' && r != '.' {
			return "", fmt.Errorf("Provider 标识只能包含字母、数字、点、下划线或短横线")
		}
	}
	return provider, nil
}

func providerDisplayName(provider string) string {
	switch provider {
	case "openai_compatible":
		return "OpenAI Compatible"
	case "deepseek":
		return "DeepSeek"
	default:
		return provider
	}
}

func normalizeProfileName(name, provider string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = providerDisplayName(provider)
	}
	if len(name) > 80 {
		return "", fmt.Errorf("AI 配置名称不能超过80个字符")
	}
	if strings.ContainsAny(name, "\r\n\x00") {
		return "", fmt.Errorf("AI 配置名称包含无效字符")
	}
	return name, nil
}

func normalizeAISettings(input AISettingsInput) (AISettingsInput, error) {
	defaults := defaultSettings()
	provider, err := normalizeAIProvider(input.Provider)
	if err != nil {
		return AISettingsInput{}, err
	}
	input.Provider = provider
	input.Name, err = normalizeProfileName(input.Name, provider)
	if err != nil {
		return AISettingsInput{}, err
	}
	if strings.TrimSpace(input.BaseURL) == "" {
		input.BaseURL = defaults.BaseURL
	}
	input.BaseURL = strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	if err := validateAIBaseURL(input.BaseURL); err != nil {
		return AISettingsInput{}, err
	}
	if strings.TrimSpace(input.Model) == "" || len(input.Model) > 128 {
		return AISettingsInput{}, fmt.Errorf("模型名称不能为空且不能超过128个字符")
	}
	if input.TimeoutSeconds < 5 || input.TimeoutSeconds > 300 {
		return AISettingsInput{}, fmt.Errorf("超时必须在5到300秒之间")
	}
	if input.Concurrency < 1 || input.Concurrency > 4 {
		return AISettingsInput{}, fmt.Errorf("并发必须在1到4之间")
	}
	if input.MaxOutputTokens < 128 || input.MaxOutputTokens > 8192 {
		return AISettingsInput{}, fmt.Errorf("最大输出 token 必须在128到8192之间")
	}
	// Daily AI valuation limits are disabled. The legacy column remains in the
	// schema for compatibility, but it is always written as zero.
	input.DailyLimit = 0
	if input.CacheTTLSeconds < 300 || input.CacheTTLSeconds > 30*24*3600 {
		return AISettingsInput{}, fmt.Errorf("缓存 TTL 必须在5分钟到30天之间")
	}
	input.APIKey = strings.TrimSpace(input.APIKey)
	return input, nil
}

func (s *AIService) storedSettings(ctx context.Context) (AISettingsInput, string, error) {
	settings := defaultSettings()
	var encrypted string
	row := s.db.QueryRowContext(ctx, `SELECT provider,base_url,model,encrypted_api_key,timeout_seconds,concurrency,max_output_tokens,daily_limit,cache_ttl_seconds,enabled FROM ai_provider_settings WHERE id=1`)
	var enabled int
	if err := row.Scan(&settings.Provider, &settings.BaseURL, &settings.Model, &encrypted, &settings.TimeoutSeconds,
		&settings.Concurrency, &settings.MaxOutputTokens, &settings.DailyLimit, &settings.CacheTTLSeconds, &enabled); err != nil {
		return settings, "", err
	}
	settings.Enabled = enabled == 1
	return settings, encrypted, nil
}

func (s *AIService) PublicSettings(ctx context.Context) (AISettingsPublic, error) {
	settings, encrypted, err := s.storedSettings(ctx)
	if err != nil {
		return AISettingsPublic{}, err
	}
	var profileID int64
	var profileName string
	var isDefault int
	err = s.db.QueryRowContext(ctx, `SELECT id,name,is_default FROM ai_provider_profiles WHERE is_default=1 ORDER BY id LIMIT 1`).Scan(&profileID, &profileName, &isDefault)
	if err != nil && err != sql.ErrNoRows {
		return AISettingsPublic{}, err
	}
	if profileName == "" {
		profileName = providerDisplayName(settings.Provider)
	}
	_, source := configuredAPIKey(encrypted)
	return AISettingsPublic{ProfileID: profileID, ProfileName: profileName, IsDefault: profileID > 0 && isDefault == 1,
		Provider: settings.Provider, BaseURL: settings.BaseURL, Model: settings.Model,
		APIKeySet: source != "none", KeySource: source, TimeoutSeconds: settings.TimeoutSeconds,
		Concurrency: settings.Concurrency, MaxOutputTokens: settings.MaxOutputTokens, DailyLimit: settings.DailyLimit,
		CacheTTLSeconds: settings.CacheTTLSeconds, Enabled: settings.Enabled}, nil
}

func (s *AIService) SaveSettings(ctx context.Context, input AISettingsInput) (AISettingsPublic, error) {
	var err error
	input, err = normalizeAISettings(input)
	if err != nil {
		return AISettingsPublic{}, err
	}
	var oldEncrypted string
	if input.ProfileID > 0 {
		if err := s.db.QueryRowContext(ctx, `SELECT encrypted_api_key FROM ai_provider_profiles WHERE id=?`, input.ProfileID).Scan(&oldEncrypted); err != nil {
			if err == sql.ErrNoRows {
				return AISettingsPublic{}, fmt.Errorf("AI 配置不存在")
			}
			return AISettingsPublic{}, err
		}
	} else {
		_, oldEncrypted, err = s.storedSettings(ctx)
		if err != nil {
			return AISettingsPublic{}, err
		}
	}
	encrypted := oldEncrypted
	if strings.TrimSpace(input.APIKey) != "" {
		secret := os.Getenv("DOMAINHUNTER_SECRET_KEY")
		if strings.TrimSpace(secret) == "" {
			return AISettingsPublic{}, fmt.Errorf("保存 API Key 前必须配置 DOMAINHUNTER_SECRET_KEY")
		}
		encrypted, err = encryptSecret(input.APIKey, secret)
		if err != nil {
			return AISettingsPublic{}, fmt.Errorf("加密 API Key 失败: %w", err)
		}
	}
	if input.ProfileID > 0 {
		if err := s.saveProfile(ctx, input, encrypted); err != nil {
			return AISettingsPublic{}, err
		}
		return s.PublicSettings(ctx)
	}
	if err := s.saveDefaultSettings(ctx, input, encrypted); err != nil {
		return AISettingsPublic{}, err
	}
	return s.PublicSettings(ctx)
}

func (s *AIService) saveDefaultSettings(ctx context.Context, input AISettingsInput, encrypted string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE ai_provider_settings SET provider=?,base_url=?,model=?,encrypted_api_key=?,timeout_seconds=?,concurrency=?,max_output_tokens=?,daily_limit=?,cache_ttl_seconds=?,enabled=?,updated_at=CURRENT_TIMESTAMP WHERE id=1`,
		input.Provider, input.BaseURL, input.Model, encrypted, input.TimeoutSeconds, input.Concurrency, input.MaxOutputTokens,
		input.DailyLimit, input.CacheTTLSeconds, boolInt(input.Enabled)); err != nil {
		return err
	}
	var profileID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM ai_provider_profiles WHERE is_default=1 ORDER BY id LIMIT 1`).Scan(&profileID)
	switch err {
	case nil:
		_, err = tx.ExecContext(ctx, `UPDATE ai_provider_profiles SET name=?,provider=?,base_url=?,model=?,encrypted_api_key=?,timeout_seconds=?,concurrency=?,max_output_tokens=?,daily_limit=?,cache_ttl_seconds=?,enabled=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`,
			input.Name, input.Provider, input.BaseURL, input.Model, encrypted, input.TimeoutSeconds, input.Concurrency, input.MaxOutputTokens,
			input.DailyLimit, input.CacheTTLSeconds, boolInt(input.Enabled), profileID)
	case sql.ErrNoRows:
		_, err = tx.ExecContext(ctx, `INSERT INTO ai_provider_profiles(name,provider,base_url,model,encrypted_api_key,timeout_seconds,concurrency,max_output_tokens,daily_limit,cache_ttl_seconds,enabled,is_default) VALUES(?,?,?,?,?,?,?,?,?,?,?,1)`,
			input.Name, input.Provider, input.BaseURL, input.Model, encrypted, input.TimeoutSeconds, input.Concurrency, input.MaxOutputTokens,
			input.DailyLimit, input.CacheTTLSeconds, boolInt(input.Enabled))
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *AIService) saveProfile(ctx context.Context, input AISettingsInput, encrypted string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var currentDefault int
	if err := tx.QueryRowContext(ctx, `SELECT is_default FROM ai_provider_profiles WHERE id=?`, input.ProfileID).Scan(&currentDefault); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("AI 配置不存在")
		}
		return err
	}
	makeDefault := input.IsDefault || currentDefault == 1
	if makeDefault {
		if _, err := tx.ExecContext(ctx, `UPDATE ai_provider_profiles SET is_default=0,updated_at=CURRENT_TIMESTAMP WHERE is_default=1 AND id<>?`, input.ProfileID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE ai_provider_profiles SET name=?,provider=?,base_url=?,model=?,encrypted_api_key=?,timeout_seconds=?,concurrency=?,max_output_tokens=?,daily_limit=?,cache_ttl_seconds=?,enabled=?,is_default=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		input.Name, input.Provider, input.BaseURL, input.Model, encrypted, input.TimeoutSeconds, input.Concurrency, input.MaxOutputTokens,
		input.DailyLimit, input.CacheTTLSeconds, boolInt(input.Enabled), boolInt(makeDefault), input.ProfileID); err != nil {
		return err
	}
	if makeDefault {
		if _, err := tx.ExecContext(ctx, `UPDATE ai_provider_settings SET provider=?,base_url=?,model=?,encrypted_api_key=?,timeout_seconds=?,concurrency=?,max_output_tokens=?,daily_limit=?,cache_ttl_seconds=?,enabled=?,updated_at=CURRENT_TIMESTAMP WHERE id=1`,
			input.Provider, input.BaseURL, input.Model, encrypted, input.TimeoutSeconds, input.Concurrency, input.MaxOutputTokens,
			input.DailyLimit, input.CacheTTLSeconds, boolInt(input.Enabled)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *AIService) ListProviderProfiles(ctx context.Context) ([]AIProviderProfilePublic, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,provider,base_url,model,encrypted_api_key,timeout_seconds,concurrency,max_output_tokens,daily_limit,cache_ttl_seconds,enabled,is_default FROM ai_provider_profiles ORDER BY is_default DESC, name COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	profiles := make([]AIProviderProfilePublic, 0)
	for rows.Next() {
		var profile AIProviderProfilePublic
		var encrypted string
		var enabled, isDefault int
		if err := rows.Scan(&profile.ProfileID, &profile.ProfileName, &profile.Provider, &profile.BaseURL, &profile.Model, &encrypted,
			&profile.TimeoutSeconds, &profile.Concurrency, &profile.MaxOutputTokens, &profile.DailyLimit, &profile.CacheTTLSeconds,
			&enabled, &isDefault); err != nil {
			return nil, err
		}
		profile.Enabled = enabled == 1
		profile.IsDefault = isDefault == 1
		_, profile.KeySource = configuredProfileAPIKey(encrypted, profile.IsDefault)
		profile.APIKeySet = profile.KeySource != "none"
		profiles = append(profiles, profile)
	}
	return profiles, rows.Err()
}

func (s *AIService) CreateProviderProfile(ctx context.Context, input AISettingsInput) (AIProviderProfilePublic, error) {
	input.ProfileID = 0
	var err error
	input, err = normalizeAISettings(input)
	if err != nil {
		return AIProviderProfilePublic{}, err
	}
	if input.APIKey != "" {
		secret := os.Getenv("DOMAINHUNTER_SECRET_KEY")
		if strings.TrimSpace(secret) == "" {
			return AIProviderProfilePublic{}, fmt.Errorf("保存 API Key 前必须配置 DOMAINHUNTER_SECRET_KEY")
		}
		if input.APIKey, err = encryptSecret(input.APIKey, secret); err != nil {
			return AIProviderProfilePublic{}, fmt.Errorf("加密 API Key 失败: %w", err)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AIProviderProfilePublic{}, err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_provider_profiles WHERE is_default=1`).Scan(&count); err != nil {
		return AIProviderProfilePublic{}, err
	}
	makeDefault := input.IsDefault || count == 0
	if makeDefault {
		if _, err := tx.ExecContext(ctx, `UPDATE ai_provider_profiles SET is_default=0,updated_at=CURRENT_TIMESTAMP WHERE is_default=1`); err != nil {
			return AIProviderProfilePublic{}, err
		}
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO ai_provider_profiles(name,provider,base_url,model,encrypted_api_key,timeout_seconds,concurrency,max_output_tokens,daily_limit,cache_ttl_seconds,enabled,is_default) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		input.Name, input.Provider, input.BaseURL, input.Model, input.APIKey, input.TimeoutSeconds, input.Concurrency, input.MaxOutputTokens,
		input.DailyLimit, input.CacheTTLSeconds, boolInt(input.Enabled), boolInt(makeDefault))
	if err != nil {
		return AIProviderProfilePublic{}, err
	}
	input.ProfileID, err = result.LastInsertId()
	if err != nil {
		return AIProviderProfilePublic{}, err
	}
	if makeDefault {
		if _, err := tx.ExecContext(ctx, `UPDATE ai_provider_settings SET provider=?,base_url=?,model=?,encrypted_api_key=?,timeout_seconds=?,concurrency=?,max_output_tokens=?,daily_limit=?,cache_ttl_seconds=?,enabled=?,updated_at=CURRENT_TIMESTAMP WHERE id=1`,
			input.Provider, input.BaseURL, input.Model, input.APIKey, input.TimeoutSeconds, input.Concurrency, input.MaxOutputTokens,
			input.DailyLimit, input.CacheTTLSeconds, boolInt(input.Enabled)); err != nil {
			return AIProviderProfilePublic{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return AIProviderProfilePublic{}, err
	}
	profiles, err := s.ListProviderProfiles(ctx)
	if err != nil {
		return AIProviderProfilePublic{}, err
	}
	for _, profile := range profiles {
		if profile.ProfileID == input.ProfileID {
			return profile, nil
		}
	}
	return AIProviderProfilePublic{}, fmt.Errorf("创建 AI 配置后读取失败")
}

func (s *AIService) DeleteProviderProfile(ctx context.Context, id int64) error {
	if id <= 0 {
		return fmt.Errorf("AI 配置 ID 无效")
	}
	var isDefault int
	if err := s.db.QueryRowContext(ctx, `SELECT is_default FROM ai_provider_profiles WHERE id=?`, id).Scan(&isDefault); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("AI 配置不存在")
		}
		return err
	}
	if isDefault == 1 {
		return fmt.Errorf("默认 AI 配置不能删除，请先切换默认项")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM ai_provider_profiles WHERE id=?`, id)
	return err
}

func configuredProfileAPIKey(encrypted string, isDefault bool) (string, string) {
	if isDefault {
		return configuredAPIKey(encrypted)
	}
	if key := strings.TrimSpace(os.Getenv("DOMAINHUNTER_SECRET_KEY")); key != "" && strings.TrimSpace(encrypted) != "" {
		if value, err := decryptSecret(encrypted, key); err == nil && value != "" {
			return value, "encrypted"
		}
		return "", "encrypted_unavailable"
	}
	if strings.TrimSpace(encrypted) != "" {
		return "", "encrypted_unavailable"
	}
	return "", "none"
}

func configuredAPIKey(encrypted string) (string, string) {
	if key := strings.TrimSpace(os.Getenv("DOMAINHUNTER_AI_API_KEY")); key != "" {
		return key, "env"
	}
	if strings.TrimSpace(encrypted) == "" {
		return "", "none"
	}
	if secret := os.Getenv("DOMAINHUNTER_SECRET_KEY"); strings.TrimSpace(secret) != "" {
		if key, err := decryptSecret(encrypted, secret); err == nil && key != "" {
			return key, "encrypted"
		}
	}
	return "", "encrypted_unavailable"
}

func encryptSecret(value, secret string) (string, error) {
	block, err := aes.NewCipher(secretKey(secret))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(value), nil)
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

func decryptSecret(value, secret string) (string, error) {
	data, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(secretKey(secret))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("密文无效")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func secretKey(secret string) []byte { digest := sha256.Sum256([]byte(secret)); return digest[:] }

func validateAIBaseURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("Base URL 无效")
	}
	if parsed.User != nil || parsed.Hostname() == "" || parsed.Path == "/" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("Base URL 必须是没有凭据和查询参数的绝对 URL")
	}
	allowLocal := strings.EqualFold(os.Getenv("DOMAINHUNTER_AI_ALLOW_INSECURE_LOCAL"), "true")
	if parsed.Scheme != "https" && !(allowLocal && parsed.Scheme == "http") {
		return fmt.Errorf("Base URL 必须使用 HTTPS；本机 HTTP 仅能由 DOMAINHUNTER_AI_ALLOW_INSECURE_LOCAL=true 显式开启")
	}
	host := strings.ToLower(parsed.Hostname())
	allowlist := splitCSV(os.Getenv("DOMAINHUNTER_AI_ALLOWED_HOSTS"))
	if len(allowlist) > 0 && !containsHost(allowlist, host) {
		return fmt.Errorf("Base URL 主机不在 DOMAINHUNTER_AI_ALLOWED_HOSTS allowlist 中")
	}
	if len(allowlist) == 0 && host != "api.deepseek.com" && !(allowLocal && isLocalHost(host)) {
		return fmt.Errorf("自定义 Base URL 必须配置 DOMAINHUNTER_AI_ALLOWED_HOSTS allowlist")
	}
	if isBlockedHost(host) && !(allowLocal && isLocalHost(host)) {
		return fmt.Errorf("Base URL 主机指向被禁止的本机、私有网络或元数据地址")
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("无法解析 Base URL 主机")
	}
	for _, ip := range ips {
		if isBlockedIP(ip) && !(allowLocal && ip.IsLoopback()) {
			return fmt.Errorf("Base URL DNS 解析包含被禁止的地址")
		}
	}
	return nil
}

func isLocalHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
func containsHost(values []string, host string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), host) {
			return true
		}
	}
	return false
}
func isBlockedHost(host string) bool {
	return isLocalHost(host) || host == "metadata.google.internal" || host == "169.254.169.254" || strings.HasSuffix(host, ".local")
}
func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.Equal(net.ParseIP("169.254.169.254"))
}

func (s *AIService) worker(ctx context.Context) {
	defer s.wg.Done()
	for {
		job, ok := s.claim(ctx)
		if ok {
			s.process(ctx, job)
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-time.After(2 * time.Second):
		}
	}
}

type aiJobRecord struct {
	ID               int64
	Domain           string
	InputFingerprint string
	InputJSON        string
	Attempts         int
	MaxAttempts      int
}

func (s *AIService) claim(ctx context.Context) (aiJobRecord, bool) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return aiJobRecord{}, false
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	_, _ = tx.ExecContext(ctx, `UPDATE ai_jobs SET status='queued', lease_until=NULL, updated_at=? WHERE status='running' AND lease_until IS NOT NULL AND lease_until < ?`, now, now)
	var job aiJobRecord
	err = tx.QueryRowContext(ctx, `SELECT id,domain,input_fingerprint,input_json,attempts,max_attempts FROM ai_jobs WHERE status IN ('queued','deferred') AND available_at <= ? AND (lease_until IS NULL OR lease_until < ?) ORDER BY id LIMIT 1`, now, now).Scan(&job.ID, &job.Domain, &job.InputFingerprint, &job.InputJSON, &job.Attempts, &job.MaxAttempts)
	if err != nil {
		return aiJobRecord{}, false
	}
	lease := now.Add(2 * time.Minute)
	if _, err = tx.ExecContext(ctx, `UPDATE ai_jobs SET status='running', attempts=attempts+1, lease_until=?, started_at=COALESCE(started_at,?), updated_at=? WHERE id=?`, lease, now, now, job.ID); err != nil {
		return aiJobRecord{}, false
	}
	if err = tx.Commit(); err != nil {
		return aiJobRecord{}, false
	}
	job.Attempts++
	return job, true
}

func (s *AIService) process(ctx context.Context, job aiJobRecord) {
	settings, encrypted, err := s.storedSettings(ctx)
	if err != nil {
		s.fail(job, "读取 AI 设置失败", false)
		return
	}
	key, source := configuredAPIKey(encrypted)
	if !settings.Enabled || key == "" || source == "encrypted_unavailable" {
		s.deferJob(job, "AI 未启用或未配置可用 API Key")
		return
	}
	if err := validateAIBaseURL(settings.BaseURL); err != nil {
		s.fail(job, "Base URL 安全校验失败", false)
		return
	}
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(settings.TimeoutSeconds)*time.Second)
	defer cancel()
	content, err := callOpenAICompatible(requestCtx, settings, key, job.InputJSON)
	if err != nil {
		s.fail(job, "AI Provider 请求失败", true)
		return
	}
	response, err := validateValuation(content)
	if err != nil {
		s.fail(job, "AI 返回未通过 JSON Schema 校验", true)
		return
	}
	now := time.Now().UTC()
	ttl := time.Duration(settings.CacheTTLSeconds) * time.Second
	valuation := Valuation{
		Domain: job.Domain, Provider: settings.Provider, Model: settings.Model,
		AnalysisVersion: analysisVersion, InputFingerprint: job.InputFingerprint,
		QualityScore: response.QualityScore, LiquidityScore: response.LiquidityScore,
		RiskLevel: response.RiskLevel, ValueLow: response.ValueLow, ValueHigh: response.ValueHigh,
		Confidence: response.Confidence, Strengths: response.Strengths, Limitations: response.Limitations,
		DataGaps: response.DataGaps, Rationale: response.Rationale, Disclaimer: response.Disclaimer,
		GeneratedAt: now, ExpiresAt: now.Add(ttl),
	}
	data, _ := json.Marshal(valuation)
	_, err = s.db.ExecContext(ctx, `INSERT INTO ai_domain_valuations(domain,job_id,provider,model,analysis_version,input_fingerprint,result_json,quality_score,liquidity_score,risk_level,value_low,value_high,confidence,generated_at,expires_at)
		SELECT ?,?,?,?,?,input_fingerprint,?,?,?,?,?,?,?,?,? FROM ai_jobs WHERE id=?
		ON CONFLICT(domain) DO UPDATE SET job_id=excluded.job_id,provider=excluded.provider,model=excluded.model,analysis_version=excluded.analysis_version,input_fingerprint=excluded.input_fingerprint,result_json=excluded.result_json,quality_score=excluded.quality_score,liquidity_score=excluded.liquidity_score,risk_level=excluded.risk_level,value_low=excluded.value_low,value_high=excluded.value_high,confidence=excluded.confidence,generated_at=excluded.generated_at,expires_at=excluded.expires_at`,
		job.Domain, job.ID, settings.Provider, settings.Model, analysisVersion, string(data), valuation.QualityScore, valuation.LiquidityScore,
		valuation.RiskLevel, valuation.ValueLow, valuation.ValueHigh, valuation.Confidence, now, now.Add(ttl), job.ID)
	if err != nil {
		s.fail(job, "保存估价结果失败", true)
		return
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE ai_jobs SET status='succeeded',lease_until=NULL,last_error='',completed_at=?,updated_at=? WHERE id=?`, now, now, job.ID)
}

func (s *AIService) deferJob(job aiJobRecord, message string) {
	next := time.Now().UTC().Add(5 * time.Minute)
	_, _ = s.db.Exec(`UPDATE ai_jobs SET status='deferred',lease_until=NULL,last_error=?,available_at=?,updated_at=? WHERE id=?`, message, next, time.Now().UTC(), job.ID)
}

func (s *AIService) fail(job aiJobRecord, message string, retry bool) {
	now := time.Now().UTC()
	if retry && job.Attempts < job.MaxAttempts {
		delay := time.Duration(1<<(job.Attempts-1)) * time.Minute
		_, _ = s.db.Exec(`UPDATE ai_jobs SET status='queued',lease_until=NULL,last_error=?,available_at=?,updated_at=? WHERE id=?`, message, now.Add(delay), now, job.ID)
		return
	}
	_, _ = s.db.Exec(`UPDATE ai_jobs SET status='failed',lease_until=NULL,last_error=?,completed_at=?,updated_at=? WHERE id=?`, message, now, now, job.ID)
}

func callOpenAICompatible(ctx context.Context, settings AISettingsInput, key, inputJSON string) (string, error) {
	schema := `{"analysis_version":"p1-valuation-v1","quality_score":0,"liquidity_score":0,"risk_level":"low|medium|high","value_low":0,"value_high":0,"confidence":"low|medium|high","strengths":["..."],"limitations":["..."],"data_gaps":["..."],"rationale":"...","disclaimer":"..."}`
	system := "你是域名研究助手。只输出严格 JSON，不要 Markdown、HTML 或自然语言前后缀。不得进行商标或法律结论，不得把研究性估价当成交保证或购买建议。输出必须满足 schema: " + schema
	body := map[string]any{"model": settings.Model, "temperature": 0.1, "max_tokens": settings.MaxOutputTokens, "messages": []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": inputJSON}}}
	encoded, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimRight(settings.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	client := aiHTTPClient(settings)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("provider status %d", resp.StatusCode)
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || len(envelope.Choices) == 0 {
		return "", errors.New("provider response missing choices")
	}
	return envelope.Choices[0].Message.Content, nil
}

func aiHTTPClient(settings AISettingsInput) *http.Client {
	allowLocal := strings.EqualFold(os.Getenv("DOMAINHUNTER_AI_ALLOW_INSECURE_LOCAL"), "true")
	return &http.Client{
		Timeout:       time.Duration(settings.TimeoutSeconds) * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			Proxy:       http.ProxyFromEnvironment,
			DialContext: safeAIDialContext(allowLocal),
		},
	}
}

func safeAIDialContext(allowLocal bool) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.LookupIP(host)
		if err != nil {
			return nil, err
		}
		var lastErr error
		for _, ip := range ips {
			if isBlockedIP(ip) && !(allowLocal && ip.IsLoopback()) {
				lastErr = fmt.Errorf("AI 目标地址被网络安全策略拒绝")
				continue
			}
			conn, dialErr := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("AI 目标地址没有可用 IP")
		}
		return nil, lastErr
	}
}

type valuationResponse struct {
	AnalysisVersion string   `json:"analysis_version"`
	QualityScore    int      `json:"quality_score"`
	LiquidityScore  int      `json:"liquidity_score"`
	RiskLevel       string   `json:"risk_level"`
	ValueLow        float64  `json:"value_low"`
	ValueHigh       float64  `json:"value_high"`
	Confidence      string   `json:"confidence"`
	Strengths       []string `json:"strengths"`
	Limitations     []string `json:"limitations"`
	DataGaps        []string `json:"data_gaps"`
	Rationale       string   `json:"rationale"`
	Disclaimer      string   `json:"disclaimer"`
}

func validateValuation(content string) (valuationResponse, error) {
	if strings.TrimSpace(content) == "" || strings.ContainsAny(content, "<>") {
		return valuationResponse{}, errors.New("空或 HTML 响应")
	}
	var value valuationResponse
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return valuationResponse{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return valuationResponse{}, errors.New("估价响应包含多余内容")
	}
	if value.AnalysisVersion == "" || value.QualityScore < 0 || value.QualityScore > 100 || value.LiquidityScore < 0 || value.LiquidityScore > 100 || value.ValueLow < 0 || value.ValueHigh < value.ValueLow || value.ValueHigh > 1e9 || value.Rationale == "" || value.Disclaimer == "" {
		return valuationResponse{}, errors.New("估价字段缺失或超出范围")
	}
	if value.Strengths == nil || value.Limitations == nil || value.DataGaps == nil {
		return valuationResponse{}, errors.New("估价数组字段缺失")
	}
	if value.RiskLevel != "low" && value.RiskLevel != "medium" && value.RiskLevel != "high" {
		return valuationResponse{}, errors.New("risk_level 无效")
	}
	if value.Confidence != "low" && value.Confidence != "medium" && value.Confidence != "high" {
		return valuationResponse{}, errors.New("confidence 无效")
	}
	return value, nil
}

func (s *AIService) inputForDomain(ctx context.Context, name string) (string, string, error) {
	var rawName, status, registrar, method, created, expiry, updated, lastChecked, tags string
	var priority int
	err := s.db.QueryRowContext(ctx, `SELECT d.name,COALESCE(r.status,''),COALESCE(r.registrar,''),COALESCE(r.query_method,''),COALESCE(r.created_at,''),COALESCE(r.expiry_at,''),COALESCE(r.updated_at,''),COALESCE(r.last_checked,''),COALESCE(d.tags,''),COALESCE(d.priority,0) FROM domains d LEFT JOIN domain_results r ON lower(r.domain)=lower(d.name) WHERE lower(d.name)=lower(?)`, name).Scan(&rawName, &status, &registrar, &method, &created, &expiry, &updated, &lastChecked, &tags, &priority)
	if err != nil {
		return "", "", err
	}
	features := map[string]any{"length": len(strings.ReplaceAll(rawName, ".", "")), "labels": len(strings.Split(rawName, ".")), "hyphen": strings.Count(rawName, "-"), "digits": countDigits(rawName), "punycode": strings.HasPrefix(strings.ToLower(rawName), "xn--")}
	input := map[string]any{"domain": rawName, "tld": tldOf(rawName), "character_features": features, "confirmed_status": status, "confidence": method, "dates": map[string]string{"created": created, "expiry": expiry, "updated": updated, "last_checked": lastChecked}, "registrar": registrar}
	if cleaned := splitCSV(tags); len(cleaned) > 0 {
		input["tags"] = cleaned
	}
	if priority > 0 {
		input["priority"] = priority
	}
	data, _ := json.Marshal(input)
	sum := sha256.Sum256(data)
	return string(data), fmt.Sprintf("%x", sum[:]), nil
}
func countDigits(value string) int {
	count := 0
	for _, r := range value {
		if r >= '0' && r <= '9' {
			count++
		}
	}
	return count
}

func (s *AIService) Enqueue(ctx context.Context, names []string) (int, error) {
	if len(names) > 100 {
		return 0, fmt.Errorf("单次最多创建100个 AI 任务")
	}
	seen := map[string]bool{}
	queued := 0
	now := time.Now().UTC()
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		input, fingerprint, err := s.inputForDomain(ctx, name)
		if err != nil {
			continue
		}
		var exists int
		_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_domain_valuations WHERE lower(domain)=lower(?) AND input_fingerprint=? AND expires_at>?`, name, fingerprint, now).Scan(&exists)
		if exists > 0 {
			continue
		}
		var active int
		_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_jobs WHERE lower(domain)=lower(?) AND input_fingerprint=? AND status IN ('queued','running','deferred')`, name, fingerprint).Scan(&active)
		if active > 0 {
			continue
		}
		_, err = s.db.ExecContext(ctx, `INSERT INTO ai_jobs(domain,status,input_fingerprint,input_json,available_at) VALUES(?,?,?,?,?)`, name, "queued", fingerprint, input, now)
		if err == nil {
			queued++
		}
	}
	s.signal()
	return queued, nil
}

func (s *AIService) Usage(ctx context.Context) (AIUsage, error) {
	var usage AIUsage
	usage.Date = time.Now().UTC().Format("2006-01-02")
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_jobs WHERE created_at >= date('now') AND status IN ('queued','running','succeeded','failed')`).Scan(&usage.Used)
	if err != nil {
		return usage, err
	}
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_jobs WHERE status='queued' OR status='deferred'`).Scan(&usage.Queued)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_jobs WHERE status='running'`).Scan(&usage.Running)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_jobs WHERE status='succeeded' AND completed_at >= date('now')`).Scan(&usage.Succeeded)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_jobs WHERE status='failed' AND completed_at >= date('now')`).Scan(&usage.Failed)
	return usage, nil
}

func (s *AIService) ListJobs(ctx context.Context, limit int) ([]AIJob, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,domain,status,attempts,max_attempts,last_error,created_at,updated_at,started_at,completed_at FROM ai_jobs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AIJob
	for rows.Next() {
		var j AIJob
		var started, completed sql.NullTime
		if err := rows.Scan(&j.ID, &j.Domain, &j.Status, &j.Attempts, &j.MaxAttempts, &j.LastError, &j.CreatedAt, &j.UpdatedAt, &started, &completed); err != nil {
			return nil, err
		}
		if started.Valid {
			j.StartedAt = &started.Time
		}
		if completed.Valid {
			j.CompletedAt = &completed.Time
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *AIService) GetValuation(ctx context.Context, name string) (*Valuation, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT result_json FROM ai_domain_valuations WHERE lower(domain)=lower(?)`, name).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var value Valuation
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, err
	}
	return &value, nil
}
func (s *AIService) CancelJob(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE ai_jobs SET status='cancelled',lease_until=NULL,completed_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE id=? AND status IN ('queued','deferred','running')`, id)
	return err
}

func (s *AIService) ListModels(ctx context.Context) ([]string, error) {
	settings, encrypted, err := s.storedSettings(ctx)
	if err != nil {
		return nil, err
	}
	key, _ := configuredAPIKey(encrypted)
	if key == "" {
		return []string{settings.Model}, nil
	}
	if err := validateAIBaseURL(settings.BaseURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(settings.BaseURL, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := aiHTTPClient(settings).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("provider status %d", resp.StatusCode)
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(body.Data))
	for _, item := range body.Data {
		if item.ID != "" {
			out = append(out, item.ID)
		}
	}
	if len(out) == 0 {
		out = []string{settings.Model}
	}
	return out, nil
}
