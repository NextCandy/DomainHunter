// Package repository 定义存储层接口。
//
// 业务代码只依赖这些接口，不再直接调用全局的 storage.XxxDomain() 函数，
// 这样存储实现可替换、可在测试中替身，也不会出现 config → storage 这类反向依赖。
package repository

import (
	"context"
	"time"

	"DomainHunter/internal/domain"
)

// DomainPatch 域名可更新字段；nil 表示不改动
type DomainPatch struct {
	Enabled  *bool
	Notify   *bool
	Favorite *bool
	Note     *string
	Tags     *[]string
	Priority *int
	FolderID *int64
	// ClearFolder 将域名移回根目录；仅在需要清空 folder_id 时设为 true。
	ClearFolder bool
}

// DomainRepository 监控域名列表
type DomainRepository interface {
	List(ctx context.Context, enabledOnly bool) ([]domain.Domain, error)
	Get(ctx context.Context, name string) (*domain.Domain, error)
	Create(ctx context.Context, name string, enabled, notify bool) error
	Delete(ctx context.Context, name string) error
	DeleteMany(ctx context.Context, names []string) (int64, error)
	Update(ctx context.Context, name string, patch DomainPatch) error
	IsEnabled(ctx context.Context, name string) (bool, error)

	// DueForCheck 返回到期待查询的域名，按优先级与到期时间排序
	DueForCheck(ctx context.Context, now time.Time, limit int) ([]domain.Domain, error)
	// ScheduleNext 写回下次检查时间与重试计数
	ScheduleNext(ctx context.Context, name string, next time.Time, retryCount int) error
	// BackfillSchedule 为 next_check_at 为空的域名补一个初始时间
	BackfillSchedule(ctx context.Context, defaultInterval time.Duration) (int64, error)
	// LastNotifiedStatus 读取/写入最近一次已通知的状态，用于抑制重复通知
	LastNotifiedStatus(ctx context.Context, name string) (string, error)
	SetLastNotifiedStatus(ctx context.Context, name, status string) error

	CleanOrphaned(ctx context.Context) (results int64, notifications int64, err error)
	Count(ctx context.Context, enabledOnly bool) (int, error)
}

// ResultRepository 当前状态快照（domain_results）
type ResultRepository interface {
	Get(ctx context.Context, name string) (*domain.Info, error)
	LoadAll(ctx context.Context) (map[string]domain.Info, error)
	Save(ctx context.Context, info domain.Info) error
	UpdateRaw(ctx context.Context, name, raw string) error
}

// Retention 历史数据保留策略
type Retention struct {
	// Days 保留天数，<=0 表示不按时间清理
	Days int
	// MaxPerDomain 每个域名最多保留的观测条数，<=0 表示不限制
	MaxPerDomain int
	// RawMaxBytes 单条原始报文最大保存字节数，超出截断
	RawMaxBytes int
	// RawMode 原始报文保存策略：change_only / always / never
	RawMode string
}

// 原始报文保存策略取值
const (
	RawModeChangeOnly = "change_only"
	RawModeAlways     = "always"
	RawModeNever      = "never"
)

// DefaultRetention 默认保留策略：兼顾"有历史价值"与"树莓派上不会无限增长"
func DefaultRetention() Retention {
	return Retention{Days: 180, MaxPerDomain: 200, RawMaxBytes: 16 * 1024, RawMode: RawModeChangeOnly}
}

// ObservationRepository 历史观测与查询尝试
type ObservationRepository interface {
	Save(ctx context.Context, obs domain.Observation, attempts []domain.Attempt) (int64, error)
	ListByDomain(ctx context.Context, name string, limit int) ([]domain.Observation, error)
	ListAttempts(ctx context.Context, name string, limit int) ([]domain.Attempt, error)
	ListAttemptsByObservation(ctx context.Context, observationID int64) ([]domain.Attempt, error)
	ListRecentChanges(ctx context.Context, limit int) ([]domain.Observation, error)
	Prune(ctx context.Context, retention Retention) (observations int64, attempts int64, err error)
	Stats(ctx context.Context) (observations int64, attempts int64, err error)
}

// DailyStatusCount 是某个自然日内单个状态的观测计数。
// Day 使用 YYYY-MM-DD，按应用保存观测时采用的本地日历计算。
type DailyStatusCount struct {
	Day            string
	Status         domain.Status
	Count          int
	Changed        int
	HighConfidence int
}

// ObservationChange 是一条状态变化观测，以及它之前最近一次观测的状态。
// OldStatus 为空表示数据库中没有更早的观测（例如首次查询）。
type ObservationChange struct {
	Observation domain.Observation
	OldStatus   domain.Status
}

// ObservationAnalyticsRepository 是历史观测的分析查询扩展接口。
//
// 它独立于 ObservationRepository，避免给已有的替代实现增加必须实现的方法；
// SQLite 实现同时提供两者，趋势与每日摘要在不破坏旧接口的前提下使用它。
type ObservationAnalyticsRepository interface {
	DailyStatusCounts(ctx context.Context, fromDay, toDay string) ([]DailyStatusCount, error)
	ChangesBetween(ctx context.Context, fromDay, toDay string) ([]ObservationChange, error)
}

// SettingsRepository 键值配置（app_settings）
type SettingsRepository interface {
	All(ctx context.Context) (map[string]string, error)
	Get(ctx context.Context, key string) (string, bool, error)
	Upsert(ctx context.Context, values map[string]string) error
}

// NotificationRecord 通知历史
type NotificationRecord struct {
	ID        int64     `json:"id"`
	Domain    string    `json:"domain"`
	Status    string    `json:"status"`
	OldStatus string    `json:"old_status"`
	SentAt    time.Time `json:"sent_at"`
	Type      string    `json:"type"`
}

// NotificationRepository 通知历史
type NotificationRepository interface {
	Last(ctx context.Context, name string) (*NotificationRecord, error)
	Save(ctx context.Context, name, status, oldStatus string) error
	ListRecent(ctx context.Context, limit int) ([]NotificationRecord, error)
}

// FolderRepository 域名文件夹与批量移动。
type FolderRepository interface {
	List(ctx context.Context) ([]domain.Folder, error)
	Get(ctx context.Context, id int64) (*domain.Folder, error)
	Create(ctx context.Context, name string, parentID *int64) (*domain.Folder, error)
	Update(ctx context.Context, id int64, name string, parentID *int64) error
	Delete(ctx context.Context, id int64) error
	MoveDomains(ctx context.Context, names []string, folderID *int64) (int64, error)
}

// APIToken 只在创建响应中携带 RawToken；数据库只保存 Hash。
type APIToken struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	RawToken   string     `json:"token,omitempty"`
}

// APITokenRepository Bearer token 的持久化接口。
type APITokenRepository interface {
	Create(ctx context.Context, name string, scopes []string, tokenHash string) (*APIToken, error)
	List(ctx context.Context) ([]APIToken, error)
	Validate(ctx context.Context, tokenHash string) (*APIToken, error)
	Revoke(ctx context.Context, id int64) error
}

// NotificationRule 通知过滤规则。
type NotificationRule struct {
	ID            int64    `json:"id"`
	Name          string   `json:"name"`
	Enabled       bool     `json:"enabled"`
	Statuses      []string `json:"statuses,omitempty"`
	SilenceStart  string   `json:"silence_start,omitempty"`
	SilenceEnd    string   `json:"silence_end,omitempty"`
	PerDomain     bool     `json:"per_domain"`
	DigestEnabled bool     `json:"digest_enabled"`
}

// NotificationTemplate 通知模板。
type NotificationTemplate struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	EventType string `json:"event_type"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`
	Enabled   bool   `json:"enabled"`
}

// NotificationDigest 摘要调度设置。
type NotificationDigest struct {
	Enabled    bool      `json:"enabled"`
	Hour       int       `json:"hour"`
	Minute     int       `json:"minute"`
	LastSentAt time.Time `json:"last_sent_at,omitempty"`
}

// NotificationConfigRepository 通知规则、模板与摘要配置。
type NotificationConfigRepository interface {
	ListRules(ctx context.Context) ([]NotificationRule, error)
	CreateRule(ctx context.Context, rule NotificationRule) (*NotificationRule, error)
	UpdateRule(ctx context.Context, rule NotificationRule) error
	DeleteRule(ctx context.Context, id int64) error
	ListTemplates(ctx context.Context) ([]NotificationTemplate, error)
	CreateTemplate(ctx context.Context, template NotificationTemplate) (*NotificationTemplate, error)
	UpdateTemplate(ctx context.Context, template NotificationTemplate) error
	DeleteTemplate(ctx context.Context, id int64) error
	GetDigest(ctx context.Context) (*NotificationDigest, error)
	UpdateDigest(ctx context.Context, digest NotificationDigest) error
}

// NotificationDigestRepository 是每日摘要调度所需的最小配置接口。
// 单独定义以保持旧的 NotificationConfigRepository 替代实现兼容。
type NotificationDigestRepository interface {
	GetDigest(ctx context.Context) (*NotificationDigest, error)
	MarkDigestSent(ctx context.Context, when time.Time) error
}
