package service

import (
	"context"
	"sync"
	"time"

	"DomainHunter/internal/config"
	"DomainHunter/internal/domain"
	"DomainHunter/internal/logger"
	"DomainHunter/internal/repository"
	"DomainHunter/internal/scheduler"
)

// MonitorService 管理调度器生命周期与后台维护任务
type MonitorService struct {
	scheduler    *scheduler.Scheduler
	query        *QueryService
	domains      repository.DomainRepository
	observations repository.ObservationRepository
	log          *logger.Logger

	mu        sync.RWMutex
	cfg       *config.Config
	startedAt time.Time

	rootCtx    context.Context
	pruneStop  chan struct{}
	pruneOnce  sync.Once
	pruneAlive bool
}

// NewMonitorService 创建监控服务
func NewMonitorService(
	sched *scheduler.Scheduler,
	query *QueryService,
	domains repository.DomainRepository,
	observations repository.ObservationRepository,
	cfg *config.Config,
) *MonitorService {
	return &MonitorService{
		scheduler:    sched,
		query:        query,
		domains:      domains,
		observations: observations,
		cfg:          cfg,
		log:          logger.Component("monitor"),
	}
}

// Scheduler 返回底层调度器
func (m *MonitorService) Scheduler() *scheduler.Scheduler { return m.scheduler }

// Start 启动调度器与历史清理任务
func (m *MonitorService) Start(ctx context.Context) error {
	m.mu.Lock()
	m.rootCtx = ctx
	m.startedAt = time.Now()
	m.mu.Unlock()

	if err := m.scheduler.Start(ctx); err != nil {
		return err
	}
	m.startPrune(ctx)
	return nil
}

// Stop 停止调度器
func (m *MonitorService) Stop() {
	m.scheduler.Stop()
	m.pruneOnce.Do(func() {
		m.mu.Lock()
		stop := m.pruneStop
		m.pruneAlive = false
		m.mu.Unlock()
		if stop != nil {
			close(stop)
		}
	})
}

// IsRunning 是否正在运行
func (m *MonitorService) IsRunning() bool { return m.scheduler.IsRunning() }

// Restart 重新启动调度器（对应 /api/monitor/start）
func (m *MonitorService) Restart(ctx context.Context) error {
	if m.scheduler.IsRunning() {
		return nil
	}
	return m.scheduler.Start(ctx)
}

// CheckNow 手动触发一次高优先级查询
func (m *MonitorService) CheckNow(ctx context.Context, name string) (*domain.Info, error) {
	return m.scheduler.RunNow(ctx, name)
}

// Enqueue 把域名放入调度队列
func (m *MonitorService) Enqueue(name string, priority domain.Priority) {
	reason := scheduler.ReasonScheduled
	if priority >= domain.PriorityManual {
		reason = scheduler.ReasonManual
	}
	m.scheduler.Enqueue(name, priority, reason)
}

// UpdateConfig 热更新监控参数
func (m *MonitorService) UpdateConfig(cfg *config.Config) {
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()

	m.query.UpdateConfig(cfg)
	m.scheduler.SetConcurrency(cfg.Monitor.ConcurrentLimit)
	m.scheduler.SetDefaultInterval(cfg.Monitor.CheckInterval)

	// 缩短检查间隔后，让已排定的下次检查时间不晚于新的间隔上限。
	if ctx := m.context(); ctx != nil {
		go m.compressSchedule(ctx, cfg.Monitor.CheckInterval)
	}

	m.log.Info(logger.Fields{
		"interval":   cfg.Monitor.CheckInterval.String(),
		"concurrent": cfg.Monitor.ConcurrentLimit,
		"timeout":    cfg.Monitor.Timeout.String(),
	}, "监控配置已热更新")
}

func (m *MonitorService) context() context.Context {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rootCtx
}

// compressSchedule 把超过新间隔的 next_check_at 压缩到窗口内
func (m *MonitorService) compressSchedule(ctx context.Context, interval time.Duration) {
	entries, err := m.domains.List(ctx, true)
	if err != nil {
		return
	}
	limit := time.Now().Add(interval)
	adjusted := 0
	for _, entry := range entries {
		if entry.NextCheckAt != nil && entry.NextCheckAt.After(limit) {
			if err := m.domains.ScheduleNext(ctx, entry.Name, limit, entry.RetryCount); err == nil {
				adjusted++
			}
		}
	}
	if adjusted > 0 {
		m.log.Info(logger.Fields{"count": adjusted}, "已按新的检查间隔压缩下次检查时间")
	}
}

func (m *MonitorService) startPrune(ctx context.Context) {
	m.mu.Lock()
	if m.pruneAlive {
		m.mu.Unlock()
		return
	}
	m.pruneStop = make(chan struct{})
	m.pruneAlive = true
	stop := m.pruneStop
	m.mu.Unlock()

	go func() {
		// 启动后先清理一次，之后每 6 小时一次
		m.prune(ctx)
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.prune(ctx)
			case <-stop:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (m *MonitorService) prune(ctx context.Context) {
	m.mu.RLock()
	retention := m.cfg.History.Retention()
	m.mu.RUnlock()

	observations, attempts, err := m.observations.Prune(ctx, retention)
	if err != nil {
		m.log.Warn(logger.Fields{"error": err.Error()}, "清理历史数据失败")
		return
	}
	if observations > 0 || attempts > 0 {
		m.log.Info(logger.Fields{"observations": observations, "attempts": attempts}, "历史数据清理完成")
	}
}

// Stats 返回监控统计（兼容旧的 /api/stats 结构）
func (m *MonitorService) Stats(ctx context.Context, domains *DomainService) map[string]any {
	counts, total, err := domains.StatusCounts(ctx)
	if err != nil {
		m.log.Error(logger.Fields{"error": err.Error()}, "统计域名状态失败")
		counts = map[domain.Status]int{}
	}

	stats := m.scheduler.Stats()
	stats["domain_count"] = total
	stats["is_running"] = m.IsRunning()
	stats["worker_count"] = m.scheduler.Workers()
	stats["status_counts"] = counts

	m.mu.RLock()
	startedAt := m.startedAt
	m.mu.RUnlock()
	if !startedAt.IsZero() {
		stats["uptime"] = time.Since(startedAt).String()
	}
	return stats
}
