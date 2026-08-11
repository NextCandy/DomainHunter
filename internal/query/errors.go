package query

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

// Kind 查询错误的分类。
//
// 上层 Policy 用它决定"要不要继续尝试下一个 Provider"以及"要不要重试"，
// 从而不再依赖对错误字符串的模式匹配。
type Kind string

const (
	// KindUnavailable Provider 本身不可用（连接失败、DNS 失败、5xx）
	KindUnavailable Kind = "provider_unavailable"
	// KindTimeout 查询超时或被取消
	KindTimeout Kind = "timeout"
	// KindRateLimited 被上游限流（HTTP 429、WHOIS query limit）
	KindRateLimited Kind = "rate_limited"
	// KindParse 响应拿到了但无法解析
	KindParse Kind = "parse_error"
	// KindNotSupported 该 Provider 不支持这个 TLD / 未配置
	KindNotSupported Kind = "not_supported"
	// KindUnknownResponse 响应合法但语义不明确，不能据此下结论
	KindUnknownResponse Kind = "unknown_response"
	// KindCanceled 调度关闭导致的主动取消
	KindCanceled Kind = "canceled"
)

// Error 带分类的查询错误
type Error struct {
	Kind     Kind
	Provider string
	Message  string
	Err      error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Kind)
}

func (e *Error) Unwrap() error { return e.Err }

// NewError 构造一个带分类的查询错误
func NewError(kind Kind, provider, format string, args ...any) *Error {
	return &Error{Kind: kind, Provider: provider, Message: fmt.Sprintf(format, args...)}
}

// WrapError 包装底层错误并自动推断分类
func WrapError(provider string, err error) *Error {
	if err == nil {
		return nil
	}
	var qe *Error
	if errors.As(err, &qe) {
		return qe
	}
	return &Error{Kind: classify(err), Provider: provider, Message: err.Error(), Err: err}
}

func classify(err error) Kind {
	switch {
	case errors.Is(err, context.Canceled):
		return KindCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return KindTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return KindTimeout
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded"):
		return KindTimeout
	case strings.Contains(msg, "429") || strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "too many requests") || strings.Contains(msg, "query limit"):
		return KindRateLimited
	case strings.Contains(msg, "connection refused") || strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "network is unreachable") || strings.Contains(msg, "connection reset"):
		return KindUnavailable
	default:
		return KindUnavailable
	}
}

// KindOf 提取错误分类；非查询错误统一归类为 provider_unavailable
func KindOf(err error) Kind {
	if err == nil {
		return ""
	}
	var qe *Error
	if errors.As(err, &qe) {
		return qe.Kind
	}
	return classify(err)
}

// Retryable 判断该错误是否值得在下一轮重试
func Retryable(err error) bool {
	switch KindOf(err) {
	case KindTimeout, KindRateLimited, KindUnavailable:
		return true
	default:
		return false
	}
}
