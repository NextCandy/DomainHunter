package notification

import (
	"context"
	"sync"
	"time"

	"DomainHunter/internal/logger"
	"DomainHunter/internal/repository"
)

const (
	// groupWindow 一个通知组最多等待多久
	groupWindow = 10 * time.Second
	// idleWindow 若这么久没有新域名进入查询，就提前发出
	idleWindow = 8 * time.Second
)

// Aggregator 通知聚合器。
//
// 一轮调度往往会同时产生多个域名的状态变化，逐条发送会刷屏。这里把短时间内
// 的变化合并成一条：最多等 groupWindow，或者 idleWindow 内没有新查询就提前发。
type Aggregator struct {
	mgr     *Manager
	history repository.NotificationRepository
	log     *logger.Logger

	mu            sync.Mutex
	pending       []Event
	lastQueryTime time.Time

	eventCh  chan Event
	stopCh   chan struct{}
	doneCh   chan struct{}
	running  bool
	stopOnce sync.Once
}

// NewAggregator 创建聚合器
func NewAggregator(mgr *Manager, history repository.NotificationRepository) *Aggregator {
	return &Aggregator{
		mgr:     mgr,
		history: history,
		log:     logger.Component("notification"),
		eventCh: make(chan Event, 1000),
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
}

// Start 启动聚合器
func (a *Aggregator) Start() {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return
	}
	a.running = true
	a.mu.Unlock()

	go a.run()
	a.log.Info(nil, "通知聚合器已启动")
}

// Stop 停止聚合器，并把待发送的通知立刻发出
func (a *Aggregator) Stop() {
	a.stopOnce.Do(func() {
		a.mu.Lock()
		running := a.running
		a.running = false
		a.mu.Unlock()
		if !running {
			return
		}
		close(a.stopCh)
		<-a.doneCh
		a.log.Info(nil, "通知聚合器已停止")
	})
}

// Add 加入一个状态变化事件
func (a *Aggregator) Add(event Event) {
	a.mu.Lock()
	running := a.running
	a.mu.Unlock()
	if !running {
		return
	}
	select {
	case a.eventCh <- event:
	default:
		a.log.Warn(logger.Fields{"domain": event.Domain}, "通知聚合器队列已满，丢弃通知")
	}
}

// RecordQuery 记录域名开始查询的时间
func (a *Aggregator) RecordQuery(string) {
	a.mu.Lock()
	a.lastQueryTime = time.Now()
	a.mu.Unlock()
}

func (a *Aggregator) run() {
	defer close(a.doneCh)

	groupTimer := time.NewTimer(time.Hour)
	groupTimer.Stop()
	idleTicker := time.NewTicker(time.Second)
	defer idleTicker.Stop()

	for {
		select {
		case event := <-a.eventCh:
			if a.accept(event) {
				a.mu.Lock()
				first := len(a.pending) == 1
				a.mu.Unlock()
				if first {
					if !groupTimer.Stop() {
						select {
						case <-groupTimer.C:
						default:
						}
					}
					groupTimer.Reset(groupWindow)
				}
			}

		case <-groupTimer.C:
			a.flush()

		case <-idleTicker.C:
			a.mu.Lock()
			shouldFlush := len(a.pending) > 0 && time.Since(a.lastQueryTime) >= idleWindow
			a.mu.Unlock()
			if shouldFlush {
				a.log.Info(nil, "%d 秒内无新域名查询，提前发送通知组", int(idleWindow.Seconds()))
				groupTimer.Stop()
				a.flush()
			}

		case <-a.stopCh:
			groupTimer.Stop()
			a.flush()
			return
		}
	}
}

// accept 做去重与首查抑制；返回 true 表示事件已进入待发送列表
func (a *Aggregator) accept(event Event) bool {
	// 首次查询没有旧状态，不通知，避免初始化时把所有域名刷一遍。
	if event.OldStatus == "" || event.OldStatus == "unknown" {
		a.log.Info(logger.Fields{"domain": event.Domain, "status": event.Status}, "首次查询，不发送通知")
		return false
	}
	if event.OldStatus == event.Status {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if last, err := a.history.Last(ctx, event.Domain); err != nil {
		a.log.Error(logger.Fields{"domain": event.Domain, "error": err.Error()}, "获取最后通知记录失败")
	} else if last != nil && last.Status == event.Status && last.OldStatus == event.OldStatus {
		a.log.Info(logger.Fields{"domain": event.Domain, "status": event.Status},
			"该状态变化已通知过，跳过")
		return false
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	for _, pending := range a.pending {
		if pending.Domain == event.Domain {
			return false
		}
	}
	a.pending = append(a.pending, event)
	a.log.Info(logger.Fields{"domain": event.Domain, "status": event.Status, "count": len(a.pending)},
		"域名加入通知组")
	return true
}

func (a *Aggregator) flush() {
	a.mu.Lock()
	pending := a.pending
	a.pending = nil
	a.mu.Unlock()

	if len(pending) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, event := range pending {
		if err := a.history.SaveEvent(ctx, event.Domain, event.Status, event.OldStatus, EventCategory(event)); err != nil {
			a.log.Error(logger.Fields{"domain": event.Domain, "error": err.Error()}, "保存通知历史失败")
		}
	}

	a.log.Info(logger.Fields{"count": len(pending)}, "发送通知组")
	a.mgr.enqueueBatch(pending)
}
