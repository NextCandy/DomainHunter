package p1

import (
	"encoding/json"
	"time"

	"DomainHunter/internal/domain"
)

// FilterNode 是可刷新、可分享的版本化筛选树。没有把它暴露成任意 SQL/DSL。
type FilterNode struct {
	Version    int          `json:"version,omitempty"`
	Logic      string       `json:"logic,omitempty"`
	Conditions []FilterNode `json:"conditions,omitempty"`
	Field      string       `json:"field,omitempty"`
	Op         string       `json:"op,omitempty"`
	Value      any          `json:"value,omitempty"`
}

type SavedView struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Filter    FilterNode `json:"filter"`
	Shared    bool       `json:"shared"`
	CreatedBy string     `json:"created_by,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type DomainListResult struct {
	Domains       []*domain.Info `json:"domains"`
	Total         int            `json:"total"`
	TotalFiltered int            `json:"total_filtered"`
	Page          int            `json:"page"`
	Limit         int            `json:"limit"`
	TotalPages    int            `json:"total_pages"`
	HasNext       bool           `json:"has_next"`
	HasPrev       bool           `json:"has_prev"`
	DataStatus    string         `json:"data_status"`
}

type AISettingsInput struct {
	Provider        string `json:"provider"`
	BaseURL         string `json:"base_url"`
	Model           string `json:"model"`
	APIKey          string `json:"api_key,omitempty"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	Concurrency     int    `json:"concurrency"`
	MaxOutputTokens int    `json:"max_output_tokens"`
	DailyLimit      int    `json:"daily_limit"`
	CacheTTLSeconds int    `json:"cache_ttl_seconds"`
	Enabled         bool   `json:"enabled"`
}

type AISettingsPublic struct {
	Provider        string `json:"provider"`
	BaseURL         string `json:"base_url"`
	Model           string `json:"model"`
	APIKeySet       bool   `json:"api_key_set"`
	KeySource       string `json:"key_source"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	Concurrency     int    `json:"concurrency"`
	MaxOutputTokens int    `json:"max_output_tokens"`
	DailyLimit      int    `json:"daily_limit"`
	CacheTTLSeconds int    `json:"cache_ttl_seconds"`
	Enabled         bool   `json:"enabled"`
}

type AIJob struct {
	ID          int64      `json:"id"`
	Domain      string     `json:"domain"`
	Status      string     `json:"status"`
	Attempts    int        `json:"attempts"`
	MaxAttempts int        `json:"max_attempts"`
	LastError   string     `json:"last_error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type Valuation struct {
	Domain           string    `json:"domain"`
	Provider         string    `json:"provider"`
	Model            string    `json:"model"`
	AnalysisVersion  string    `json:"analysis_version"`
	InputFingerprint string    `json:"input_fingerprint"`
	QualityScore     int       `json:"quality_score"`
	LiquidityScore   int       `json:"liquidity_score"`
	RiskLevel        string    `json:"risk_level"`
	ValueLow         float64   `json:"value_low"`
	ValueHigh        float64   `json:"value_high"`
	Confidence       string    `json:"confidence"`
	Strengths        []string  `json:"strengths"`
	Limitations      []string  `json:"limitations"`
	DataGaps         []string  `json:"data_gaps"`
	Rationale        string    `json:"rationale"`
	Disclaimer       string    `json:"disclaimer"`
	GeneratedAt      time.Time `json:"generated_at"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type AIUsage struct {
	Date       string `json:"date"`
	DailyLimit int    `json:"daily_limit"`
	Used       int    `json:"used"`
	Queued     int    `json:"queued"`
	Running    int    `json:"running"`
	Succeeded  int    `json:"succeeded"`
	Failed     int    `json:"failed"`
}

type BulkAction struct {
	Type     string     `json:"type"`
	Tag      string     `json:"tag,omitempty"`
	Priority *int       `json:"priority,omitempty"`
	FolderID *int64     `json:"folder_id,omitempty"`
	Enabled  *bool      `json:"enabled,omitempty"`
	Notify   *bool      `json:"notify,omitempty"`
	Domains  []string   `json:"domains,omitempty"`
	Filter   FilterNode `json:"filter,omitempty"`
}

type BulkPreview struct {
	ActionType  string   `json:"action_type"`
	Matched     int      `json:"matched"`
	Samples     []string `json:"samples"`
	TaskCount   int      `json:"task_count"`
	CacheHits   int      `json:"cache_hits"`
	DailyLimit  int      `json:"daily_limit"`
	DailyUsed   int      `json:"daily_used"`
	WithinLimit bool     `json:"within_limit"`
	Warning     string   `json:"warning,omitempty"`
}

type AutomationRule struct {
	ID              int64            `json:"id"`
	Name            string           `json:"name"`
	Enabled         bool             `json:"enabled"`
	DryRun          bool             `json:"dry_run"`
	Trigger         FilterNode       `json:"trigger"`
	Conditions      FilterNode       `json:"conditions"`
	Actions         []map[string]any `json:"actions"`
	CooldownSeconds int              `json:"cooldown_seconds"`
	DailyRunCap     int              `json:"daily_run_cap"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

type AutomationRun struct {
	ID          int64          `json:"id"`
	RuleID      int64          `json:"rule_id"`
	EventID     string         `json:"event_id"`
	Domain      string         `json:"domain"`
	Status      string         `json:"status"`
	DryRun      bool           `json:"dry_run"`
	ActionCount int            `json:"action_count"`
	Details     map[string]any `json:"details"`
	StartedAt   time.Time      `json:"started_at"`
	CompletedAt *time.Time     `json:"completed_at,omitempty"`
}

func (v FilterNode) MarshalJSON() ([]byte, error) {
	type alias FilterNode
	return json.Marshal(alias(v))
}
