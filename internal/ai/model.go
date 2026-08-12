// Package ai implements DomainHunter's server-side, research-only AI valuation workflow.
// It deliberately contains no browser-facing API keys and never changes query status.
package ai

import (
	"time"
)

type ProviderKind string

const (
	ProviderDeepSeek         ProviderKind = "deepseek"
	ProviderOpenAICompatible ProviderKind = "openai_compatible"
)

type ProfileStatus string

const (
	ProfileReady         ProfileStatus = "ready"
	ProfileNotConfigured ProfileStatus = "not_configured"
	ProfileDegraded      ProfileStatus = "degraded"
	ProfileDisabled      ProfileStatus = "disabled"
)

type SecretSource string

const (
	SecretSourceNone      SecretSource = "none"
	SecretSourceEnv       SecretSource = "environment"
	SecretSourceEncrypted SecretSource = "encrypted_store"
)

type ThinkingType string

const (
	ThinkingDisabled ThinkingType = "disabled"
	ThinkingEnabled  ThinkingType = "enabled"
)

type ReasoningEffort string

const (
	ReasoningLow  ReasoningEffort = "low"
	ReasoningHigh ReasoningEffort = "high"
	ReasoningMax  ReasoningEffort = "max"
)

// Profile is the safe response model. APIKey is never serialized or returned.
type Profile struct {
	ID                string          `json:"id"`
	Name              string          `json:"name"`
	Provider          ProviderKind    `json:"provider"`
	Enabled           bool            `json:"enabled"`
	IsDefault         bool            `json:"is_default"`
	Status            ProfileStatus   `json:"status"`
	BaseURL           string          `json:"base_url"`
	BaseURLHost       string          `json:"base_url_host"`
	Model             string          `json:"model"`
	APIKeySet         bool            `json:"api_key_set"`
	APIKeySource      SecretSource    `json:"api_key_source"`
	ThinkingType      ThinkingType    `json:"thinking_type"`
	ReasoningEffort   ReasoningEffort `json:"reasoning_effort"`
	TimeoutSeconds    int             `json:"timeout_seconds"`
	MaxTokens         int             `json:"max_tokens"`
	Concurrency       int             `json:"concurrency"`
	DailyLimit        int             `json:"daily_limit"`
	CacheTTLHours     int             `json:"cache_ttl_hours"`
	LastTestedAt      *time.Time      `json:"last_tested_at,omitempty"`
	LastTestLatencyMS *int64          `json:"last_test_latency_ms,omitempty"`
	LastError         string          `json:"last_error,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

// ProfileInput accepts a write-only API key. An empty APIKey means retain an existing key.
type ProfileInput struct {
	Name            string          `json:"name"`
	Provider        ProviderKind    `json:"provider"`
	Enabled         bool            `json:"enabled"`
	IsDefault       bool            `json:"is_default"`
	BaseURL         string          `json:"base_url"`
	Model           string          `json:"model"`
	APIKey          string          `json:"api_key,omitempty"`
	ThinkingType    ThinkingType    `json:"thinking_type"`
	ReasoningEffort ReasoningEffort `json:"reasoning_effort"`
	TimeoutSeconds  int             `json:"timeout_seconds"`
	MaxTokens       int             `json:"max_tokens"`
	Concurrency     int             `json:"concurrency"`
	DailyLimit      int             `json:"daily_limit"`
	CacheTTLHours   int             `json:"cache_ttl_hours"`
}

type JobState string

const (
	JobQueued    JobState = "queued"
	JobRunning   JobState = "running"
	JobSucceeded JobState = "succeeded"
	JobFailed    JobState = "failed"
	JobCancelled JobState = "cancelled"
	JobDeferred  JobState = "deferred"
)

type JobPriority string

const (
	PriorityNormal JobPriority = "normal"
	PriorityHigh   JobPriority = "high"
)

type ValueRange struct {
	Low      int64  `json:"low"`
	High     int64  `json:"high"`
	Currency string `json:"currency"`
}

type Valuation struct {
	ID                 string      `json:"id"`
	Domain             string      `json:"domain"`
	ProfileID          string      `json:"profile_id"`
	ProfileName        string      `json:"profile_name"`
	Provider           string      `json:"provider"`
	Model              string      `json:"model"`
	PromptVersion      string      `json:"prompt_version"`
	InputFingerprint   string      `json:"input_fingerprint"`
	QualityScore       int         `json:"quality_score"`
	LiquidityScore     int         `json:"liquidity_score"`
	RiskLevel          string      `json:"risk_level"`
	Confidence         string      `json:"confidence"`
	IndicativeValueUSD *ValueRange `json:"indicative_value_usd,omitempty"`
	Summary            string      `json:"summary"`
	Strengths          []string    `json:"strengths"`
	Risks              []string    `json:"risks"`
	DataGaps           []string    `json:"data_gaps"`
	EvidenceUsed       []string    `json:"evidence_used"`
	StatusGuard        string      `json:"status_guard"`
	Disclaimer         string      `json:"disclaimer"`
	CreatedAt          time.Time   `json:"created_at"`
	ExpiresAt          *time.Time  `json:"expires_at,omitempty"`
}

type Quota struct {
	UsedToday      int `json:"used_today"`
	DailyLimit     int `json:"daily_limit"`
	RemainingToday int `json:"remaining_today"`
}

type Job struct {
	ID           string      `json:"id"`
	Domain       string      `json:"domain"`
	State        JobState    `json:"state"`
	ProfileID    string      `json:"profile_id"`
	Priority     JobPriority `json:"priority"`
	QueuedAt     time.Time   `json:"queued_at"`
	StartedAt    *time.Time  `json:"started_at,omitempty"`
	CompletedAt  *time.Time  `json:"completed_at,omitempty"`
	Result       *Valuation  `json:"result,omitempty"`
	RetryAfter   *time.Time  `json:"retry_after,omitempty"`
	ErrorCode    string      `json:"error_code,omitempty"`
	ErrorMessage string      `json:"error_message,omitempty"`
	Cached       bool        `json:"cached"`
	Quota        *Quota      `json:"quota,omitempty"`
}

type EnqueueInput struct {
	ProfileID    string      `json:"profile_id,omitempty"`
	Priority     JobPriority `json:"priority,omitempty"`
	ForceRefresh bool        `json:"force_refresh,omitempty"`
}

type ConnectionTestResult struct {
	OK                bool          `json:"ok"`
	Status            ProfileStatus `json:"status"`
	LatencyMS         *int64        `json:"latency_ms,omitempty"`
	Model             string        `json:"model,omitempty"`
	NormalizedBaseURL string        `json:"normalized_base_url,omitempty"`
	Message           string        `json:"message"`
}

type Policy struct {
	StatusGuard            string `json:"status_guard"`
	ResearchOnlyDisclaimer string `json:"research_only_disclaimer"`
	RawWHOISSent           bool   `json:"raw_whois_sent"`
	UserNoteSent           bool   `json:"user_note_sent"`
}

func DefaultPolicy() Policy {
	return Policy{
		StatusGuard:            "域名状态、可信度和可注册结论由 DomainHunter 查询链路决定；AI 仅辅助研究性排序与解释。",
		ResearchOnlyDisclaimer: "仅供研究性排序与解释，不构成估值、投资、购买或法律建议。",
		RawWHOISSent:           false,
		UserNoteSent:           false,
	}
}

func DefaultDeepSeekProfile() ProfileInput {
	return ProfileInput{
		Name:            "DeepSeek 官方 · 研究性估价",
		Provider:        ProviderDeepSeek,
		Enabled:         true,
		IsDefault:       true,
		BaseURL:         "https://api.deepseek.com",
		Model:           "deepseek-v4-flash",
		ThinkingType:    ThinkingDisabled,
		ReasoningEffort: ReasoningLow,
		TimeoutSeconds:  30,
		MaxTokens:       900,
		Concurrency:     1,
		DailyLimit:      50,
		CacheTTLHours:   24,
	}
}
