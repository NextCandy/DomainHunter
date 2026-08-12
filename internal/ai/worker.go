package ai

import (
	"context"
	"sync"
	"time"

	"DomainHunter/internal/logger"
)

// Worker polls the persisted queue; unlike the in-memory domain monitor queue,
// jobs survive a process restart and are recovered through leases.
type Worker struct {
	service *Service
	workers int
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once
}

func NewWorker(service *Service, workers int) *Worker {
	if workers <= 0 {
		workers = 1
	}
	if workers > 5 {
		workers = 5
	} // external model calls must remain intentionally bounded.
	return &Worker{service: service, workers: workers, stop: make(chan struct{}), done: make(chan struct{})}
}

func (w *Worker) Start(ctx context.Context) {
	go func() {
		defer close(w.done)
		if recovered, err := w.service.RecoverLeases(ctx); err != nil {
			logger.Warn("恢复 AI 任务租约失败: %v", err)
		} else if recovered > 0 {
			logger.Warn("已恢复 %d 个过期 AI 任务租约", recovered)
		}
		var group sync.WaitGroup
		for i := 0; i < w.workers; i++ {
			group.Add(1)
			go func() { defer group.Done(); w.loop(ctx) }()
		}
		group.Wait()
	}()
}

func (w *Worker) loop(ctx context.Context) {
	idle := time.NewTimer(0)
	defer idle.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-ctx.Done():
			return
		case <-idle.C:
		}
		worked, err := w.service.ProcessOne(ctx)
		if err != nil {
			logger.Warn("执行 AI 估价任务失败: %v", err)
			idle.Reset(2 * time.Second)
			continue
		}
		if worked {
			idle.Reset(20 * time.Millisecond)
		} else {
			idle.Reset(900 * time.Millisecond)
		}
	}
}

func (w *Worker) Stop(ctx context.Context) error {
	w.once.Do(func() { close(w.stop) })
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
