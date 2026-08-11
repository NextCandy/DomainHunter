package query

import (
	"context"
	"strings"
	"sync"
	"time"
)

// Limiter 按 "provider:tld" 维度限制查询速率。
//
// 默认关闭（间隔为 0）。它的存在是为了在需要时可以限制例如
// "1000 个 .com 同时打注册局 WHOIS" 这类会招致封禁的行为，
// 而不必再改动调度或 Provider 代码。
type Limiter struct {
	mu       sync.Mutex
	defaults time.Duration
	rules    map[string]time.Duration
	next     map[string]time.Time
}

// NewLimiter 创建限速器
func NewLimiter() *Limiter {
	return &Limiter{rules: map[string]time.Duration{}, next: map[string]time.Time{}}
}

// SetDefault 设置所有 provider:tld 组合的最小查询间隔
func (l *Limiter) SetDefault(interval time.Duration) {
	l.mu.Lock()
	l.defaults = interval
	l.mu.Unlock()
}

// Set 为指定 provider 或 "provider:tld" 设置最小查询间隔
func (l *Limiter) Set(key string, interval time.Duration) {
	l.mu.Lock()
	l.rules[strings.ToLower(key)] = interval
	l.mu.Unlock()
}

// Wait 阻塞到允许发起下一次查询为止；ctx 取消时立刻返回错误
func (l *Limiter) Wait(ctx context.Context, provider, tld string) error {
	if l == nil {
		return nil
	}
	key := strings.ToLower(provider + ":" + tld)

	l.mu.Lock()
	interval, ok := l.rules[key]
	if !ok {
		interval, ok = l.rules[strings.ToLower(provider)]
	}
	if !ok {
		interval = l.defaults
	}
	if interval <= 0 {
		l.mu.Unlock()
		return nil
	}
	now := time.Now()
	earliest := l.next[key]
	if earliest.Before(now) {
		earliest = now
	}
	l.next[key] = earliest.Add(interval)
	l.mu.Unlock()

	delay := time.Until(earliest)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
