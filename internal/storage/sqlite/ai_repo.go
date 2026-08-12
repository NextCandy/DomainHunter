package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"DomainHunter/internal/ai"
)

type AIRepo struct{ db *DB }

func NewAIRepo(db *DB) *AIRepo { return &AIRepo{db: db} }

const valuationSelect = `SELECT v.id,v.domain,v.profile_id,COALESCE(p.name,''),v.provider,v.model,v.prompt_version,v.input_fingerprint,
	v.quality_score,v.liquidity_score,v.risk_level,v.confidence,
	v.indicative_value_low,v.indicative_value_high,v.currency,
	v.price_evaluation_low,v.price_evaluation_high,v.price_evaluation_currency,
	v.summary,v.core_analysis,v.strengths_json,v.risks_json,v.data_gaps_json,v.evidence_used_json,
	v.status_guard,v.disclaimer,v.created_at,v.expires_at
	FROM ai_domain_valuations_v2 v LEFT JOIN ai_profiles p ON p.id=v.profile_id`

const aiProfileColumns = `id,name,provider,enabled,is_default,base_url,base_url_host,model,
	api_key_ciphertext,api_key_source,thinking_type,reasoning_effort,timeout_seconds,max_tokens,
	concurrency,daily_limit,cache_ttl_hours,last_tested_at,last_test_latency_ms,last_error,created_at,updated_at`

func scanProfile(scanner interface{ Scan(...any) error }) (ai.ProfileRecord, error) {
	var record ai.ProfileRecord
	var enabled, isDefault int
	var tested sql.NullTime
	var latency sql.NullInt64
	if err := scanner.Scan(
		&record.ID, &record.Name, &record.Provider, &enabled, &isDefault, &record.BaseURL,
		&record.BaseURLHost, &record.Model, &record.APIKeyCiphertext, &record.APIKeySource,
		&record.ThinkingType, &record.ReasoningEffort, &record.TimeoutSeconds, &record.MaxTokens,
		&record.Concurrency, &record.DailyLimit, &record.CacheTTLHours, &tested, &latency,
		&record.LastError, &record.CreatedAt, &record.UpdatedAt,
	); err != nil {
		return record, err
	}
	record.Enabled = enabled == 1
	record.IsDefault = isDefault == 1
	record.APIKeySet = record.APIKeyCiphertext != "" || (record.APIKeySource == ai.SecretSourceEnv && os.Getenv("DOMAINHUNTER_AI_API_KEY") != "")
	if record.APIKeyCiphertext == "" && os.Getenv("DOMAINHUNTER_AI_API_KEY") != "" {
		record.APIKeySource = ai.SecretSourceEnv
		record.APIKeySet = true
	}
	if tested.Valid {
		value := tested.Time
		record.LastTestedAt = &value
	}
	if latency.Valid {
		value := latency.Int64
		record.LastTestLatencyMS = &value
	}
	record.Status = profileStatus(record.Profile)
	return record, nil
}

func profileStatus(profile ai.Profile) ai.ProfileStatus {
	if !profile.Enabled {
		return ai.ProfileDisabled
	}
	if !profile.APIKeySet {
		return ai.ProfileNotConfigured
	}
	if profile.LastError != "" {
		return ai.ProfileDegraded
	}
	return ai.ProfileReady
}

func (r *AIRepo) ListProfiles(ctx context.Context) ([]ai.Profile, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+aiProfileColumns+` FROM ai_profiles ORDER BY is_default DESC, name ASC`)
	if err != nil {
		return nil, fmt.Errorf("查询 AI 档案失败: %w", err)
	}
	defer rows.Close()
	out := []ai.Profile{}
	for rows.Next() {
		record, scanErr := scanProfile(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, record.Profile)
	}
	return out, rows.Err()
}

func (r *AIRepo) GetProfile(ctx context.Context, id string) (*ai.Profile, error) {
	record, err := r.GetProfileRecord(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record.Profile, nil
}

func (r *AIRepo) GetProfileRecord(ctx context.Context, id string) (*ai.ProfileRecord, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+aiProfileColumns+` FROM ai_profiles WHERE id=?`, id)
	record, err := scanProfile(row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (r *AIRepo) GetDefaultProfile(ctx context.Context) (*ai.ProfileRecord, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+aiProfileColumns+` FROM ai_profiles WHERE enabled=1 ORDER BY is_default DESC, created_at ASC LIMIT 1`)
	record, err := scanProfile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (r *AIRepo) CreateProfile(ctx context.Context, profile ai.ProfileRecord) (*ai.Profile, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if profile.IsDefault {
		if _, err = tx.ExecContext(ctx, `UPDATE ai_profiles SET is_default=0, updated_at=? WHERE is_default=1`, time.Now().UTC()); err != nil {
			return nil, err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO ai_profiles(
		id,name,provider,enabled,is_default,base_url,base_url_host,model,api_key_ciphertext,api_key_source,
		thinking_type,reasoning_effort,timeout_seconds,max_tokens,concurrency,daily_limit,cache_ttl_hours,
		last_tested_at,last_test_latency_ms,last_error,created_at,updated_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		profile.ID, profile.Name, profile.Provider, boolToInt(profile.Enabled), boolToInt(profile.IsDefault), profile.BaseURL,
		profile.BaseURLHost, profile.Model, profile.APIKeyCiphertext, profile.APIKeySource, profile.ThinkingType,
		profile.ReasoningEffort, profile.TimeoutSeconds, profile.MaxTokens, profile.Concurrency, profile.DailyLimit,
		profile.CacheTTLHours, profile.LastTestedAt, profile.LastTestLatencyMS, profile.LastError, profile.CreatedAt, profile.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("创建 AI 档案失败: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	profile.APIKeySet = profile.APIKeyCiphertext != "" || profile.APIKeySource == ai.SecretSourceEnv
	profile.Status = profileStatus(profile.Profile)
	return &profile.Profile, nil
}

func (r *AIRepo) UpdateProfile(ctx context.Context, profile ai.ProfileRecord, updateSecret bool) (*ai.Profile, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if profile.IsDefault {
		if _, err = tx.ExecContext(ctx, `UPDATE ai_profiles SET is_default=0, updated_at=? WHERE id<>? AND is_default=1`, time.Now().UTC(), profile.ID); err != nil {
			return nil, err
		}
	}
	secretSQL := ""
	args := []any{profile.Name, profile.Provider, boolToInt(profile.Enabled), boolToInt(profile.IsDefault), profile.BaseURL, profile.BaseURLHost, profile.Model, profile.ThinkingType, profile.ReasoningEffort, profile.TimeoutSeconds, profile.MaxTokens, profile.Concurrency, profile.DailyLimit, profile.CacheTTLHours, profile.UpdatedAt}
	if updateSecret {
		secretSQL = `, api_key_ciphertext=?, api_key_source=?`
		args = append(args, profile.APIKeyCiphertext, profile.APIKeySource)
	}
	args = append(args, profile.ID)
	query := `UPDATE ai_profiles SET name=?,provider=?,enabled=?,is_default=?,base_url=?,base_url_host=?,model=?,thinking_type=?,reasoning_effort=?,timeout_seconds=?,max_tokens=?,concurrency=?,daily_limit=?,cache_ttl_hours=?,updated_at=?` + secretSQL + ` WHERE id=?`
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("更新 AI 档案失败: %w", err)
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return nil, sql.ErrNoRows
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetProfile(ctx, profile.ID)
}

func (r *AIRepo) DeleteProfile(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM ai_profiles WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("删除 AI 档案失败: %w", err)
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *AIRepo) SetProfileTestResult(ctx context.Context, id string, at time.Time, latencyMS *int64, safeError string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE ai_profiles SET last_tested_at=?,last_test_latency_ms=?,last_error=?,updated_at=? WHERE id=?`, at, latencyMS, safeError, at, id)
	return err
}

const aiJobColumns = `id,domain,profile_id,state,priority,queued_at,started_at,completed_at,retry_after,error_code,safe_error_message`

func scanJob(scanner interface{ Scan(...any) error }) (ai.Job, error) {
	var job ai.Job
	var started, completed, retry sql.NullTime
	if err := scanner.Scan(&job.ID, &job.Domain, &job.ProfileID, &job.State, &job.Priority, &job.QueuedAt, &started, &completed, &retry, &job.ErrorCode, &job.ErrorMessage); err != nil {
		return job, err
	}
	if started.Valid {
		value := started.Time
		job.StartedAt = &value
	}
	if completed.Valid {
		value := completed.Time
		job.CompletedAt = &value
	}
	if retry.Valid {
		value := retry.Time
		job.RetryAfter = &value
	}
	return job, nil
}

func (r *AIRepo) GetLatestJob(ctx context.Context, domain string) (*ai.Job, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+aiJobColumns+` FROM ai_valuation_jobs WHERE lower(domain)=lower(?) ORDER BY queued_at DESC LIMIT 1`, domain)
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = r.attachResult(ctx, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *AIRepo) GetJob(ctx context.Context, id string) (*ai.Job, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+aiJobColumns+` FROM ai_valuation_jobs WHERE id=?`, id)
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = r.attachResult(ctx, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *AIRepo) GetFreshValuation(ctx context.Context, domain, profileID, fingerprint, promptVersion string, now time.Time) (*ai.Valuation, error) {
	row := r.db.QueryRowContext(ctx, valuationSelect+` WHERE lower(v.domain)=lower(?) AND v.profile_id=? AND v.input_fingerprint=? AND v.prompt_version=? AND (v.expires_at IS NULL OR v.expires_at>?) ORDER BY v.created_at DESC LIMIT 1`, domain, profileID, fingerprint, promptVersion, now)
	valuation, err := scanValuation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &valuation, nil
}

func (r *AIRepo) CreateJob(ctx context.Context, job ai.Job, fingerprint, promptVersion, causationID string) (*ai.Job, error) {
	_, err := r.db.ExecContext(ctx, `INSERT INTO ai_valuation_jobs(id,domain,profile_id,state,priority,input_fingerprint,prompt_version,queued_at,causation_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, job.ID, job.Domain, job.ProfileID, job.State, job.Priority, fingerprint, promptVersion, job.QueuedAt, causationID, job.QueuedAt, job.QueuedAt)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "idx_ai_valuation_jobs_active_dedup") || strings.Contains(strings.ToLower(err.Error()), "unique constraint failed") {
			return nil, ai.ErrActiveJobDuplicate
		}
		return nil, err
	}
	return r.GetJob(ctx, job.ID)
}

func (r *AIRepo) ClaimNextJob(ctx context.Context, now time.Time, lease time.Duration) (*ai.JobClaim, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, `SELECT `+aiJobColumns+` FROM ai_valuation_jobs AS candidate
		WHERE (candidate.state='queued' OR (candidate.state='deferred' AND (candidate.retry_after IS NULL OR candidate.retry_after<=?)))
		  AND (SELECT COUNT(1) FROM ai_valuation_jobs AS running WHERE running.profile_id=candidate.profile_id AND running.state='running')
		      < COALESCE((SELECT concurrency FROM ai_profiles AS profile WHERE profile.id=candidate.profile_id), 1)
		ORDER BY CASE candidate.priority WHEN 'high' THEN 0 ELSE 1 END, candidate.queued_at ASC LIMIT 1`, now)
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	leaseUntil := now.Add(lease)
	result, err := tx.ExecContext(ctx, `UPDATE ai_valuation_jobs SET state='running',started_at=COALESCE(started_at,?),lease_until=?,attempt_count=attempt_count+1,updated_at=? WHERE id=? AND state IN ('queued','deferred')`, now, leaseUntil, now, job.ID)
	if err != nil {
		return nil, err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return nil, nil
	}
	row = tx.QueryRowContext(ctx, `SELECT `+aiProfileColumns+` FROM ai_profiles WHERE id=? AND enabled=1`, job.ProfileID)
	profile, err := scanProfile(row)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	job.State = ai.JobRunning
	job.StartedAt = &now
	return &ai.JobClaim{Job: job, Profile: profile, LeaseUntil: leaseUntil}, nil
}

func (r *AIRepo) RenewLease(ctx context.Context, jobID string, until time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE ai_valuation_jobs SET lease_until=?,updated_at=? WHERE id=? AND state='running'`, until, time.Now().UTC(), jobID)
	return err
}

func (r *AIRepo) CancelJob(ctx context.Context, jobID string, now time.Time) (*ai.Job, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE ai_valuation_jobs SET state='cancelled',completed_at=?,lease_until=NULL,updated_at=? WHERE id=? AND state IN ('queued','deferred')`, now, now, jobID)
	if err != nil {
		return nil, err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return nil, ai.ErrJobNotCancellable
	}
	return r.GetJob(ctx, jobID)
}

func (r *AIRepo) RetryJobNow(ctx context.Context, jobID string, now time.Time) (*ai.Job, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE ai_valuation_jobs SET state='queued',retry_after=NULL,lease_until=NULL,error_code='',safe_error_message='',updated_at=? WHERE id=? AND state='deferred'`, now, jobID)
	if err != nil {
		return nil, err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return nil, ai.ErrJobNotRetryable
	}
	return r.GetJob(ctx, jobID)
}

func (r *AIRepo) CompleteJob(ctx context.Context, jobID string, valuation ai.Valuation, now time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	strengths, _ := json.Marshal(valuation.Strengths)
	risks, _ := json.Marshal(valuation.Risks)
	gaps, _ := json.Marshal(valuation.DataGaps)
	evidence, _ := json.Marshal(valuation.EvidenceUsed)
	var low, high, priceLow, priceHigh any
	if valuation.IndicativeValueUSD != nil {
		low = valuation.IndicativeValueUSD.Low
		high = valuation.IndicativeValueUSD.High
	}
	if valuation.PriceEvaluationCNY != nil {
		priceLow = valuation.PriceEvaluationCNY.Low
		priceHigh = valuation.PriceEvaluationCNY.High
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO ai_domain_valuations_v2(id,domain,job_id,profile_id,provider,model,prompt_version,input_fingerprint,quality_score,liquidity_score,risk_level,confidence,indicative_value_low,indicative_value_high,currency,price_evaluation_low,price_evaluation_high,price_evaluation_currency,summary,core_analysis,strengths_json,risks_json,data_gaps_json,evidence_used_json,status_guard,disclaimer,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, valuation.ID, valuation.Domain, jobID, valuation.ProfileID, valuation.Provider, valuation.Model, valuation.PromptVersion, valuation.InputFingerprint, valuation.QualityScore, valuation.LiquidityScore, valuation.RiskLevel, valuation.Confidence, low, high, "USD", priceLow, priceHigh, "CNY", valuation.Summary, valuation.CoreAnalysis, string(strengths), string(risks), string(gaps), string(evidence), valuation.StatusGuard, valuation.Disclaimer, valuation.CreatedAt, valuation.ExpiresAt)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE ai_valuation_jobs SET state='succeeded',completed_at=?,lease_until=NULL,error_code='',safe_error_message='',updated_at=? WHERE id=? AND state='running'`, now, now, jobID)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return errors.New("任务不是运行状态")
	}
	return tx.Commit()
}

func (r *AIRepo) FailJob(ctx context.Context, jobID, code, safeMessage string, retryAfter *time.Time, now time.Time) error {
	state := "failed"
	if retryAfter != nil {
		state = "deferred"
	}
	_, err := r.db.ExecContext(ctx, `UPDATE ai_valuation_jobs SET state=?,completed_at=CASE WHEN ?='failed' THEN ? ELSE NULL END,retry_after=?,lease_until=NULL,error_code=?,safe_error_message=?,updated_at=? WHERE id=?`, state, state, now, retryAfter, code, safeMessage, now, jobID)
	return err
}

func (r *AIRepo) RecoverExpiredLeases(ctx context.Context, now time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE ai_valuation_jobs SET state='deferred',retry_after=?,lease_until=NULL,safe_error_message='worker lease expired; task will retry',updated_at=? WHERE state='running' AND lease_until<?`, now, now, now)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
func (r *AIRepo) CountStartedToday(ctx context.Context, profileID string, dayStart, dayEnd time.Time) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM ai_valuation_jobs WHERE profile_id=? AND started_at>=? AND started_at<?`, profileID, dayStart, dayEnd).Scan(&count)
	return count, err
}
func (r *AIRepo) Audit(ctx context.Context, eventType, domain, profileID, jobID, actor string, details map[string]any) error {
	raw, _ := json.Marshal(details)
	_, err := r.db.ExecContext(ctx, `INSERT INTO ai_audit_log(event_type,domain,profile_id,job_id,actor,details_json,created_at) VALUES(?,?,?,?,?,?,?)`, eventType, domain, profileID, jobID, actor, string(raw), time.Now().UTC())
	return err
}

func (r *AIRepo) attachResult(ctx context.Context, job *ai.Job) error {
	row := r.db.QueryRowContext(ctx, valuationSelect+` WHERE v.job_id=?`, job.ID)
	value, err := scanValuation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	job.Result = &value
	return nil
}

func scanValuation(scanner interface{ Scan(...any) error }) (ai.Valuation, error) {
	var v ai.Valuation
	var low, high, priceLow, priceHigh sql.NullInt64
	var expires sql.NullTime
	var strengths, risks, gaps, evidence, currency, priceCurrency string
	err := scanner.Scan(&v.ID, &v.Domain, &v.ProfileID, &v.ProfileName, &v.Provider, &v.Model, &v.PromptVersion, &v.InputFingerprint, &v.QualityScore, &v.LiquidityScore, &v.RiskLevel, &v.Confidence, &low, &high, &currency, &priceLow, &priceHigh, &priceCurrency, &v.Summary, &v.CoreAnalysis, &strengths, &risks, &gaps, &evidence, &v.StatusGuard, &v.Disclaimer, &v.CreatedAt, &expires)
	if err != nil {
		return v, err
	}
	if low.Valid && high.Valid {
		v.IndicativeValueUSD = &ai.ValueRange{Low: low.Int64, High: high.Int64, Currency: currency}
	}
	v.Score = v.QualityScore
	if priceLow.Valid && priceHigh.Valid {
		if priceCurrency == "" {
			priceCurrency = "CNY"
		}
		v.PriceEvaluationCNY = &ai.ValueRange{Low: priceLow.Int64, High: priceHigh.Int64, Currency: priceCurrency}
	}
	_ = json.Unmarshal([]byte(strengths), &v.Strengths)
	_ = json.Unmarshal([]byte(risks), &v.Risks)
	_ = json.Unmarshal([]byte(gaps), &v.DataGaps)
	_ = json.Unmarshal([]byte(evidence), &v.EvidenceUsed)
	if expires.Valid {
		x := expires.Time
		v.ExpiresAt = &x
	}
	return v, nil
}
