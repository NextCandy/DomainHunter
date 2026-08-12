package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"DomainHunter/internal/ai"
)

func TestAIRepoPersistsChineseReportFields(t *testing.T) {
	db := newTestDB(t)
	repo := NewAIRepo(db)
	now := time.Now().UTC()
	profileID := "aip_test_report"
	_, err := repo.CreateProfile(context.Background(), ai.ProfileRecord{Profile: ai.Profile{
		ID: profileID, Name: "测试默认 AI", Provider: ai.ProviderDeepSeek, Enabled: true, IsDefault: true,
		BaseURL: "https://api.deepseek.com", BaseURLHost: "api.deepseek.com", Model: "test-model",
		ThinkingType: ai.ThinkingDisabled, ReasoningEffort: ai.ReasoningLow, TimeoutSeconds: 30,
		MaxTokens: 600, Concurrency: 1, DailyLimit: 50, CacheTTLHours: 24, CreatedAt: now, UpdatedAt: now,
	}})
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	job := ai.Job{ID: "aij_test_report", Domain: "daydream.im", ProfileID: profileID, State: ai.JobRunning, Priority: ai.PriorityNormal, QueuedAt: now}
	if _, err := repo.CreateJob(context.Background(), job, "fingerprint", ai.PromptVersion, "cause"); err != nil {
		t.Fatalf("create job: %v", err)
	}
	valuation := ai.Valuation{
		ID: "aiv_test_report", Domain: "daydream.im", ProfileID: profileID, Provider: "deepseek", Model: "test-model",
		PromptVersion: ai.PromptVersion, InputFingerprint: "fingerprint", Score: 88, QualityScore: 88, LiquidityScore: 71,
		RiskLevel: "medium", Confidence: "medium", PriceEvaluationCNY: &ai.ValueRange{Low: 1800, High: 6800, Currency: "CNY"},
		Summary: "短、易记，适合品牌用途", CoreAnalysis: "前缀与后缀组合语义完整，适合品牌和创意项目。",
		StatusGuard: "AI 不改变系统查询结论", Disclaimer: "仅供研究性排序与解释，不构成估值、投资、购买或法律建议。",
		CreatedAt: now, ExpiresAt: ptrTime(now.Add(time.Hour)),
	}
	if err := repo.CompleteJob(context.Background(), job.ID, valuation, now); err != nil {
		t.Fatalf("complete job: %v", err)
	}
	stored, err := repo.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if stored == nil || stored.Result == nil || stored.Result.Score != 88 || stored.Result.PriceEvaluationCNY == nil || stored.Result.PriceEvaluationCNY.Low != 1800 || stored.Result.CoreAnalysis == "" {
		t.Fatalf("report fields did not round-trip: %+v", stored)
	}
}

func TestAIRepoRetryDeferredJobNow(t *testing.T) {
	db := newTestDB(t)
	repo := NewAIRepo(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	profileID := "aip_test_retry"
	_, err := repo.CreateProfile(ctx, ai.ProfileRecord{Profile: ai.Profile{
		ID: profileID, Name: "测试重试 AI", Provider: ai.ProviderOpenAICompatible, Enabled: true, IsDefault: true,
		BaseURL: "https://example.com/v1", BaseURLHost: "example.com", Model: "test-model",
		ThinkingType: ai.ThinkingDisabled, ReasoningEffort: ai.ReasoningLow, TimeoutSeconds: 30,
		MaxTokens: 600, Concurrency: 1, DailyLimit: 50, CacheTTLHours: 24, CreatedAt: now, UpdatedAt: now,
	}})
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	job := ai.Job{ID: "aij_test_retry", Domain: "daydream.im", ProfileID: profileID, State: ai.JobRunning, Priority: ai.PriorityNormal, QueuedAt: now}
	if _, err = repo.CreateJob(ctx, job, "fingerprint", ai.PromptVersion, "cause"); err != nil {
		t.Fatalf("create job: %v", err)
	}
	automaticRetry := now.Add(10 * time.Minute)
	if err = repo.FailJob(ctx, job.ID, "provider_error", "上游限流", &automaticRetry, now); err != nil {
		t.Fatalf("defer job: %v", err)
	}

	retried, err := repo.RetryJobNow(ctx, job.ID, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("retry deferred job: %v", err)
	}
	if retried.State != ai.JobQueued || retried.RetryAfter != nil || retried.ErrorCode != "" || retried.ErrorMessage != "" {
		t.Fatalf("deferred job was not reset for an immediate retry: %+v", retried)
	}
	if retried.Priority != ai.PriorityNormal {
		t.Fatalf("manual retry must preserve retry policy, got priority %q", retried.Priority)
	}
	if _, err = repo.RetryJobNow(ctx, job.ID, now.Add(2*time.Minute)); !errors.Is(err, ai.ErrJobNotRetryable) {
		t.Fatalf("queued job must not be retried again, got %v", err)
	}
}

func ptrTime(value time.Time) *time.Time { return &value }
