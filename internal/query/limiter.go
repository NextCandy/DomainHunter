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
	mu          sync.Mutex
	defaults    time.Duration
	rules       map[string]time.Duration
	concurrency map[string]int
	semaphores  map[string]chan struct{}
	next        map[string]time.Time
}

// NewLimiter 创建限速器
func NewLimiter() *Limiter {
	return &Limiter{
		rules:       map[string]time.Duration{},
		concurrency: map[string]int{},
		semaphores:  map[string]chan struct{}{},
		next:        map[string]time.Time{},
	}
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

// Apply 用配置替换全部规则；未在配置里出现的键回落到 defaults。
// configuredConcurrency 中的 0 表示不限并发；fallback/whois_ls 的默认值为 1。
func (l *Limiter) Apply(configured map[string]string, configuredConcurrency map[string]int, defaults map[string]time.Duration) {
	rules := make(map[string]time.Duration, len(configured)+len(defaults))
	for key, interval := range defaults {
		rules[strings.ToLower(key)] = interval
	}
	for key, raw := range configured {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		interval, err := time.ParseDuration(raw)
		if err != nil || interval < 0 {
			continue
		}
		rules[strings.ToLower(key)] = interval
	}
	concurrency := map[string]int{
		ProviderWhoisLS:  1,
		ProviderFallback: 1,
	}
	for key, limit := range configuredConcurrency {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" || limit < 0 {
			continue
		}
		concurrency[key] = limit
	}

	l.mu.Lock()
	l.rules = rules
	l.concurrency = concurrency
	l.semaphores = make(map[string]chan struct{})
	for key, limit := range concurrency {
		if limit > 0 {
			l.semaphores[key] = make(chan struct{}, limit)
		}
	}
	l.mu.Unlock()
}

// Rules 返回当前生效的限速规则（供 API 展示）
func (l *Limiter) Rules() map[string]string {
	l.mu.Lock()
	defer l.mu.Unlock()

	out := make(map[string]string, len(l.rules))
	for key, interval := range l.rules {
		out[key] = interval.String()
	}
	return out
}

// ConcurrencyRules 返回当前生效的并发限制，0 表示不限。
func (l *Limiter) ConcurrencyRules() map[string]int {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[string]int, len(l.concurrency))
	for key, limit := range l.concurrency {
		out[key] = limit
	}
	return out
}

// Wait 阻塞到允许发起下一次查询为止；ctx 取消时立刻返回错误
func (l *Limiter) Wait(ctx context.Context, provider, tld string) error {
	_, err := l.acquire(ctx, provider, tld, false)
	return err
}

// Acquire 等待最小间隔并占用 Provider 信号量。调用方必须执行返回的 release，
// 以保证并发计数在查询完成后归还。
func (l *Limiter) Acquire(ctx context.Context, provider, tld string) (func(), error) {
	return l.acquire(ctx, provider, tld, true)
}

func (l *Limiter) acquire(ctx context.Context, provider, tld string, semaphore bool) (func(), error) {
	if l == nil {
		return func() {}, nil
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	tld = strings.ToLower(strings.Trim(strings.TrimSpace(tld), "."))
	key := provider + ":" + tld

	l.mu.Lock()
	interval, ok := l.rules[key]
	if !ok {
		interval, ok = l.rules[strings.ToLower(provider)]
	}
	if !ok {
		interval = l.defaults
	}
	if interval <= 0 {
		// 仍然需要在下方读取信号量配置。
	}
	now := time.Now()
	earliest := l.next[key]
	if interval > 0 && earliest.Before(now) {
		earliest = now
	}
	if interval > 0 {
		l.next[key] = earliest.Add(interval)
	}
	sem := l.semaphores[key]
	if sem == nil {
		sem = l.semaphores[provider]
	}
	l.mu.Unlock()

	if interval > 0 {
		delay := time.Until(earliest)
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return func() {}, ctx.Err()
			}
		}
	}

	if !semaphore || sem == nil {
		return func() {}, nil
	}
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return func() {}, ctx.Err()
	}
}
