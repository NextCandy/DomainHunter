package scheduler

import (
	"context"
	"sync"
	"time"

	"DomainHunter/internal/domain"
)

// Reason 任务来源
type Reason string

const (
	// ReasonScheduled 定时到期
	ReasonScheduled Reason = "scheduled"
	// ReasonManual 用户手动触发
	ReasonManual Reason = "manual"
	// ReasonRetry 失败重试
	ReasonRetry Reason = "retry"
)

// Task 一个查询任务
type Task struct {
	Domain     string
	Priority   domain.Priority
	Reason     Reason
	EnqueuedAt time.Time

	// result 非 nil 时表示调用方在同步等待结果
	result chan Outcome
}

// Outcome 任务执行结果
type Outcome struct {
	Info *domain.Info
	Err  error
}

// Queue 三级优先级队列，并对同一域名去重。
//
// 用三个 channel 而不是堆 + 条件变量：worker 只需要按 manual → retry →
// scheduled 的顺序 select，实现简单且没有唤醒丢失的风险。
type Queue struct {
	manual    chan Task
	retry     chan Task
	scheduled chan Task

	mu      sync.Mutex
	pending map[string]struct{}
}

// NewQueue 创建队列
func NewQueue(capacity int) *Queue {
	if capacity <= 0 {
		capacity = 1024
	}
	return &Queue{
		manual:    make(chan Task, capacity),
		retry:     make(chan Task, capacity),
		scheduled: make(chan Task, capacity),
		pending:   make(map[string]struct{}),
	}
}

// Offer 入队。已在队列中的域名会被忽略，返回 false。
func (q *Queue) Offer(task Task) bool {
	name := domain.Normalize(task.Domain)
	if name == "" {
		return false
	}
	task.Domain = name
	if task.EnqueuedAt.IsZero() {
		task.EnqueuedAt = time.Now()
	}

	q.mu.Lock()
	if _, exists := q.pending[name]; exists {
		q.mu.Unlock()
		return false
	}
	q.pending[name] = struct{}{}
	q.mu.Unlock()

	target := q.scheduled
	switch {
	case task.Priority >= domain.PriorityManual:
		target = q.manual
	case task.Priority >= domain.PriorityRetry:
		target = q.retry
	}

	select {
	case target <- task:
		return true
	default:
		// 队列已满：撤销占位，让调度器下一轮重新尝试。
		q.mu.Lock()
		delete(q.pending, name)
		q.mu.Unlock()
		return false
	}
}

// Take 取出下一个任务，按优先级从高到低。ctx 结束时返回 false。
func (q *Queue) Take(ctx context.Context, stop <-chan struct{}) (Task, bool) {
	// 先做一次非阻塞的高优先级探测，保证手动任务永远插队。
	select {
	case task := <-q.manual:
		return q.dequeued(task), true
	default:
	}
	select {
	case task := <-q.manual:
		return q.dequeued(task), true
	case task := <-q.retry:
		return q.dequeued(task), true
	default:
	}

	select {
	case task := <-q.manual:
		return q.dequeued(task), true
	case task := <-q.retry:
		return q.dequeued(task), true
	case task := <-q.scheduled:
		return q.dequeued(task), true
	case <-stop:
		return Task{}, false
	case <-ctx.Done():
		return Task{}, false
	}
}

func (q *Queue) dequeued(task Task) Task {
	q.mu.Lock()
	delete(q.pending, task.Domain)
	q.mu.Unlock()
	return task
}

// Contains 判断域名是否已在队列中
func (q *Queue) Contains(name string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	_, ok := q.pending[domain.Normalize(name)]
	return ok
}

// Len 返回各优先级的排队数量
func (q *Queue) Len() (manual, retry, scheduled int) {
	return len(q.manual), len(q.retry), len(q.scheduled)
}

// Size 返回排队总数
func (q *Queue) Size() int {
	m, r, s := q.Len()
	return m + r + s
}

// Free 返回 scheduled 通道的剩余容量，调度器据此决定本轮抓取多少域名
func (q *Queue) Free() int { return cap(q.scheduled) - len(q.scheduled) }

// Drain 清空队列（停止监控时使用）
func (q *Queue) Drain() {
	for {
		select {
		case <-q.manual:
		case <-q.retry:
		case <-q.scheduled:
		default:
			q.mu.Lock()
			q.pending = make(map[string]struct{})
			q.mu.Unlock()
			return
		}
	}
}
