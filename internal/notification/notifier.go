// Package notification 负责把域名状态变化送到各个通知渠道。
//
// 渠道通过 Notifier 接口接入，新增 Webhook / Bark / Discord / Slack 只需要实现
// 该接口并注册，不必改动聚合、去重与格式化逻辑。
package notification

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"DomainHunter/internal/config"
	"DomainHunter/internal/logger"
	"DomainHunter/internal/repository"
)

// ErrNoEnabledNotifiers 表示当前没有可发送的通知渠道。
// 每日摘要遇到此错误不会写入 last_sent_at，待渠道配置好后会重试。
var ErrNoEnabledNotifiers = errors.New("没有启用的通知渠道")

// Event 一次通知事件
type Event struct {
	Type      string    `json:"type"`
	Domain    string    `json:"domain"`
	Status    string    `json:"status"`
	OldStatus string    `json:"old_status"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	WhoisRaw  string    `json:"whois_raw,omitempty"`

	// Batch 非空时表示这是一条合并通知
	Batch []Event `json:"batch,omitempty"`

	// Subject/Body 由 Manager 统一格式化后填入
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// Notifier 通知渠道
type Notifier interface {
	// Name 渠道名称，如 email / telegram
	Name() string
	// Enabled 当前是否启用
	Enabled() bool
	// Send 发送一条通知
	Send(ctx context.Context, event Event) error
	// Test 发送测试通知
	Test(ctx context.Context) error
}

// Manager 通知管理器
type Manager struct {
	mu        sync.RWMutex
	notifiers []Notifier
	enabled   bool
	rules     []repository.NotificationRule
	templates []repository.NotificationTemplate

	queue      chan Event
	wg         sync.WaitGroup
	stopOnce   sync.Once
	aggregator *Aggregator
	history    repository.NotificationRepository
	log        *logger.Logger
}

// NewManager 创建通知管理器
func NewManager(history repository.NotificationRepository) *Manager {
	m := &Manager{
		enabled: true,
		queue:   make(chan Event, 1000),
		history: history,
		log:     logger.Component("notification"),
	}
	m.aggregator = NewAggregator(m, history)
	return m
}

// RegisterAll 按配置创建并注册全部内置渠道。
// 渠道始终注册，是否发送由各自的 Enabled() 决定，这样设置页可以随时开关。
func (m *Manager) RegisterAll(cfg *config.Config) {
	m.Register(NewEmailNotifier(cfg.SMTP))
	m.Register(NewTelegramNotifier(cfg.Telegram))
	m.Register(NewBarkNotifier(cfg.Bark))
	m.Register(NewFeishuNotifier(cfg.Feishu))
	m.Register(NewWebhookNotifier(cfg.Webhook))
}

// Register 注册任意通知渠道
func (m *Manager) Register(n Notifier) {
	if n == nil {
		return
	}
	m.mu.Lock()
	m.notifiers = append(m.notifiers, n)
	m.mu.Unlock()
}

// SetRules 热更新通知过滤规则。规则为空或没有启用规则时保持原有通知语义。
func (m *Manager) SetRules(rules []repository.NotificationRule) {
	m.mu.Lock()
	m.rules = append([]repository.NotificationRule(nil), rules...)
	m.mu.Unlock()
}

// SetTemplates 热更新通知模板。
func (m *Manager) SetTemplates(templates []repository.NotificationTemplate) {
	m.mu.Lock()
	m.templates = append([]repository.NotificationTemplate(nil), templates...)
	m.mu.Unlock()
}

// ApplyConfig 把最新配置广播给所有渠道。
//
// 新增渠道时只需要在这里补一个 case，聚合、去重与格式化都不用动。
func (m *Manager) ApplyConfig(cfg *config.Config) {
	for _, n := range m.Notifiers() {
		switch channel := n.(type) {
		case *EmailNotifier:
			channel.UpdateConfig(cfg.SMTP)
		case *TelegramNotifier:
			channel.UpdateConfig(cfg.Telegram)
		case *BarkNotifier:
			channel.UpdateConfig(cfg.Bark)
		case *FeishuNotifier:
			channel.UpdateConfig(cfg.Feishu)
		case *WebhookNotifier:
			channel.UpdateConfig(cfg.Webhook)
		}
	}
}

// Notifiers 返回全部渠道
func (m *Manager) Notifiers() []Notifier {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]Notifier(nil), m.notifiers...)
}

// Find 按名称查找渠道
func (m *Manager) Find(name string) Notifier {
	for _, n := range m.Notifiers() {
		if n.Name() == name {
			return n
		}
	}
	return nil
}

// EnabledNames 返回已启用的渠道名称
func (m *Manager) EnabledNames() []string {
	var out []string
	for _, n := range m.Notifiers() {
		if n.Enabled() {
			out = append(out, n.Name())
		}
	}
	return out
}

// Start 启动发送协程与聚合器
func (m *Manager) Start() {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		for event := range m.queue {
			m.dispatch(event)
		}
	}()
	m.aggregator.Start()
}

// Stop 停止聚合器并等待队列排空
func (m *Manager) Stop() {
	m.stopOnce.Do(func() {
		m.aggregator.Stop()

		m.mu.Lock()
		m.enabled = false
		m.mu.Unlock()

		close(m.queue)
		m.wg.Wait()
	})
}

func (m *Manager) isEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled
}

// Submit 提交状态变化事件（走聚合器合并）
func (m *Manager) Submit(event Event) {
	if !m.Allows(event) {
		return
	}
	if event.Type == "status_change" {
		m.aggregator.Add(event)
		return
	}
	m.enqueue(event)
}

// SendDigest 同步发送一条每日摘要。
//
// 状态变化通知继续走异步聚合队列；每日摘要需要确认各渠道发送结果后才能
// 持久化 last_sent_at，因此单独提供同步入口。它仍然复用 Manager 当前已注册
// 渠道、启用状态、摘要专用规则与模板配置，不占用查询调度器的 worker。
func (m *Manager) SendDigest(ctx context.Context, event Event) error {
	if !m.isEnabled() {
		return errors.New("通知管理器已停止")
	}
	if event.Type == "" {
		event.Type = "notification_digest"
	}
	if !m.allowsDigest(event) {
		m.log.Info(nil, "每日通知摘要被通知规则抑制")
		return nil
	}

	notifiers := m.Notifiers()
	enabled := make([]Notifier, 0, len(notifiers))
	for _, n := range notifiers {
		if n.Enabled() {
			enabled = append(enabled, n)
		}
	}
	if len(enabled) == 0 {
		return ErrNoEnabledNotifiers
	}

	event.Subject = formatSubject(event)
	event.Body = formatBody(event)
	m.applyTemplate(&event)

	if ctx == nil {
		ctx = context.Background()
	}
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for _, n := range enabled {
		wg.Add(1)
		go func(n Notifier) {
			defer wg.Done()
			if err := n.Send(ctx, event); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s: %w", n.Name(), err))
				mu.Unlock()
			}
		}(n)
	}
	wg.Wait()
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// Allows 判断事件是否会通过当前规则过滤器；调用方可在写入通知去重状态前预检。
func (m *Manager) Allows(event Event) bool {
	return m.isEnabled() && m.allowed(event)
}

// RecordQuery 记录域名开始查询，用于聚合器判断"是否还有新查询在进行"
func (m *Manager) RecordQuery(name string) { m.aggregator.RecordQuery(name) }

func (m *Manager) enqueue(event Event) {
	if !m.isEnabled() {
		return
	}
	event.Subject = formatSubject(event)
	event.Body = formatBody(event)
	m.applyTemplate(&event)

	select {
	case m.queue <- event:
	default:
		m.log.Warn(logger.Fields{"domain": event.Domain}, "通知队列已满，丢弃通知")
	}
}

func (m *Manager) allowed(event Event) bool {
	m.mu.RLock()
	rules := append([]repository.NotificationRule(nil), m.rules...)
	m.mu.RUnlock()
	active := false
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		active = true
		if len(rule.Statuses) > 0 && !containsStatus(rule.Statuses, event.Status) {
			continue
		}
		if inSilenceWindow(rule.SilenceStart, rule.SilenceEnd, time.Now()) {
			continue
		}
		return true
	}
	return !active
}

// allowsDigest 只应用明确标记为 digest_enabled 的规则；若没有摘要专用规则，
// 保持摘要设置原有语义，避免普通状态通知规则意外吞掉每日摘要。
func (m *Manager) allowsDigest(event Event) bool {
	m.mu.RLock()
	rules := append([]repository.NotificationRule(nil), m.rules...)
	m.mu.RUnlock()

	hasDigestRule := false
	for _, rule := range rules {
		if !rule.Enabled || !rule.DigestEnabled {
			continue
		}
		hasDigestRule = true
		if inSilenceWindow(rule.SilenceStart, rule.SilenceEnd, time.Now()) {
			continue
		}
		if len(rule.Statuses) == 0 {
			return true
		}
		if containsStatus(rule.Statuses, event.Status) {
			return true
		}
		for _, item := range event.Batch {
			if containsStatus(rule.Statuses, item.Status) {
				return true
			}
		}
	}
	return !hasDigestRule
}

func containsStatus(statuses []string, status string) bool {
	for _, item := range statuses {
		if strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(status)) {
			return true
		}
	}
	return false
}

func inSilenceWindow(start, end string, now time.Time) bool {
	parse := func(raw string) (int, bool) {
		parts := strings.Split(strings.TrimSpace(raw), ":")
		if len(parts) != 2 {
			return 0, false
		}
		hour, hourErr := strconv.Atoi(parts[0])
		minute, minuteErr := strconv.Atoi(parts[1])
		if hourErr != nil || minuteErr != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
			return 0, false
		}
		return hour*60 + minute, true
	}
	from, okFrom := parse(start)
	to, okTo := parse(end)
	if !okFrom || !okTo {
		return false
	}
	current := now.Hour()*60 + now.Minute()
	if from <= to {
		return current >= from && current < to
	}
	return current >= from || current < to
}

func (m *Manager) applyTemplate(event *Event) {
	if event == nil {
		return
	}
	m.mu.RLock()
	templates := append([]repository.NotificationTemplate(nil), m.templates...)
	m.mu.RUnlock()
	for _, template := range templates {
		if !template.Enabled || (template.EventType != "" && template.EventType != event.Type) {
			continue
		}
		if template.Subject != "" {
			event.Subject = renderTemplate(template.Subject, *event)
		}
		if template.Body != "" {
			event.Body = renderTemplate(template.Body, *event)
		}
		return
	}
}

func renderTemplate(raw string, event Event) string {
	replacer := strings.NewReplacer(
		"{{domain}}", event.Domain,
		"{{status}}", event.Status,
		"{{old_status}}", event.OldStatus,
		"{{message}}", event.Message,
		"{{time}}", event.Timestamp.Format("2006-01-02 15:04:05"),
	)
	return replacer.Replace(raw)
}

// enqueueBatch 提交合并后的批量通知
func (m *Manager) enqueueBatch(events []Event) {
	if !m.isEnabled() || len(events) == 0 {
		return
	}
	if len(events) == 1 {
		m.enqueue(events[0])
		return
	}
	batch := Event{
		Type:      "status_change_batch",
		Timestamp: time.Now(),
		Batch:     events,
	}
	batch.Subject = formatSubject(batch)
	batch.Body = formatBody(batch)

	select {
	case m.queue <- batch:
	default:
		m.log.Warn(logger.Fields{"count": len(events)}, "通知队列已满，丢弃批量通知")
	}
}

func (m *Manager) dispatch(event Event) {
	notifiers := m.Notifiers()
	var wg sync.WaitGroup
	for _, n := range notifiers {
		if !n.Enabled() {
			continue
		}
		wg.Add(1)
		go func(n Notifier) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			if err := n.Send(ctx, event); err != nil {
				// 某些 SMTP 服务器发送成功后会回一个不完整响应，不视为失败。
				if strings.Contains(err.Error(), "short response") {
					return
				}
				m.log.Error(logger.Fields{"channel": n.Name(), "domain": event.Domain, "error": err.Error()},
					"发送通知失败")
				return
			}
			m.log.Info(logger.Fields{"channel": n.Name(), "domain": event.Domain, "count": len(event.Batch)},
				"通知发送成功")
		}(n)
	}
	wg.Wait()
}

// Stats 返回通知统计
func (m *Manager) Stats() map[string]any {
	return map[string]any{
		"enabled":           m.isEnabled(),
		"notifier_count":    len(m.Notifiers()),
		"queue_length":      len(m.queue),
		"queue_capacity":    cap(m.queue),
		"enabled_notifiers": m.EnabledNames(),
	}
}

func formatSubject(event Event) string {
	if event.Type == "notification_digest" {
		return fmt.Sprintf("域名每日状态摘要 (%d个域名)", len(event.Batch))
	}
	if len(event.Batch) > 0 {
		return fmt.Sprintf("域名状态变化通知 (%d个域名)", len(event.Batch))
	}
	switch event.Type {
	case "status_change":
		return fmt.Sprintf("%s 状态变化", event.Domain)
	case "available":
		return fmt.Sprintf("%s 可注册！", event.Domain)
	case "redemption":
		return fmt.Sprintf("%s 进入赎回期", event.Domain)
	case "pending_delete":
		return fmt.Sprintf("%s 进入待删除期", event.Domain)
	case "error":
		return fmt.Sprintf("%s 查询失败", event.Domain)
	default:
		return fmt.Sprintf("%s 通知", event.Domain)
	}
}

func formatBody(event Event) string {
	var b strings.Builder

	if event.Type == "notification_digest" {
		b.WriteString(fmt.Sprintf("前一天检测到 %d 个域名状态发生变化\n", len(event.Batch)))
		b.WriteString(fmt.Sprintf("时间: %s\n\n", event.Timestamp.Format("2006-01-02 15:04:05")))
		for i, item := range event.Batch {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, item.Domain))
			if item.OldStatus != "" {
				b.WriteString(fmt.Sprintf("   状态变化: %s → %s\n", item.OldStatus, item.Status))
			} else {
				b.WriteString(fmt.Sprintf("   当前状态: %s\n", item.Status))
			}
		}
		b.WriteString("\n---\n此消息由 DomainHunter 自动发送")
		return b.String()
	}

	if len(event.Batch) > 0 {
		b.WriteString(fmt.Sprintf("检测到 %d 个域名状态发生变化\n", len(event.Batch)))
		b.WriteString(fmt.Sprintf("时间: %s\n\n", event.Timestamp.Format("2006-01-02 15:04:05")))
		for i, item := range event.Batch {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, item.Domain))
			b.WriteString(fmt.Sprintf("   状态变化: %s → %s\n", item.OldStatus, item.Status))
			if i < len(event.Batch)-1 {
				b.WriteString("\n")
			}
		}
		b.WriteString("\n---\n此消息由 DomainHunter 自动发送")
		return b.String()
	}

	b.WriteString(fmt.Sprintf("域名: %s\n", event.Domain))
	b.WriteString(fmt.Sprintf("时间: %s\n", event.Timestamp.Format("2006-01-02 15:04:05")))

	switch event.Type {
	case "status_change":
		b.WriteString(fmt.Sprintf("状态变化: %s → %s\n", event.OldStatus, event.Status))
	case "available":
		b.WriteString("状态: 可注册\n此域名现在可以注册！\n")
	case "redemption":
		b.WriteString("状态: 赎回期\n此域名现在处于赎回期，可以尝试赎回。\n")
	case "pending_delete":
		b.WriteString("状态: 待删除\n此域名即将删除，进入抢注阶段！\n")
	case "error":
		b.WriteString(fmt.Sprintf("状态: 查询失败\n错误信息: %s\n", event.Message))
	}

	if event.Message != "" && event.Type != "error" {
		b.WriteString(fmt.Sprintf("\n详细信息: %s\n", event.Message))
	}
	if event.WhoisRaw != "" {
		b.WriteString("\n=== WHOIS/RDAP 信息 ===\n")
		if len(event.WhoisRaw) > 2000 {
			b.WriteString(event.WhoisRaw[:2000] + "\n...(已截断)")
		} else {
			b.WriteString(event.WhoisRaw)
		}
		b.WriteString("\n")
	}

	b.WriteString("\n---\n此消息由 DomainHunter 自动发送")
	return b.String()
}
