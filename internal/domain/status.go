package domain

import (
	"fmt"
	"time"
)

// Status 域名状态枚举。
//
// 这些取值同时是 SQLite 中 domain_results.status / domain_observations.status
// 的持久化值，任何改动都会影响既有数据的可读性，因此只允许新增，不允许重命名。
type Status string

const (
	// StatusAvailable 域名可注册
	StatusAvailable Status = "available"

	// StatusRegistered 域名已注册
	StatusRegistered Status = "registered"

	// StatusRedemption 域名在赎回期
	StatusRedemption Status = "redemption"

	// StatusPendingDelete 域名待删除/抢注期
	StatusPendingDelete Status = "pending_delete"

	// StatusExpired 域名已过期
	StatusExpired Status = "expired"

	// StatusGrace 宽限期/续费宽限
	StatusGrace Status = "grace"

	// StatusTransferLocked 域名转移锁定
	StatusTransferLocked Status = "transfer_locked"

	// StatusHold 域名被 Hold
	StatusHold Status = "hold"

	// StatusUnknown 未知状态
	StatusUnknown Status = "unknown"

	// StatusError 查询错误
	StatusError Status = "error"

	// StatusSkipped 没有可用查询源时跳过，避免把不可查询误报为可注册
	StatusSkipped Status = "skipped"
)

// AllStatuses 返回全部业务状态，顺序与前端展示顺序一致。
func AllStatuses() []Status {
	return []Status{
		StatusAvailable,
		StatusGrace,
		StatusRedemption,
		StatusPendingDelete,
		StatusRegistered,
		StatusTransferLocked,
		StatusHold,
		StatusExpired,
		StatusUnknown,
		StatusError,
		StatusSkipped,
	}
}

// StatusInfo 状态的展示与通知元信息
type StatusInfo struct {
	Status       Status `json:"status"`
	Description  string `json:"description"`
	Color        string `json:"color"`
	Priority     int    `json:"priority"`
	ShouldNotify bool   `json:"should_notify"`
}

var statusInfos = map[Status]StatusInfo{
	StatusAvailable:      {StatusAvailable, "域名可注册", "#28a745", 1, true},
	StatusGrace:          {StatusGrace, "宽限期", "#ffc107", 2, true},
	StatusRedemption:     {StatusRedemption, "域名在赎回期", "#fd7e14", 3, true},
	StatusPendingDelete:  {StatusPendingDelete, "域名待删除/抢注期", "#dc3545", 4, true},
	StatusRegistered:     {StatusRegistered, "域名已注册", "#6c757d", 5, true},
	StatusTransferLocked: {StatusTransferLocked, "域名转移锁定", "#17a2b8", 6, false},
	StatusHold:           {StatusHold, "域名处于 Hold 状态", "#0ea5e9", 7, false},
	StatusExpired:        {StatusExpired, "域名已过期", "#f97316", 8, true},
	StatusUnknown:        {StatusUnknown, "未知状态", "#343a40", 9, false},
	StatusError:          {StatusError, "查询错误", "#dc3545", 10, false},
	StatusSkipped:        {StatusSkipped, "已跳过（没有可用查询源）", "#64748b", 11, false},
}

// GetAllStatusInfo 返回全部状态元信息的副本
func GetAllStatusInfo() map[Status]StatusInfo {
	out := make(map[Status]StatusInfo, len(statusInfos))
	for k, v := range statusInfos {
		out[k] = v
	}
	return out
}

// GetStatusInfo 获取指定状态的元信息，未知取值回落到 unknown
func GetStatusInfo(status Status) StatusInfo {
	if info, ok := statusInfos[status]; ok {
		return info
	}
	return statusInfos[StatusUnknown]
}

// IsDefinitive 判断状态是否为"明确结论"。
//
// 只有明确结论才允许结束查询链路；unknown / error / skipped 必须继续尝试
// 其他查询源，这是"无法确认就不能报告可注册"安全模型的基础。
func IsDefinitive(status Status) bool {
	switch status {
	case StatusAvailable, StatusRegistered, StatusGrace, StatusRedemption,
		StatusPendingDelete, StatusExpired, StatusTransferLocked, StatusHold:
		return true
	default:
		return false
	}
}

// IsRegisteredLike 判断状态是否意味着"域名当前被占用"
func IsRegisteredLike(status Status) bool {
	switch status {
	case StatusRegistered, StatusGrace, StatusRedemption, StatusPendingDelete,
		StatusExpired, StatusTransferLocked, StatusHold:
		return true
	default:
		return false
	}
}

// ShouldNotify 判断状态变化到该状态时是否应该发送通知
func ShouldNotify(status Status) bool {
	return GetStatusInfo(status).ShouldNotify
}

// StatusChangeEvent 状态变化事件
type StatusChangeEvent struct {
	Domain     string    `json:"domain"`
	OldStatus  Status    `json:"old_status"`
	NewStatus  Status    `json:"new_status"`
	Timestamp  time.Time `json:"timestamp"`
	Message    string    `json:"message"`
	DomainInfo *Info     `json:"domain_info,omitempty"`
}

// GetStatusChangeMessage 生成状态变化的中文描述
func GetStatusChangeMessage(name string, oldStatus, newStatus Status) string {
	return fmt.Sprintf("域名 %s 状态发生变化：从 [%s] 变为 [%s]",
		name, GetStatusInfo(oldStatus).Description, GetStatusInfo(newStatus).Description)
}
