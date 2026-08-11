// Package scheduler 负责决定"什么时候查哪个域名"，并把任务交给固定规模的
// worker pool 执行。
//
// 重构前每个域名都有一个常驻 goroutine 和自己的 ticker（817 个域名 = 817 个
// 协程各自轮询数据库）。现在只有一个调度循环 + N 个 worker：
//
//	数据库(next_check_at) → Scheduler → 优先级队列 → Worker Pool → Query Engine
package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/logger"
	"DomainHunter/internal/repository"
)

// Executor 执行单个查询任务。由 service.QueryService 实现。
type Executor interface {
	Execute(ctx context.Context, task Task) (*domain.Info, error)
}

// Options 调度器参数
type Options struct {
	// Workers 并发 worker 数量
	Workers int
	// PollInterval 扫描到期域名的间隔
	PollInterval time.Duration
	// BatchSize 单次扫描最多取多少个域名
	BatchSize int
	// DefaultInterval 首次启动补齐 next_check_at 时使用的间隔
	DefaultInterval time.Duration
}

func (o Options) normalized() Options {
	if o.Workers <= 0 {
		o.Workers = 10
	}
	if o.PollInterval <= 0 {
		o.PollInterval = 5 * time.Second
	}
	if o.BatchSize <= 0 {
		o.BatchSize = 200
	}
	if o.DefaultInterval <= 0 {
		o.DefaultInterval = 5 * time.Minute
	}
	return o
}

// Scheduler 调度器
type Scheduler struct {
	repo     repository.DomainRepository
	executor Executor
	queue    *Queue
	log      *logger.Logger

	mu       sync.Mutex
	running  bool
	opts     Options
	workers  int
	cancel   context.CancelFunc
	ctx      context.Context
	stopWork chan struct{}
	wg       sync.WaitGroup

	statsMu   sync.Mutex
	executed  int64
	failed    int64
	startedAt time.Time
	lastRunAt time.Time
}

// New 创建调度器
func New(repo repository.DomainRepository, executor Executor, opts Options) *Scheduler {
	return &Scheduler{
		repo:     repo,
		executor: executor,
		queue:    NewQueue(2048),
		log:      logger.Component("scheduler"),
		opts:     opts.normalized(),
	}
}

// Queue 返回底层队列
func (s *Scheduler) Queue() *Queue { return s.queue }

// IsRunning 是否正在运行
func (s *Scheduler) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Workers 返回当前 worker 数量
func (s *Scheduler) Workers() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workers
}

// Start 启动调度循环与 worker pool
func (s *Scheduler) Start(parent context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("监控器已在运行")
	}
	ctx, cancel := context.WithCancel(parent)
	s.ctx, s.cancel = ctx, cancel
	s.stopWork = make(chan struct{})
	s.running = true
	s.startedAt = time.Now()
	workers := s.opts.Workers
	s.workers = 0
	s.mu.Unlock()

	if n, err := s.repo.BackfillSchedule(ctx, s.opts.DefaultInterval); err != nil {
		s.log.Warn(logger.Fields{"error": err.Error()}, "补齐 next_check_at 失败")
	} else if n > 0 {
		s.log.Info(logger.Fields{"count": n}, "已为历史域名补齐下次检查时间")
	}

	s.addWorkers(workers)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.loop(ctx)
	}()

	s.log.Info(logger.Fields{"count": workers, "poll_interval": s.opts.PollInterval.String()}, "调度器已启动")
	return nil
}

// Stop 停止调度器，等待正在执行的任务结束
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	cancel := s.cancel
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	s.wg.Wait()

	s.mu.Lock()
	s.workers = 0
	s.mu.Unlock()
	s.queue.Drain()
	s.log.Info(nil, "调度器已停止")
}

// SetConcurrency 热更新 worker 数量
func (s *Scheduler) SetConcurrency(n int) {
	if n <= 0 {
		n = 1
	}
	s.mu.Lock()
	s.opts.Workers = n
	if !s.running {
		s.mu.Unlock()
		return
	}
	diff := n - s.workers
	stop := s.stopWork
	s.mu.Unlock()

	switch {
	case diff > 0:
		s.addWorkers(diff)
	case diff < 0:
		for i := 0; i < -diff; i++ {
			select {
			case stop <- struct{}{}:
			default:
			}
		}
		s.mu.Lock()
		s.workers = n
		s.mu.Unlock()
	}
	s.log.Info(logger.Fields{"count": n}, "并发 worker 数量已更新")
}

// SetPollInterval 热更新扫描间隔
func (s *Scheduler) SetPollInterval(d time.Duration) {
	if d <= 0 {
		return
	}
	s.mu.Lock()
	s.opts.PollInterval = d
	s.mu.Unlock()
}

// SetDefaultInterval 热更新默认检查间隔
func (s *Scheduler) SetDefaultInterval(d time.Duration) {
	if d <= 0 {
		return
	}
	s.mu.Lock()
	s.opts.DefaultInterval = d
	s.mu.Unlock()
}

// Enqueue 把域名加入队列（异步执行）
func (s *Scheduler) Enqueue(name string, priority domain.Priority, reason Reason) bool {
	return s.queue.Offer(Task{Domain: name, Priority: priority, Reason: reason})
}

// RunNow 同步执行一次高优先级查询并等待结果。
//
// 手动"立即检查"走的是同一条 Pipeline，只是优先级更高，因此不会被大量定时
// 任务堵在后面。
func (s *Scheduler) RunNow(ctx context.Context, name string) (*domain.Info, error) {
	if !s.IsRunning() {
		// 监控未启动时直接同步执行，保证手动查询始终可用。
		return s.executor.Execute(ctx, Task{
			Domain: name, Priority: domain.PriorityManual, Reason: ReasonManual, EnqueuedAt: time.Now(),
		})
	}

	result := make(chan Outcome, 1)
	task := Task{
		Domain:     name,
		Priority:   domain.PriorityManual,
		Reason:     ReasonManual,
		EnqueuedAt: time.Now(),
		result:     result,
	}
	if !s.queue.Offer(task) {
		// 已在队列中或队列已满：直接同步执行，避免用户点了没反应。
		return s.executor.Execute(ctx, task)
	}

	select {
	case outcome := <-result:
		return outcome.Info, outcome.Err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Scheduler) addWorkers(n int) {
	for i := 0; i < n; i++ {
		s.mu.Lock()
		if !s.running {
			s.mu.Unlock()
			return
		}
		s.workers++
		ctx, stop := s.ctx, s.stopWork
		s.mu.Unlock()

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.worker(ctx, stop)
		}()
	}
}

func (s *Scheduler) worker(ctx context.Context, stop <-chan struct{}) {
	for {
		task, ok := s.queue.Take(ctx, stop)
		if !ok {
			return
		}

		started := time.Now()
		info, err := s.executor.Execute(ctx, task)

		s.statsMu.Lock()
		s.executed++
		if err != nil {
			s.failed++
		}
		s.lastRunAt = time.Now()
		s.statsMu.Unlock()

		if task.result != nil {
			task.result <- Outcome{Info: info, Err: err}
		}
		if err != nil && ctx.Err() == nil {
			s.log.Warn(logger.Fields{"domain": task.Domain, "error": err.Error(),
				"latency_ms": time.Since(started).Milliseconds()}, "查询任务失败")
		}
	}
}

func (s *Scheduler) loop(ctx context.Context) {
	// 启动后立刻扫一次，避免等一个完整周期
	s.dispatchDue(ctx)

	for {
		s.mu.Lock()
		interval := s.opts.PollInterval
		s.mu.Unlock()

		timer := time.NewTimer(interval)
		select {
		case <-timer.C:
			s.dispatchDue(ctx)
		case <-ctx.Done():
			timer.Stop()
			return
		}
	}
}

func (s *Scheduler) dispatchDue(ctx context.Context) {
	s.mu.Lock()
	batch := s.opts.BatchSize
	s.mu.Unlock()

	if free := s.queue.Free(); free < batch {
		batch = free
	}
	if batch <= 0 {
		return
	}

	due, err := s.repo.DueForCheck(ctx, time.Now(), batch)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Error(logger.Fields{"error": err.Error()}, "扫描到期域名失败")
		}
		return
	}
	if len(due) == 0 {
		return
	}

	enqueued := 0
	for _, d := range due {
		priority := domain.PriorityScheduled
		reason := ReasonScheduled
		if d.RetryCount > 0 {
			priority = domain.PriorityRetry
			reason = ReasonRetry
		}
		if d.Priority > 0 {
			priority += domain.Priority(d.Priority)
		}
		if s.queue.Offer(Task{Domain: d.Name, Priority: priority, Reason: reason}) {
			enqueued++
		}
	}
	if enqueued > 0 {
		s.log.Debug(logger.Fields{"count": enqueued}, "本轮入队的到期域名")
	}
}

// Stats 返回调度统计
func (s *Scheduler) Stats() map[string]any {
	s.statsMu.Lock()
	executed, failed, lastRun := s.executed, s.failed, s.lastRunAt
	startedAt := s.startedAt
	s.statsMu.Unlock()

	manual, retry, scheduled := s.queue.Len()
	stats := map[string]any{
		"running":         s.IsRunning(),
		"workers":         s.Workers(),
		"executed":        executed,
		"failed":          failed,
		"queue_manual":    manual,
		"queue_retry":     retry,
		"queue_scheduled": scheduled,
	}
	if !startedAt.IsZero() {
		stats["uptime"] = time.Since(startedAt).String()
	}
	if !lastRun.IsZero() {
		stats["last_run_at"] = lastRun
	}
	return stats
}
