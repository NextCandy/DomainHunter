// Package service 承载业务逻辑：HTTP handler 只负责解析请求、调用 service、
// 返回响应；调度器只负责决定什么时候调用 service。
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"DomainHunter/internal/config"
	"DomainHunter/internal/domain"
	"DomainHunter/internal/logger"
	"DomainHunter/internal/notification"
	"DomainHunter/internal/query"
	"DomainHunter/internal/repository"
	"DomainHunter/internal/scheduler"
)

// ErrDomainNotMonitored 域名不在当前监控列表中
var ErrDomainNotMonitored = errors.New("域名不在当前监控列表中")

// cstZone 与重构前一致：写库前把时间统一转成北京时间，
// 避免新旧数据在同一张表里出现两种时区表示。
var cstZone = time.FixedZone("CST", 8*3600)

// 查询重试参数：有限次退避，避免对注册局造成压力
const (
	maxQueryAttempts = 3
	retryBaseDelay   = 500 * time.Millisecond
)

// QueryService 统一的查询执行入口。
//
// 自动查询、手动查询、批量检查、失败重试全部走这里的同一条 Pipeline，
// 不再存在 queryWithRetry / queryDomainWithRetry 两份重复实现。
type QueryService struct {
	engine       *query.Engine
	domains      repository.DomainRepository
	results      repository.ResultRepository
	observations repository.ObservationRepository
	notifier     *notification.Manager
	log          *logger.Logger

	mu  sync.RWMutex
	cfg *config.Config
}

// NewQueryService 创建查询服务
func NewQueryService(
	engine *query.Engine,
	domains repository.DomainRepository,
	results repository.ResultRepository,
	observations repository.ObservationRepository,
	notifier *notification.Manager,
	cfg *config.Config,
) *QueryService {
	return &QueryService{
		engine:       engine,
		domains:      domains,
		results:      results,
		observations: observations,
		notifier:     notifier,
		cfg:          cfg,
		log:          logger.Component("query"),
	}
}

// UpdateConfig 热更新配置
func (s *QueryService) UpdateConfig(cfg *config.Config) {
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
	s.engine.Providers().UpdateTimeout(cfg.Monitor.Timeout)
}

func (s *QueryService) config() *config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// Engine 返回底层查询引擎
func (s *QueryService) Engine() *query.Engine { return s.engine }

// Execute 实现 scheduler.Executor
func (s *QueryService) Execute(ctx context.Context, task scheduler.Task) (*domain.Info, error) {
	name := domain.Normalize(task.Domain)
	if name == "" {
		return nil, fmt.Errorf("域名不能为空")
	}

	record, err := s.domains.Get(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("读取域名失败: %w", err)
	}
	if record == nil || !record.Enabled {
		return nil, fmt.Errorf("%w: %s", ErrDomainNotMonitored, name)
	}

	if s.notifier != nil {
		s.notifier.RecordQuery(name)
	}

	previous, err := s.results.Get(ctx, name)
	if err != nil {
		s.log.Warn(logger.Fields{"domain": name, "error": err.Error()}, "读取历史结果失败")
	}
	previousStatus := domain.StatusUnknown
	if previous != nil {
		previousStatus = previous.Status
	}

	started := time.Now()
	forceRefresh := task.Priority >= domain.PriorityManual || task.Reason == scheduler.ReasonManual
	outcome := s.runWithRetry(ctx, name, forceRefresh)
	info := outcome.Info
	info.AddedAt = &record.CreatedAt
	info.Favorite = record.Favorite
	info.Tags = record.Tags
	info.Note = record.Note
	info.Priority = record.Priority

	s.log.Info(logger.Fields{
		"domain":     name,
		"provider":   outcome.Winner.Provider,
		"status":     string(info.Status),
		"confidence": string(info.Confidence),
		"reason":     string(task.Reason),
		"latency_ms": time.Since(started).Milliseconds(),
	}, "查询完成")

	// 查询可能与删除并发完成：删除后不再写回结果，避免制造孤立记录。
	stillEnabled, err := s.domains.IsEnabled(ctx, name)
	if err != nil {
		return info, fmt.Errorf("确认域名仍在监控列表失败: %w", err)
	}
	if !stillEnabled {
		s.log.Info(logger.Fields{"domain": name}, "域名已不在监控列表，跳过结果写回")
		return info, nil
	}

	stored := s.toStored(info)
	if err := s.results.Save(ctx, stored); err != nil {
		s.log.Error(logger.Fields{"domain": name, "error": err.Error()}, "保存域名结果失败")
	}

	changed := previous != nil && previousStatus != info.Status
	s.saveHistory(ctx, record, outcome, stored, changed || previous == nil)
	s.scheduleNext(ctx, record, info.Status)

	if previous != nil {
		s.maybeNotify(ctx, record, previousStatus, outcome)
	}
	return info, nil
}

// CheckNow 立即查询（不经过调度队列，供批量检查等场景使用）
func (s *QueryService) CheckNow(ctx context.Context, name string) (*domain.Info, error) {
	return s.Execute(ctx, scheduler.Task{
		Domain:     name,
		Priority:   domain.PriorityManual,
		Reason:     scheduler.ReasonManual,
		EnqueuedAt: time.Now(),
	})
}

// runWithRetry 执行查询并在可重试的错误上做有限次退避重试
func (s *QueryService) runWithRetry(ctx context.Context, name string, forceRefresh bool) query.Outcome {
	var (
		last    query.Outcome
		reasons []string
	)

	for attempt := 1; attempt <= maxQueryAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			last.Info = &domain.Info{
				Name:         name,
				Status:       domain.StatusError,
				ErrorMessage: "查询被取消",
				LastChecked:  time.Now(),
			}
			return last
		}

		if forceRefresh {
			last = s.engine.QueryUncached(ctx, name)
		} else {
			last = s.engine.Query(ctx, name)
		}
		if last.Winner.Status != domain.StatusError {
			return last
		}

		if msg := simplifyError(last.Info.ErrorMessage); msg != "" {
			reasons = appendUnique(reasons, msg)
		}
		if !query.Retryable(last.Winner.Err) {
			break
		}
		if attempt == maxQueryAttempts {
			break
		}

		wait := time.Duration(attempt) * retryBaseDelay
		s.log.Debug(logger.Fields{"domain": name, "error": last.Info.ErrorMessage},
			"第%d次查询失败，%v 后重试", attempt, wait)
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return last
		}
	}

	if last.Info != nil && last.Info.Status == domain.StatusError {
		switch len(reasons) {
		case 0:
			last.Info.ErrorMessage = "连续查询失败，原因未知"
		default:
			last.Info.ErrorMessage = fmt.Sprintf("连续%d次查询失败: %s",
				maxQueryAttempts, strings.Join(reasons, "; "))
		}
	}
	return last
}

// toStored 把查询结果转换为写库用的快照（时区与注册商占位符与重构前一致）
func (s *QueryService) toStored(info *domain.Info) domain.Info {
	stored := *info

	registrar := stored.Registrar
	if registrar == "" &&
		stored.Status != domain.StatusAvailable &&
		stored.Status != domain.StatusError &&
		stored.Status != domain.StatusUnknown &&
		stored.Status != domain.StatusSkipped {
		registrar = "该后缀不支持注册商信息"
	}
	stored.Registrar = registrar

	stored.LastChecked = stored.LastChecked.In(cstZone)
	stored.CreatedDate = toCST(stored.CreatedDate)
	stored.ExpiryDate = toCST(stored.ExpiryDate)
	stored.UpdatedDate = toCST(stored.UpdatedDate)
	return stored
}

// saveHistory 写入观测与查询尝试历史。
//
// 状态没变时不必每次都记：817 个域名 10 分钟一轮，每天就是 11 万条几乎完全
// 相同的行。这里只在"状态变化"或"距上次观测超过心跳间隔"时落盘，
// 既保留完整的状态流转，也不会把数据库撑爆。
func (s *QueryService) saveHistory(ctx context.Context, record *domain.Domain, outcome query.Outcome, stored domain.Info, changed bool) {
	history := s.config().History
	if !changed && !s.heartbeatDue(ctx, record.Name, history.HeartbeatInterval()) {
		return
	}
	retention := history.Retention()

	obs := domain.Observation{
		DomainID:     record.ID,
		Domain:       record.Name,
		Status:       stored.Status,
		Registrar:    stored.Registrar,
		RegisteredAt: stored.CreatedDate,
		UpdatedAt:    stored.UpdatedDate,
		ExpiryAt:     stored.ExpiryDate,
		NameServers:  stored.NameServers,
		Provider:     outcome.Winner.Provider,
		Confidence:   stored.Confidence,
		ObservedAt:   time.Now().In(cstZone),
		Changed:      changed,
	}

	attempts := make([]domain.Attempt, 0, len(outcome.Results))
	for _, res := range outcome.Results {
		attempt := domain.Attempt{
			DomainID:  record.ID,
			Domain:    record.Name,
			Provider:  res.Provider,
			Status:    res.Status,
			Success:   res.Err == nil,
			LatencyMS: res.Latency.Milliseconds(),
			QueriedAt: res.StartedAt.In(cstZone),
		}
		if res.Err != nil {
			attempt.ErrorMessage = res.Err.Error()
		} else if res.Note != "" {
			attempt.ErrorMessage = res.Note
		}
		attempt.RawResponse = trimRaw(res.Raw, retention, changed)
		attempts = append(attempts, attempt)
	}

	if _, err := s.observations.Save(ctx, obs, attempts); err != nil {
		s.log.Error(logger.Fields{"domain": record.Name, "error": err.Error()}, "保存观测历史失败")
	}
}

// heartbeatDue 距上次观测是否已经超过心跳间隔；interval<=0 表示每次都记录
func (s *QueryService) heartbeatDue(ctx context.Context, name string, interval time.Duration) bool {
	if interval <= 0 {
		return true
	}
	latest, err := s.observations.ListByDomain(ctx, name, 1)
	if err != nil {
		// 读不到就按"该记"处理，宁可多写一条也不要丢历史
		return true
	}
	if len(latest) == 0 {
		return true
	}
	return time.Since(latest[0].ObservedAt) >= interval
}

// trimRaw 按保留策略决定是否保存原始报文，以及最多保存多少字节。
// 默认只在状态变化时保存，防止历史表被 WHOIS 全文撑爆。
func trimRaw(raw string, retention repository.Retention, changed bool) string {
	switch retention.RawMode {
	case repository.RawModeNever:
		return ""
	case repository.RawModeAlways:
	default:
		if !changed {
			return ""
		}
	}
	limit := retention.RawMaxBytes
	if limit <= 0 {
		limit = 16 * 1024
	}
	if len(raw) > limit {
		return raw[:limit] + "\n...(已截断)"
	}
	return raw
}

// scheduleNext 计算并写回下次检查时间
func (s *QueryService) scheduleNext(ctx context.Context, record *domain.Domain, status domain.Status) {
	cfg := s.config()

	retryCount := 0
	if status == domain.StatusError {
		retryCount = record.RetryCount + 1
	}
	interval := NextInterval(status, cfg.Monitor.CheckInterval, retryCount)

	if err := s.domains.ScheduleNext(ctx, record.Name, time.Now().Add(interval), retryCount); err != nil {
		s.log.Error(logger.Fields{"domain": record.Name, "error": err.Error()}, "写回下次检查时间失败")
	}
}

// NextInterval 根据状态计算下次检查间隔。
//
// 与重构前保持一致：正常状态一律使用设置界面里的"检查间隔"，
// skipped 低频重试。区别只在 error 上引入了有限退避（5min → 15min → 1h），
// 让偶发故障能更快恢复，同时长期失败不会持续制造请求。
func NextInterval(status domain.Status, base time.Duration, retryCount int) time.Duration {
	if base <= 0 {
		base = 5 * time.Minute
	}
	switch status {
	case domain.StatusError:
		switch {
		case retryCount <= 1:
			return 5 * time.Minute
		case retryCount == 2:
			return 15 * time.Minute
		default:
			return time.Hour
		}
	case domain.StatusSkipped:
		return 24 * time.Hour
	default:
		return base
	}
}

// maybeNotify 判断并发送状态变化通知
func (s *QueryService) maybeNotify(ctx context.Context, record *domain.Domain, previous domain.Status, outcome query.Outcome) {
	if s.notifier == nil || !record.Notify {
		return
	}
	current := outcome.Info.Status
	if previous == current {
		return
	}
	// 首次查询（无历史状态）与从失败态恢复都不通知
	if previous == domain.StatusUnknown || previous == domain.StatusError {
		return
	}
	// 旧版本把 EPP 转移锁误存成 transfer_locked；新版本只保留
	// epp_statuses 并把主状态纠正为 registered。这个兼容性回归不应再发通知。
	if suppressLegacyTransferNotification(previous, current) {
		return
	}
	// .im 官方公开 WHOIS 经常只有到期日，没有注册日期或明确阶段字段。
	// 这类不完整证据在历史数据里可能仍留下 grace/registered 交替，不能
	// 把它当成真实生命周期变化反复提醒。
	if suppressUncertainIMLifecycleNotification(record.Name, previous, current, outcome) {
		s.log.Debug(logger.Fields{"domain": record.Name, "previous": previous, "current": current},
			".im 生命周期证据不完整，抑制阶段变化提醒")
		return
	}
	if !domain.ShouldNotify(current) {
		return
	}
	// 可注册是最重要也最容易误报的结论：证据不足时不发通知。
	review := domain.BuildReviewState(outcome.Info, time.Now())
	if review != nil && review.Required && (current == domain.StatusAvailable || domain.IsDropStatus(current)) {
		s.log.Warn(logger.Fields{"domain": record.Name, "reasons": review.Reasons},
			"域名需要复核，不发送可注册或掉落机会通知")
		return
	}
	if current == domain.StatusAvailable && outcome.Winner.Confidence == domain.ConfidenceLow {
		s.log.Warn(logger.Fields{"domain": record.Name, "provider": outcome.Winner.Provider},
			"可注册结论证据不足，不发送通知")
		return
	}

	lastNotified, err := s.domains.LastNotifiedStatus(ctx, record.Name)
	if err == nil && lastNotified == string(current) {
		return
	}

	event := notification.Event{
		Type:      "status_change",
		Domain:    record.Name,
		Status:    string(current),
		OldStatus: string(previous),
		Message:   domain.GetStatusChangeMessage(record.Name, previous, current),
		Timestamp: time.Now(),
		WhoisRaw:  outcome.Info.WhoisRaw,
	}
	if !s.notifier.Allows(event) {
		return
	}
	s.notifier.Submit(event)
	if err := s.domains.SetLastNotifiedStatus(ctx, record.Name, string(current)); err != nil {
		s.log.Warn(logger.Fields{"domain": record.Name, "error": err.Error()}, "记录已通知状态失败")
	}
}

func suppressLegacyTransferNotification(previous, current domain.Status) bool {
	return previous == domain.StatusTransferLocked && current == domain.StatusRegistered
}

func suppressUncertainIMLifecycleNotification(name string, previous, current domain.Status, outcome query.Outcome) bool {
	if !strings.HasSuffix(domain.Normalize(name), ".im") || previous == current ||
		!domain.IsRegisteredLike(previous) || !domain.IsRegisteredLike(current) {
		return false
	}
	if outcome.Info != nil && outcome.Info.CreatedDate != nil {
		return false
	}
	for _, result := range outcome.Results {
		if result.CreatedAt != nil || result.LifecycleEvidence {
			return false
		}
	}
	return true
}

func toCST(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	converted := t.In(cstZone)
	return &converted
}

func simplifyError(msg string) string {
	msg = strings.TrimSpace(msg)
	for _, prefix := range []string{"WHOIS连接失败: ", "RDAP查询失败: "} {
		msg = strings.TrimPrefix(msg, prefix)
	}
	return msg
}

func appendUnique(list []string, value string) []string {
	for _, item := range list {
		if item == value {
			return list
		}
	}
	return append(list, value)
}
