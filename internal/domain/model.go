package domain

import (
	"strings"
	"time"
)

// Priority 查询任务优先级。数值越大越先执行。
type Priority int

const (
	// PriorityScheduled 定时到期的常规查询
	PriorityScheduled Priority = 10
	// PriorityRetry 失败重试
	PriorityRetry Priority = 50
	// PriorityManual 用户手动触发的"立即检查"
	PriorityManual Priority = 100
)

// Domain 监控列表中的一个域名（对应 domains 表）
type Domain struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Enabled     bool       `json:"enabled"`
	Notify      bool       `json:"notify"`
	Favorite    bool       `json:"favorite"`
	Note        string     `json:"note"`
	Tags        []string   `json:"tags"`
	Priority    int        `json:"priority"`
	RetryCount  int        `json:"retry_count"`
	CreatedAt   time.Time  `json:"created_at"`
	NextCheckAt *time.Time `json:"next_check_at"`
	LastChecked *time.Time `json:"last_checked_at"`
}

// Info 域名当前状态快照。
//
// JSON 字段名与重构前的 core.DomainInfo 保持一致，旧版 API 的响应结构因此不变。
type Info struct {
	Name         string     `json:"name"`
	Status       Status     `json:"status"`
	Registrar    string     `json:"registrar"`
	CreatedDate  *time.Time `json:"created_date"`
	ExpiryDate   *time.Time `json:"expiry_date"`
	UpdatedDate  *time.Time `json:"updated_date"`
	NameServers  []string   `json:"name_servers"`
	LastChecked  time.Time  `json:"last_checked"`
	QueryMethod  string     `json:"query_method"`
	ErrorMessage string     `json:"error_message"`
	AddedAt      *time.Time `json:"added_at"`
	WhoisRaw     string     `json:"whois_raw"`

	// 以下为重构新增字段，旧客户端会忽略它们。
	Confidence  Confidence `json:"confidence,omitempty"`
	EPPStatuses []string   `json:"epp_statuses,omitempty"`
	Evidence    []Evidence `json:"evidence,omitempty"`
	NextCheckAt *time.Time `json:"next_check_at,omitempty"`
	Favorite    bool       `json:"favorite,omitempty"`
	Tags        []string   `json:"tags,omitempty"`
	Note        string     `json:"note,omitempty"`
}

// HasRegistrationEvidence 判断快照里是否存在"已被注册"的实证信息。
// 用于把误判的 available 纠正回 registered。
func (i *Info) HasRegistrationEvidence() bool {
	if i == nil {
		return false
	}
	registrar := strings.ToLower(strings.TrimSpace(i.Registrar))
	hasRegistrar := registrar != "" && !strings.Contains(registrar, "不支持")
	return hasRegistrar ||
		i.CreatedDate != nil ||
		i.ExpiryDate != nil ||
		i.UpdatedDate != nil ||
		len(i.NameServers) > 0
}

// GetStatusDescription 返回状态中文描述
func (i *Info) GetStatusDescription() string { return GetStatusInfo(i.Status).Description }

// GetDisplayColor 返回状态展示颜色
func (i *Info) GetDisplayColor() string { return GetStatusInfo(i.Status).Color }

// ShouldNotify 判断当前状态是否需要通知
func (i *Info) ShouldNotify() bool { return ShouldNotify(i.Status) }

// Observation 一次查询产生的历史观测记录（对应 domain_observations 表）
type Observation struct {
	ID           int64      `json:"id"`
	DomainID     int64      `json:"domain_id"`
	Domain       string     `json:"domain"`
	Status       Status     `json:"status"`
	Registrar    string     `json:"registrar"`
	RegisteredAt *time.Time `json:"registered_at"`
	UpdatedAt    *time.Time `json:"updated_at"`
	ExpiryAt     *time.Time `json:"expiry_at"`
	NameServers  []string   `json:"name_servers"`
	Provider     string     `json:"provider"`
	Confidence   Confidence `json:"confidence"`
	ObservedAt   time.Time  `json:"observed_at"`
	Changed      bool       `json:"changed"`
}

// Attempt 单个 Provider 的一次查询尝试（对应 query_attempts 表）
type Attempt struct {
	ID            int64     `json:"id"`
	DomainID      int64     `json:"domain_id"`
	Domain        string    `json:"domain"`
	ObservationID int64     `json:"observation_id"`
	Provider      string    `json:"provider"`
	Status        Status    `json:"status"`
	Success       bool      `json:"success"`
	LatencyMS     int64     `json:"latency_ms"`
	ErrorMessage  string    `json:"error_message"`
	RawResponse   string    `json:"raw_response,omitempty"`
	QueriedAt     time.Time `json:"queried_at"`
}

// Normalize 统一域名写法：去空白 + 转小写 + 去掉末尾的点
func Normalize(name string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
}
