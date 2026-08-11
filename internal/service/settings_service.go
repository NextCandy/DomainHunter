package service

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"DomainHunter/internal/config"
	"DomainHunter/internal/logger"
	"DomainHunter/internal/query"
	"DomainHunter/internal/repository"
)

// SettingsService 负责配置读写与热更新广播
type SettingsService struct {
	repo repository.SettingsRepository
	log  *logger.Logger

	mu        sync.RWMutex
	cfg       *config.Config
	listeners []func(*config.Config)
}

// NewSettingsService 创建设置服务
func NewSettingsService(repo repository.SettingsRepository, cfg *config.Config) *SettingsService {
	return &SettingsService{repo: repo, cfg: cfg, log: logger.Component("settings")}
}

// Config 返回当前配置快照
func (s *SettingsService) Config() *config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// OnChange 注册配置变更回调
func (s *SettingsService) OnChange(fn func(*config.Config)) {
	s.mu.Lock()
	s.listeners = append(s.listeners, fn)
	s.mu.Unlock()
}

func (s *SettingsService) broadcast(cfg *config.Config) {
	s.mu.Lock()
	s.cfg = cfg
	listeners := append([]func(*config.Config){}, s.listeners...)
	s.mu.Unlock()

	for _, fn := range listeners {
		fn(cfg)
	}
}

// Persist 直接写入若干设置键（认证层迁移密码哈希时使用）
func (s *SettingsService) Persist(ctx context.Context, values map[string]string) error {
	return s.repo.Upsert(ctx, values)
}

// UpdateSMTP 更新邮件配置
func (s *SettingsService) UpdateSMTP(ctx context.Context, smtp config.SMTPConfig) error {
	if err := s.repo.Upsert(ctx, map[string]string{
		config.KeySMTPHost:    smtp.Host,
		config.KeySMTPPort:    strconv.Itoa(smtp.Port),
		config.KeySMTPUser:    smtp.User,
		config.KeySMTPPass:    smtp.Password,
		config.KeySMTPFrom:    smtp.From,
		config.KeySMTPTo:      smtp.To,
		config.KeySMTPEnabled: strconv.FormatBool(smtp.Enabled),
	}); err != nil {
		return fmt.Errorf("保存设置失败: %w", err)
	}

	next := s.Config().Clone()
	next.SMTP = smtp
	s.broadcast(next)
	return nil
}

// UpdateTelegram 更新 Telegram 配置
func (s *SettingsService) UpdateTelegram(ctx context.Context, telegram config.TelegramConfig) error {
	if err := s.repo.Upsert(ctx, map[string]string{
		config.KeyTelegramToken:   telegram.BotToken,
		config.KeyTelegramChatID:  telegram.ChatID,
		config.KeyTelegramEnabled: strconv.FormatBool(telegram.Enabled),
	}); err != nil {
		return fmt.Errorf("保存设置失败: %w", err)
	}

	next := s.Config().Clone()
	next.Telegram = telegram
	s.broadcast(next)
	return nil
}

// UpdateMonitor 更新监控参数（秒）
func (s *SettingsService) UpdateMonitor(ctx context.Context, checkInterval, concurrentLimit, timeout int) error {
	if checkInterval < 5 {
		return fmt.Errorf("检查间隔不能小于5秒")
	}
	if concurrentLimit <= 0 || concurrentLimit > 1000 {
		return fmt.Errorf("并发限制必须在1-1000之间")
	}
	if timeout <= 0 || timeout > 120 {
		return fmt.Errorf("超时时间必须在1-120秒之间")
	}

	if err := s.repo.Upsert(ctx, map[string]string{
		config.KeyCheckInterval:   strconv.Itoa(checkInterval),
		config.KeyConcurrentLimit: strconv.Itoa(concurrentLimit),
		config.KeyTimeout:         strconv.Itoa(timeout),
	}); err != nil {
		return fmt.Errorf("保存设置失败: %w", err)
	}

	next := s.Config().Clone()
	next.Monitor.CheckInterval = time.Duration(checkInterval) * time.Second
	next.Monitor.ConcurrentLimit = concurrentLimit
	next.Monitor.Timeout = time.Duration(timeout) * time.Second
	s.broadcast(next)
	return nil
}

// UpdateHistory 更新历史保留策略
func (s *SettingsService) UpdateHistory(ctx context.Context, history config.HistoryConfig) error {
	if history.RetentionDays < 0 || history.MaxPerDomain < 0 || history.RawMaxBytes < 0 {
		return fmt.Errorf("保留策略不能为负数")
	}
	switch history.RawMode {
	case repository.RawModeAlways, repository.RawModeNever, repository.RawModeChangeOnly:
	default:
		return fmt.Errorf("原始报文保存策略必须是 always / never / change_only")
	}

	if err := s.repo.Upsert(ctx, map[string]string{
		config.KeyHistoryDays:     strconv.Itoa(history.RetentionDays),
		config.KeyHistoryMax:      strconv.Itoa(history.MaxPerDomain),
		config.KeyHistoryRawMode:  history.RawMode,
		config.KeyHistoryRawBytes: strconv.Itoa(history.RawMaxBytes),
	}); err != nil {
		return fmt.Errorf("保存设置失败: %w", err)
	}

	next := s.Config().Clone()
	next.History = history
	s.broadcast(next)
	return nil
}

// UpdateQueryPolicy 更新查询策略（JSON）
func (s *SettingsService) UpdateQueryPolicy(ctx context.Context, raw string) error {
	if _, err := query.LoadConfig(raw); err != nil {
		return err
	}
	if err := s.repo.Upsert(ctx, map[string]string{config.KeyQueryPolicy: raw}); err != nil {
		return fmt.Errorf("保存查询策略失败: %w", err)
	}

	next := s.Config().Clone()
	next.QueryPolicy = raw
	s.broadcast(next)
	return nil
}

// UpdateLogLevel 更新日志级别
func (s *SettingsService) UpdateLogLevel(ctx context.Context, level string) error {
	if err := s.repo.Upsert(ctx, map[string]string{config.KeyLogLevel: level}); err != nil {
		return err
	}
	logger.SetLevel(level)

	next := s.Config().Clone()
	next.Log.Level = level
	s.broadcast(next)
	return nil
}

// SyncCredentials 认证信息变更后同步到内存配置
func (s *SettingsService) SyncCredentials(username string) {
	next := s.Config().Clone()
	next.Server.Username = username
	s.broadcast(next)
}
