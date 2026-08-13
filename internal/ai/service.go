package ai

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/repository"
)

var (
	ErrProfileNotFound = errors.New("AI 档案不存在")
	ErrProfileDisabled = errors.New("AI 档案未启用")
	ErrIneligible      = errors.New("当前域名状态或证据不足，暂不能加入 AI 估价")
)

type Service struct {
	store     Store
	domains   repository.DomainRepository
	results   repository.ResultRepository
	client    Client
	encrypt   *Encryptor
	urlPolicy BaseURLPolicy
	clock     func() time.Time
}

func NewService(store Store, domains repository.DomainRepository, results repository.ResultRepository, client Client, encrypt *Encryptor, urlPolicy BaseURLPolicy) *Service {
	return &Service{store: store, domains: domains, results: results, client: client, encrypt: encrypt, urlPolicy: urlPolicy, clock: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) Policy() Policy { return DefaultPolicy() }
func (s *Service) ListProfiles(ctx context.Context) ([]Profile, error) {
	return s.store.ListProfiles(ctx)
}

// EnsureDefaultProfile seeds a visible OpenAI-compatible profile exactly once. It never overwrites
// an existing default and does not require an API key at startup.
func (s *Service) EnsureDefaultProfile(ctx context.Context) error {
	existing, err := s.store.GetDefaultProfile(ctx)
	if err != nil {
		return err
	}
	if existing != nil {
		defaults := DefaultDeepSeekProfile()
		if existing.IsDefault && existing.BaseURLHost == "opencode.ai" && existing.Model == "deepseek-v4-flash-free" {
			defaults.Enabled = existing.Enabled
			defaults.IsDefault = true
			_, err = s.SaveProfile(ctx, existing.ID, defaults, "system")
			return err
		}
		return nil
	}
	_, err = s.SaveProfile(ctx, "", DefaultDeepSeekProfile(), "system")
	return err
}
func (s *Service) GetCurrent(ctx context.Context, name string) (*Job, error) {
	return s.store.GetLatestJob(ctx, domain.Normalize(name))
}
func (s *Service) GetJob(ctx context.Context, id string) (*Job, error) {
	return s.store.GetJob(ctx, id)
}

func (s *Service) SaveProfile(ctx context.Context, id string, input ProfileInput, actor string) (*Profile, error) {
	if err := validateProfileInput(input); err != nil {
		return nil, err
	}
	// Daily valuation limits are intentionally disabled. Keep the legacy field
	// at zero when writing older-compatible SQLite schemas.
	input.DailyLimit = 0
	endpoint, err := NormalizeBaseURL(ctx, input.BaseURL, s.urlPolicy)
	if err != nil {
		return nil, err
	}
	baseURL := rootURL(endpoint)
	now := s.clock()
	var existing *ProfileRecord
	if id != "" {
		existing, err = s.store.GetProfileRecord(ctx, id)
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrProfileNotFound) {
			return nil, ErrProfileNotFound
		}
		if err != nil {
			return nil, err
		}
		if existing == nil {
			return nil, ErrProfileNotFound
		}
	}
	record := ProfileRecord{Profile: Profile{
		ID: id, Name: strings.TrimSpace(input.Name), Provider: input.Provider, Enabled: input.Enabled, IsDefault: input.IsDefault,
		BaseURL: baseURL, BaseURLHost: endpoint.Hostname(), Model: strings.TrimSpace(input.Model), ThinkingType: input.ThinkingType,
		ReasoningEffort: input.ReasoningEffort, TimeoutSeconds: input.TimeoutSeconds, MaxTokens: input.MaxTokens,
		Concurrency: input.Concurrency, DailyLimit: input.DailyLimit, CacheTTLHours: input.CacheTTLHours, CreatedAt: now, UpdatedAt: now,
	}}
	updateSecret := false
	if existing != nil {
		record.CreatedAt = existing.CreatedAt
		record.APIKeyCiphertext = existing.APIKeyCiphertext
		record.APIKeySource = existing.APIKeySource
		record.LastTestedAt, record.LastTestLatencyMS, record.LastError = existing.LastTestedAt, existing.LastTestLatencyMS, existing.LastError
	}
	if strings.TrimSpace(input.APIKey) != "" {
		if s.encrypt == nil {
			return nil, ErrSecretKeyRequired
		}
		ciphertext, sealErr := s.encrypt.Seal(strings.TrimSpace(input.APIKey))
		if sealErr != nil {
			return nil, sealErr
		}
		record.APIKeyCiphertext, record.APIKeySource, updateSecret = ciphertext, SecretSourceEncrypted, true
	} else if record.APIKeyCiphertext == "" && strings.TrimSpace(os.Getenv("DOMAINHUNTER_AI_API_KEY")) != "" {
		record.APIKeySource = SecretSourceEnv
	}
	if existing == nil {
		record.ID = newID("aip")
		profile, createErr := s.store.CreateProfile(ctx, record)
		if createErr == nil {
			_ = s.store.Audit(ctx, "ai_profile_created", "", profile.ID, "", actor, map[string]any{"provider": profile.Provider, "host": profile.BaseURLHost})
		}
		return profile, createErr
	}
	profile, updateErr := s.store.UpdateProfile(ctx, record, updateSecret)
	if updateErr == nil {
		_ = s.store.Audit(ctx, "ai_profile_updated", "", profile.ID, "", actor, map[string]any{"provider": profile.Provider, "host": profile.BaseURLHost})
	}
	return profile, updateErr
}

func (s *Service) DeleteProfile(ctx context.Context, id, actor string) error {
	if err := s.store.DeleteProfile(ctx, id); err != nil {
		return err
	}
	return s.store.Audit(ctx, "ai_profile_deleted", "", id, "", actor, map[string]any{})
}

func (s *Service) TestConnection(ctx context.Context, input ProfileInput) (ConnectionTestResult, error) {
	if err := validateProfileInput(input); err != nil {
		return ConnectionTestResult{}, err
	}
	endpoint, err := NormalizeBaseURL(ctx, input.BaseURL, s.urlPolicy)
	if err != nil {
		return ConnectionTestResult{}, err
	}
	key := strings.TrimSpace(input.APIKey)
	if key == "" {
		key = strings.TrimSpace(os.Getenv("DOMAINHUNTER_AI_API_KEY"))
	}
	if key == "" {
		return ConnectionTestResult{OK: false, Status: ProfileNotConfigured, Message: "未配置 API Key；请使用环境变量或配置应用级加密。"}, nil
	}
	profile := Profile{Provider: input.Provider, BaseURL: rootURL(endpoint), Model: input.Model, ThinkingType: input.ThinkingType, ReasoningEffort: input.ReasoningEffort, TimeoutSeconds: input.TimeoutSeconds, MaxTokens: 60}
	inputData := SanitizedInput{Domain: "connection-test.example", TLD: ".example"}
	inputData.SystemFacts.Status = "registered"
	inputData.SystemFacts.Confidence = "high"
	inputData.SystemFacts.ProviderConsensus = "test"
	_, latency, evalErr := s.client.Evaluate(ctx, profile, key, inputData)
	if evalErr != nil {
		return ConnectionTestResult{OK: false, Status: ProfileDegraded, NormalizedBaseURL: rootURL(endpoint), Message: safeError(evalErr)}, nil
	}
	return ConnectionTestResult{OK: true, Status: ProfileReady, LatencyMS: &latency, Model: input.Model, NormalizedBaseURL: rootURL(endpoint), Message: "连接测试成功"}, nil
}

func (s *Service) Enqueue(ctx context.Context, name string, input EnqueueInput, actor string) (*Job, error) {
	name = domain.Normalize(name)
	if name == "" {
		return nil, ErrIneligible
	}
	info, err := s.results.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	watched, err := s.domains.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if info == nil || watched == nil || !eligible(info) {
		return nil, ErrIneligible
	}
	review := domain.BuildReviewState(info, s.clock())
	if review != nil && review.Required {
		return nil, ErrIneligible
	}
	info.Review = review
	var profile *ProfileRecord
	if input.ProfileID != "" {
		profile, err = s.store.GetProfileRecord(ctx, input.ProfileID)
		if err != nil {
			return nil, err
		}
	} else {
		profile, err = s.store.GetDefaultProfile(ctx)
	}
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, ErrProfileNotFound
	}
	if !profile.Enabled {
		return nil, ErrProfileDisabled
	}
	key, err := s.resolveAPIKey(*profile)
	if err != nil {
		return nil, err
	}
	if key == "" {
		return nil, ErrSecretKeyRequired
	}
	_ = key // Key is resolved here to produce a user-visible configuration error before enqueuing; worker resolves it again.
	sanitized := SanitizeInput(*watched, *info)
	fingerprint, err := fingerprint(sanitized)
	if err != nil {
		return nil, err
	}
	now := s.clock()
	if !input.ForceRefresh {
		cached, cacheErr := s.store.GetFreshValuation(ctx, name, profile.ID, fingerprint, PromptVersion, now)
		if cacheErr != nil {
			return nil, cacheErr
		}
		if cached != nil {
			return &Job{ID: "cache:" + cached.ID, Domain: name, State: JobSucceeded, ProfileID: profile.ID, Priority: input.Priority, QueuedAt: cached.CreatedAt, CompletedAt: &cached.CreatedAt, Result: cached, Cached: true}, nil
		}
	}
	priority := input.Priority
	if priority != PriorityHigh {
		priority = PriorityNormal
	}
	job := Job{ID: newID("aij"), Domain: name, State: JobQueued, ProfileID: profile.ID, Priority: priority, QueuedAt: now}
	created, err := s.store.CreateJob(ctx, job, fingerprint, PromptVersion, newID("cause"))
	if err != nil {
		return nil, err
	}
	if err = s.store.Audit(ctx, "ai_valuation_enqueued", name, profile.ID, created.ID, actor, map[string]any{"priority": priority, "force_refresh": input.ForceRefresh}); err != nil {
		return nil, err
	}
	return created, nil
}

// EnqueueBatch 将一组域名逐个放入同一持久化队列。
// 单域名资格问题会收集到 errors，不会让其它域名整批回滚；日额度不参与此流程。
func (s *Service) EnqueueBatch(ctx context.Context, names []string, input EnqueueInput, actor string) (*BatchEnqueueResult, error) {
	result := &BatchEnqueueResult{}
	seen := make(map[string]struct{}, len(names))
	for _, raw := range names {
		name := domain.Normalize(raw)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result.Requested++
		job, err := s.Enqueue(ctx, name, input, actor)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, BatchEnqueueError{Domain: name, Error: safeError(err)})
			continue
		}
		if job.Cached {
			result.Cached++
		} else {
			result.Queued++
		}
		result.Jobs = append(result.Jobs, job)
	}
	return result, nil
}

func (s *Service) Cancel(ctx context.Context, jobID, actor string) (*Job, error) {
	job, err := s.store.CancelJob(ctx, jobID, s.clock())
	if err != nil {
		return nil, err
	}
	_ = s.store.Audit(ctx, "ai_valuation_cancelled", job.Domain, job.ProfileID, job.ID, actor, map[string]any{})
	return job, nil
}

func (s *Service) RetryNow(ctx context.Context, jobID, actor string) (*Job, error) {
	job, err := s.store.RetryJobNow(ctx, jobID, s.clock())
	if err != nil {
		return nil, err
	}
	_ = s.store.Audit(ctx, "ai_valuation_retried", job.Domain, job.ProfileID, job.ID, actor, map[string]any{"manual": true})
	return job, nil
}

// ProcessOne is called by the persistent worker. It handles one claimed task and returns whether work existed.
func (s *Service) ProcessOne(ctx context.Context) (bool, error) {
	now := s.clock()
	// Profile timeout can be configured up to 120 seconds; keep a larger lease
	// than the maximum provider call so a slow request cannot be claimed twice.
	claim, err := s.store.ClaimNextJob(ctx, now, 180*time.Second)
	if err != nil {
		return false, err
	}
	if claim == nil {
		return false, nil
	}
	job, profile := claim.Job, claim.Profile
	key, err := s.resolveAPIKey(profile)
	if err != nil {
		_ = s.store.FailJob(ctx, job.ID, "key_unavailable", safeError(err), nil, now)
		return true, nil
	}
	info, err := s.results.Get(ctx, job.Domain)
	if err != nil || info == nil {
		_ = s.store.FailJob(ctx, job.ID, "domain_result_missing", "域名最新查询结果不可用", nil, now)
		return true, nil
	}
	watched, err := s.domains.Get(ctx, job.Domain)
	if err != nil || watched == nil || !eligible(info) {
		_ = s.store.FailJob(ctx, job.ID, "domain_ineligible", "域名状态或证据已不满足估价条件", nil, now)
		return true, nil
	}
	review := domain.BuildReviewState(info, now)
	if review != nil && review.Required {
		_ = s.store.FailJob(ctx, job.ID, "domain_ineligible", "域名状态或证据已不满足估价条件", nil, now)
		return true, nil
	}
	info.Review = review
	input := SanitizeInput(*watched, *info)
	output, _, err := s.client.Evaluate(ctx, profile.Profile, key, input)
	if err != nil {
		errorCode := "provider_error"
		retry := retryAfter(job, now)
		if errors.Is(err, ErrProviderAuth) {
			errorCode = "provider_auth"
			retry = nil
		} else if errors.Is(err, ErrProviderConfig) {
			errorCode = "provider_config"
			retry = nil
		}
		_ = s.store.FailJob(ctx, job.ID, errorCode, safeError(err), retry, now)
		return true, nil
	}
	finger, _ := fingerprint(input)
	expires := now.Add(time.Duration(profile.CacheTTLHours) * time.Hour)
	score := output.Score
	if score == 0 && output.QualityScore > 0 {
		score = output.QualityScore
	}
	valuation := Valuation{ID: newID("aiv"), Domain: job.Domain, ProfileID: profile.ID, ProfileName: profile.Name, Provider: string(profile.Provider), Model: profile.Model, PromptVersion: PromptVersion, InputFingerprint: finger, Score: score, QualityScore: score, LiquidityScore: output.LiquidityScore, RiskLevel: output.RiskLevel, Confidence: output.Confidence, IndicativeValueUSD: output.IndicativeValueUSD, PriceEvaluationCNY: output.PriceEvaluationCNY, Summary: output.Summary, CoreAnalysis: output.CoreAnalysis, Strengths: output.Strengths, Risks: output.Risks, DataGaps: output.DataGaps, EvidenceUsed: output.EvidenceUsed, StatusGuard: output.StatusGuard, Disclaimer: output.Disclaimer, CreatedAt: now, ExpiresAt: &expires}
	if err = s.store.CompleteJob(ctx, job.ID, valuation, now); err != nil {
		return true, err
	}
	_ = s.store.Audit(ctx, "ai_valuation_completed", job.Domain, profile.ID, job.ID, "worker", map[string]any{"model": profile.Model})
	return true, nil
}

func (s *Service) RecoverLeases(ctx context.Context) (int64, error) {
	return s.store.RecoverExpiredLeases(ctx, s.clock())
}

func (s *Service) resolveAPIKey(profile ProfileRecord) (string, error) {
	if profile.APIKeySource == SecretSourceEnv || (profile.APIKeyCiphertext == "" && os.Getenv("DOMAINHUNTER_AI_API_KEY") != "") {
		return strings.TrimSpace(os.Getenv("DOMAINHUNTER_AI_API_KEY")), nil
	}
	if profile.APIKeyCiphertext == "" {
		return "", ErrSecretKeyRequired
	}
	if s.encrypt == nil {
		return "", ErrSecretKeyRequired
	}
	return s.encrypt.Open(profile.APIKeyCiphertext)
}

func SanitizeInput(watched domain.Domain, info domain.Info) SanitizedInput {
	var in SanitizedInput
	in.Domain = domain.Normalize(info.Name)
	parts := strings.Split(in.Domain, ".")
	if len(parts) > 1 {
		in.TLD = "." + parts[len(parts)-1]
	}
	in.Lexical.Length = len([]rune(parts[0]))
	in.Lexical.HasHyphen = strings.Contains(parts[0], "-")
	in.Lexical.HasDigits = strings.IndexFunc(parts[0], func(r rune) bool { return r >= '0' && r <= '9' }) >= 0
	in.Lexical.IsIDN = strings.HasPrefix(in.Domain, "xn--")
	in.SystemFacts.Status = string(info.Status)
	in.SystemFacts.Confidence = string(info.Confidence)
	if info.Review != nil {
		in.SystemFacts.ReviewRequired = info.Review.Required
		for _, reason := range info.Review.Reasons {
			in.SystemFacts.ReviewReasons = append(in.SystemFacts.ReviewReasons, string(reason))
		}
	}
	in.SystemFacts.Registrar = safeText(info.Registrar, 80)
	in.SystemFacts.EPPStatuses = sanitizeStrings(info.EPPStatuses, 12, 80)
	in.SystemFacts.ProviderConsensus = "single_result"
	if info.ExpiryDate != nil {
		value := info.ExpiryDate.UTC().Format(time.RFC3339)
		in.SystemFacts.ExpiryDate = &value
	}
	in.UserMetadata.Tags = sanitizeStrings(watched.Tags, 8, 40)
	in.UserMetadata.Priority = watched.Priority
	return in
}

func eligible(info *domain.Info) bool {
	if info == nil || (info.Confidence != domain.ConfidenceHigh && info.Confidence != domain.ConfidenceMedium) {
		return false
	}
	switch info.Status {
	case domain.StatusUnknown, domain.StatusError, domain.StatusSkipped:
		return false
	}
	return true
}
func sanitizeStrings(in []string, max, count int) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = safeText(v, count)
		if v != "" {
			out = append(out, v)
		}
		if len(out) >= max {
			break
		}
	}
	return out
}
func safeText(value string, max int) string {
	value = strings.TrimSpace(value)
	if len([]rune(value)) > max {
		return string([]rune(value)[:max])
	}
	return value
}
func fingerprint(input SanitizedInput) (string, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func rootURL(endpoint *url.URL) string {
	path := strings.TrimSuffix(endpoint.Path, "/chat/completions")
	return endpoint.Scheme + "://" + endpoint.Host + path
}
func retryAfter(job Job, now time.Time) *time.Time {
	if job.Priority == PriorityHigh {
		return nil
	}
	value := now.Add(5 * time.Minute)
	return &value
}
func safeError(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if len([]rune(text)) > 180 {
		text = string([]rune(text)[:180])
	}
	return text
}
func newID(prefix string) string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())))
		return prefix + "_" + hex.EncodeToString(sum[:12])
	}
	return prefix + "_" + hex.EncodeToString(buf)
}
