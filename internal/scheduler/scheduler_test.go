package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/repository"
)

// fakeRepo 只实现调度器需要的部分 DomainRepository 行为
type fakeRepo struct {
	mu        sync.Mutex
	entries   map[string]*domain.Domain
	scheduled map[string]time.Time
	backfill  int64
}

func newFakeRepo(names ...string) *fakeRepo {
	repo := &fakeRepo{
		entries:   map[string]*domain.Domain{},
		scheduled: map[string]time.Time{},
	}
	past := time.Now().Add(-time.Minute)
	for _, name := range names {
		repo.entries[name] = &domain.Domain{Name: name, Enabled: true, NextCheckAt: &past}
	}
	return repo
}

func (f *fakeRepo) List(context.Context, bool) ([]domain.Domain, error) { return nil, nil }

func (f *fakeRepo) Get(_ context.Context, name string) (*domain.Domain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.entries[name], nil
}

func (f *fakeRepo) Create(context.Context, string, bool, bool) error { return nil }
func (f *fakeRepo) Delete(context.Context, string) error             { return nil }
func (f *fakeRepo) DeleteMany(context.Context, []string) (int64, error) {
	return 0, nil
}
func (f *fakeRepo) Update(context.Context, string, repository.DomainPatch) error { return nil }

func (f *fakeRepo) IsEnabled(_ context.Context, name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.entries[name]
	return ok && entry.Enabled, nil
}

func (f *fakeRepo) DueForCheck(_ context.Context, now time.Time, limit int) ([]domain.Domain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []domain.Domain
	for _, entry := range f.entries {
		if !entry.Enabled {
			continue
		}
		if entry.NextCheckAt != nil && entry.NextCheckAt.After(now) {
			continue
		}
		out = append(out, *entry)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeRepo) ScheduleNext(_ context.Context, name string, next time.Time, retryCount int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scheduled[name] = next
	if entry, ok := f.entries[name]; ok {
		copied := next
		entry.NextCheckAt = &copied
		entry.RetryCount = retryCount
	}
	return nil
}

func (f *fakeRepo) BackfillSchedule(context.Context, time.Duration) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.backfill++
	return 0, nil
}

func (f *fakeRepo) LastNotifiedStatus(context.Context, string) (string, error) { return "", nil }
func (f *fakeRepo) SetLastNotifiedStatus(context.Context, string, string) error {
	return nil
}
func (f *fakeRepo) CleanOrphaned(context.Context) (int64, int64, error) { return 0, 0, nil }
func (f *fakeRepo) Count(context.Context, bool) (int, error)            { return len(f.entries), nil }

// recordingExecutor 记录执行顺序，并把执行过的域名标记为"已排下次"
type recordingExecutor struct {
	mu       sync.Mutex
	executed []Task
	delay    time.Duration
	repo     *fakeRepo
	done     chan struct{}
	target   int
}

func (e *recordingExecutor) Execute(ctx context.Context, task Task) (*domain.Info, error) {
	if e.delay > 0 {
		select {
		case <-time.After(e.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if e.repo != nil {
		_ = e.repo.ScheduleNext(ctx, task.Domain, time.Now().Add(time.Hour), 0)
	}

	e.mu.Lock()
	e.executed = append(e.executed, task)
	count := len(e.executed)
	e.mu.Unlock()

	if e.done != nil && count == e.target {
		close(e.done)
	}
	return &domain.Info{Name: task.Domain, Status: domain.StatusRegistered}, nil
}

func (e *recordingExecutor) names() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, 0, len(e.executed))
	for _, task := range e.executed {
		out = append(out, task.Domain)
	}
	return out
}

func TestSchedulerDispatchesDueDomains(t *testing.T) {
	repo := newFakeRepo("a.com", "b.com")
	done := make(chan struct{})
	exec := &recordingExecutor{repo: repo, done: done, target: 2}

	sched := New(repo, exec, Options{Workers: 2, PollInterval: 20 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := sched.Start(ctx); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	defer sched.Stop()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("到期域名未被执行，已执行: %v", exec.names())
	}
	if repo.backfill == 0 {
		t.Fatal("启动时应补齐 next_check_at")
	}
}

func TestSchedulerSkipsFutureAndDisabledDomains(t *testing.T) {
	repo := newFakeRepo("due.com", "later.com", "off.com")
	future := time.Now().Add(time.Hour)
	repo.entries["later.com"].NextCheckAt = &future
	repo.entries["off.com"].Enabled = false

	done := make(chan struct{})
	exec := &recordingExecutor{repo: repo, done: done, target: 1}

	sched := New(repo, exec, Options{Workers: 2, PollInterval: 20 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	defer sched.Stop()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("到期域名未被执行，已执行: %v", exec.names())
	}

	time.Sleep(150 * time.Millisecond)
	for _, name := range exec.names() {
		if name != "due.com" {
			t.Fatalf("不应执行 %s", name)
		}
	}
}

func TestQueuePrefersManualPriority(t *testing.T) {
	queue := NewQueue(16)
	queue.Offer(Task{Domain: "scheduled.com", Priority: domain.PriorityScheduled, Reason: ReasonScheduled})
	queue.Offer(Task{Domain: "retry.com", Priority: domain.PriorityRetry, Reason: ReasonRetry})
	queue.Offer(Task{Domain: "manual.com", Priority: domain.PriorityManual, Reason: ReasonManual})

	ctx := context.Background()
	stop := make(chan struct{})

	want := []string{"manual.com", "retry.com", "scheduled.com"}
	for _, expected := range want {
		task, ok := queue.Take(ctx, stop)
		if !ok {
			t.Fatal("队列提前结束")
		}
		if task.Domain != expected {
			t.Fatalf("期望 %s，实际 %s", expected, task.Domain)
		}
	}
}

func TestQueueDeduplicatesPendingDomain(t *testing.T) {
	queue := NewQueue(16)
	if !queue.Offer(Task{Domain: "a.com", Priority: domain.PriorityScheduled}) {
		t.Fatal("第一次入队应成功")
	}
	if queue.Offer(Task{Domain: "a.com", Priority: domain.PriorityScheduled}) {
		t.Fatal("同一域名不应重复入队")
	}
	if queue.Size() != 1 {
		t.Fatalf("队列长度应为 1，实际 %d", queue.Size())
	}

	if _, ok := queue.Take(context.Background(), make(chan struct{})); !ok {
		t.Fatal("取出任务失败")
	}
	if !queue.Offer(Task{Domain: "a.com", Priority: domain.PriorityScheduled}) {
		t.Fatal("出队后应可以再次入队")
	}
}

func TestRunNowExecutesWithoutScheduler(t *testing.T) {
	repo := newFakeRepo("manual.com")
	exec := &recordingExecutor{repo: repo}
	sched := New(repo, exec, Options{Workers: 1})

	info, err := sched.RunNow(context.Background(), "manual.com")
	if err != nil {
		t.Fatalf("手动查询失败: %v", err)
	}
	if info == nil || info.Name != "manual.com" {
		t.Fatalf("手动查询结果错误: %+v", info)
	}
}

func TestRunNowJumpsQueue(t *testing.T) {
	repo := newFakeRepo()
	exec := &recordingExecutor{repo: repo, delay: 30 * time.Millisecond}
	sched := New(repo, exec, Options{Workers: 1, PollInterval: time.Hour})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	defer sched.Stop()

	for i := 0; i < 5; i++ {
		sched.Enqueue(nameOf(i), domain.PriorityScheduled, ReasonScheduled)
	}

	runCtx, runCancel := context.WithTimeout(ctx, 5*time.Second)
	defer runCancel()
	if _, err := sched.RunNow(runCtx, "urgent.com"); err != nil {
		t.Fatalf("手动查询失败: %v", err)
	}

	executed := exec.names()
	position := -1
	for i, name := range executed {
		if name == "urgent.com" {
			position = i
			break
		}
	}
	if position == -1 {
		t.Fatalf("手动任务未执行: %v", executed)
	}
	if position > 2 {
		t.Fatalf("手动任务应尽快插队执行，实际排在第 %d 位: %v", position+1, executed)
	}
}

func TestSchedulerStopIsGraceful(t *testing.T) {
	repo := newFakeRepo("a.com")
	exec := &recordingExecutor{repo: repo, delay: 50 * time.Millisecond}
	sched := New(repo, exec, Options{Workers: 2, PollInterval: 20 * time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	time.Sleep(80 * time.Millisecond)

	stopped := make(chan struct{})
	go func() {
		sched.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop 没有在合理时间内返回")
	}
	if sched.IsRunning() {
		t.Fatal("停止后 IsRunning 应为 false")
	}
	if sched.Workers() != 0 {
		t.Fatalf("停止后 worker 数量应归零，实际 %d", sched.Workers())
	}
}

func TestSetConcurrencyAdjustsWorkers(t *testing.T) {
	repo := newFakeRepo()
	exec := &recordingExecutor{repo: repo}
	sched := New(repo, exec, Options{Workers: 2, PollInterval: time.Hour})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	defer sched.Stop()

	if sched.Workers() != 2 {
		t.Fatalf("初始 worker 数量应为 2，实际 %d", sched.Workers())
	}
	sched.SetConcurrency(5)
	if sched.Workers() != 5 {
		t.Fatalf("扩容后应为 5，实际 %d", sched.Workers())
	}
	sched.SetConcurrency(2)
	if sched.Workers() != 2 {
		t.Fatalf("缩容后应为 2，实际 %d", sched.Workers())
	}
}

func TestStartTwiceReturnsError(t *testing.T) {
	repo := newFakeRepo()
	sched := New(repo, &recordingExecutor{repo: repo}, Options{Workers: 1, PollInterval: time.Hour})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	defer sched.Stop()

	if err := sched.Start(ctx); err == nil {
		t.Fatal("重复启动应返回错误")
	}
}

func nameOf(i int) string {
	return string(rune('a'+i)) + ".com"
}

// 确保 fakeRepo 满足接口约束
var _ repository.DomainRepository = (*fakeRepo)(nil)
