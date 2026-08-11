package query

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLimiterDefaultsDedicatedProvidersToOneInFlightQuery(t *testing.T) {
	limiter := NewLimiter()
	limiter.Apply(nil, nil, nil)

	rules := limiter.ConcurrencyRules()
	if rules[ProviderFallback] != 1 || rules[ProviderWhoisLS] != 1 {
		t.Fatalf("fallback/whois_ls 默认并发上限应为 1: %+v", rules)
	}

	for _, provider := range []string{ProviderFallback, ProviderWhoisLS} {
		release, err := limiter.Acquire(context.Background(), provider, "example.com")
		if err != nil {
			t.Fatalf("首次获取 %s 信号量失败: %v", provider, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		_, err = limiter.Acquire(ctx, provider, "example.com")
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s 第二个并发查询应被限制，实际错误 %v", provider, err)
		}
		release()
	}
}

func TestLimiterHonorsConfiguredConcurrencyCap(t *testing.T) {
	limiter := NewLimiter()
	limiter.Apply(nil, map[string]int{ProviderFallback: 2}, nil)

	releaseA, err := limiter.Acquire(context.Background(), ProviderFallback, "example.com")
	if err != nil {
		t.Fatalf("第一个查询获取信号量失败: %v", err)
	}
	releaseB, err := limiter.Acquire(context.Background(), ProviderFallback, "example.com")
	if err != nil {
		t.Fatalf("第二个查询获取信号量失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	_, err = limiter.Acquire(ctx, ProviderFallback, "example.com")
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("第三个查询不应超过并发上限 2，实际错误 %v", err)
	}

	releaseA()
	releaseB()
	if release, err := limiter.Acquire(context.Background(), ProviderFallback, "example.com"); err != nil {
		t.Fatalf("释放信号量后应允许新查询: %v", err)
	} else {
		release()
	}
}
