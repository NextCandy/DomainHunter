package ai

import (
	"context"
	"errors"
	"time"
)

var ErrActiveJobDuplicate = errors.New("相同输入的 AI 估价任务已在队列中")
var ErrJobNotCancellable = errors.New("任务已开始或不存在，不能取消")

// ProfileRecord is storage-only and must never be returned by HTTP handlers.
type ProfileRecord struct {
	Profile
	APIKeyCiphertext string
}

type JobClaim struct {
	Job        Job
	Profile    ProfileRecord
	LeaseUntil time.Time
}

type Store interface {
	ListProfiles(ctx context.Context) ([]Profile, error)
	GetProfile(ctx context.Context, id string) (*Profile, error)
	GetProfileRecord(ctx context.Context, id string) (*ProfileRecord, error)
	GetDefaultProfile(ctx context.Context) (*ProfileRecord, error)
	CreateProfile(ctx context.Context, profile ProfileRecord) (*Profile, error)
	UpdateProfile(ctx context.Context, profile ProfileRecord, updateSecret bool) (*Profile, error)
	DeleteProfile(ctx context.Context, id string) error
	SetProfileTestResult(ctx context.Context, id string, at time.Time, latencyMS *int64, safeError string) error

	GetLatestJob(ctx context.Context, domain string) (*Job, error)
	GetJob(ctx context.Context, id string) (*Job, error)
	GetFreshValuation(ctx context.Context, domain, profileID, fingerprint, promptVersion string, now time.Time) (*Valuation, error)
	CreateJob(ctx context.Context, job Job, fingerprint, promptVersion, causationID string) (*Job, error)
	ClaimNextJob(ctx context.Context, now time.Time, lease time.Duration) (*JobClaim, error)
	RenewLease(ctx context.Context, jobID string, until time.Time) error
	CancelJob(ctx context.Context, jobID string, now time.Time) (*Job, error)
	CompleteJob(ctx context.Context, jobID string, valuation Valuation, now time.Time) error
	FailJob(ctx context.Context, jobID, code, safeMessage string, retryAfter *time.Time, now time.Time) error
	RecoverExpiredLeases(ctx context.Context, now time.Time) (int64, error)
	CountStartedToday(ctx context.Context, profileID string, dayStart, dayEnd time.Time) (int, error)
	Audit(ctx context.Context, eventType, domain, profileID, jobID, actor string, details map[string]any) error
}
