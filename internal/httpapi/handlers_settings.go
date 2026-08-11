package httpapi

import (
	"net/http"
	"strings"
	"time"

	"DomainHunter/internal/config"
	"DomainHunter/internal/logger"
)

// handleGetSettings 旧版设置读取。
//
// 安全调整：不再回吐 SMTP 密码与 Telegram Bot Token 明文，只返回"是否已配置"。
// 前端保存时留空即表示不修改。
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
		return
	}
	cfg := s.config()

	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"smtp": map[string]any{
			"host":         cfg.SMTP.Host,
			"port":         cfg.SMTP.Port,
			"user":         cfg.SMTP.User,
			"password_set": cfg.SMTP.Password != "",
			"from":         cfg.SMTP.From,
			"to":           cfg.SMTP.To,
			"enabled":      cfg.SMTP.Enabled,
		},
		"telegram": map[string]any{
			"bot_token_set": cfg.Telegram.BotToken != "",
			"chat_id":       cfg.Telegram.ChatID,
			"enabled":       cfg.Telegram.Enabled,
		},
		"monitor": map[string]any{
			"check_interval":   int(cfg.Monitor.CheckInterval.Seconds()),
			"concurrent_limit": cfg.Monitor.ConcurrentLimit,
			"timeout":          int(cfg.Monitor.Timeout.Seconds()),
		},
		"username": cfg.Server.Username,
	})
}

// handleSettingsV2 返回完整设置（含历史保留策略与查询策略）
func (s *Server) handleSettingsV2(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"smtp": map[string]any{
			"host": cfg.SMTP.Host, "port": cfg.SMTP.Port, "user": cfg.SMTP.User,
			"password_set": cfg.SMTP.Password != "", "from": cfg.SMTP.From,
			"to": cfg.SMTP.To, "enabled": cfg.SMTP.Enabled,
		},
		"telegram": map[string]any{
			"bot_token_set": cfg.Telegram.BotToken != "",
			"chat_id":       cfg.Telegram.ChatID, "enabled": cfg.Telegram.Enabled,
		},
		"bark": map[string]any{
			"url": cfg.Bark.URL, "group": cfg.Bark.Group, "sound": cfg.Bark.Sound,
			"level": cfg.Bark.Level, "icon": cfg.Bark.Icon, "enabled": cfg.Bark.Enabled,
		},
		"feishu": map[string]any{
			"webhook": cfg.Feishu.Webhook, "secret_set": cfg.Feishu.Secret != "",
			"enabled": cfg.Feishu.Enabled,
		},
		"webhook": map[string]any{
			"url": cfg.Webhook.URL, "secret_set": cfg.Webhook.Secret != "",
			"enabled": cfg.Webhook.Enabled,
		},
		"monitor": map[string]any{
			"check_interval":   int(cfg.Monitor.CheckInterval.Seconds()),
			"concurrent_limit": cfg.Monitor.ConcurrentLimit,
			"timeout":          int(cfg.Monitor.Timeout.Seconds()),
		},
		"history": cfg.History,
		"security": map[string]any{
			"cookie_secure":    cfg.Security.CookieSecure,
			"cookie_same_site": cfg.Security.CookieSameSite,
			"cors_origins":     cfg.Security.CORSOrigins,
			"csrf_enabled":     cfg.Security.CSRFEnabled,
		},
		"log_level":    cfg.Log.Level,
		"query_policy": cfg.QueryPolicy,
		"username":     cfg.Server.Username,
		"version":      s.deps.Version,
	})
}

// handleSMTPSettings 保存邮件设置
func (s *Server) handleSMTPSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
		return
	}

	var req struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		User     string `json:"user"`
		Password string `json:"password"`
		From     string `json:"from"`
		To       string `json:"to"`
		Enabled  bool   `json:"enabled"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}

	cfg := s.config()
	smtp := config.SMTPConfig{
		Host: req.Host, Port: req.Port, User: req.User,
		Password: req.Password, From: req.From, To: req.To, Enabled: req.Enabled,
	}
	// 留空表示沿用已保存的密码，避免读取接口不再回吐密码后被清空。
	if strings.TrimSpace(req.Password) == "" {
		smtp.Password = cfg.SMTP.Password
	}

	if err := s.deps.Settings.UpdateSMTP(r.Context(), smtp); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"status": "success", "message": "SMTP设置保存成功"})
}

// handleTelegramSettings 保存 Telegram 设置
func (s *Server) handleTelegramSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
		return
	}

	var req struct {
		BotToken string `json:"bot_token"`
		ChatID   string `json:"chat_id"`
		Enabled  bool   `json:"enabled"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}

	cfg := s.config()
	telegram := config.TelegramConfig{BotToken: req.BotToken, ChatID: req.ChatID, Enabled: req.Enabled}
	if strings.TrimSpace(req.BotToken) == "" {
		telegram.BotToken = cfg.Telegram.BotToken
	}

	if err := s.deps.Settings.UpdateTelegram(r.Context(), telegram); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"status": "success", "message": "Telegram设置保存成功"})
}

// handleBarkSettings 保存 Bark 设置
func (s *Server) handleBarkSettings(w http.ResponseWriter, r *http.Request) {
	var req config.BarkConfig
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if err := s.deps.Settings.UpdateBark(r.Context(), req); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"status": "success", "message": "Bark 设置保存成功"})
}

// handleFeishuSettings 保存飞书机器人设置
func (s *Server) handleFeishuSettings(w http.ResponseWriter, r *http.Request) {
	var req config.FeishuConfig
	if !s.decodeJSON(w, r, &req) {
		return
	}
	// 留空表示沿用已保存的密钥
	if strings.TrimSpace(req.Secret) == "" {
		req.Secret = s.config().Feishu.Secret
	}
	if err := s.deps.Settings.UpdateFeishu(r.Context(), req); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"status": "success", "message": "飞书设置保存成功"})
}

// handleWebhookSettings 保存通用 Webhook 设置
func (s *Server) handleWebhookSettings(w http.ResponseWriter, r *http.Request) {
	var req config.WebhookConfig
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Secret) == "" {
		req.Secret = s.config().Webhook.Secret
	}
	if err := s.deps.Settings.UpdateWebhook(r.Context(), req); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"status": "success", "message": "Webhook 设置保存成功"})
}

// handleChannelTest 按名称测试单个通知渠道
func (s *Server) handleChannelTest(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("channel")
	notifier := s.deps.Notification.Find(name)
	if notifier == nil {
		s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "error", "message": "未找到通知渠道: " + name})
		return
	}
	if !notifier.Enabled() {
		s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "error", "message": "该渠道未启用或未配置完整"})
		return
	}
	if err := notifier.Test(r.Context()); err != nil {
		s.log.Error(logger.Fields{"channel": name, "error": err.Error()}, "测试通知发送失败")
		s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "error", "message": "发送失败: " + err.Error()})
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "success", "message": "测试通知已发送"})
}

// handleMonitorSettings 保存监控参数并热重载
func (s *Server) handleMonitorSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
		return
	}

	var req struct {
		CheckInterval   int `json:"check_interval"`
		ConcurrentLimit int `json:"concurrent_limit"`
		Timeout         int `json:"timeout"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if err := s.deps.Settings.UpdateMonitor(r.Context(), req.CheckInterval, req.ConcurrentLimit, req.Timeout); err != nil {
		s.writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "监控参数保存成功并已热重载",
	})
}

// handleHistorySettings 保存历史保留策略
func (s *Server) handleHistorySettings(w http.ResponseWriter, r *http.Request) {
	req := s.config().History
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if err := s.deps.Settings.UpdateHistory(r.Context(), req); err != nil {
		s.writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"status": "success", "message": "历史保留策略已保存"})
}

// handleQueryPolicy 保存查询策略
func (s *Server) handleQueryPolicy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Policy string `json:"policy"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if err := s.deps.Settings.UpdateQueryPolicy(r.Context(), req.Policy); err != nil {
		s.writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"status": "success", "message": "查询策略已保存"})
}

// handleLogLevel 保存日志级别
func (s *Server) handleLogLevel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Level string `json:"level"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if err := s.deps.Settings.UpdateLogLevel(r.Context(), req.Level); err != nil {
		s.writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"status": "success", "message": "日志级别已更新"})
}

// handleNotificationTest 测试所有已启用的通知渠道。
//
// 兼容性：email_status / telegram_status 两个字段保持不变，
// 新渠道的结果放在 channels 里，旧前端不受影响。
func (s *Server) handleNotificationTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
		return
	}

	channels := map[string]string{}
	enabled, failed := 0, 0
	for _, notifier := range s.deps.Notification.Notifiers() {
		if !notifier.Enabled() {
			channels[notifier.Name()] = "未启用"
			continue
		}
		enabled++
		if err := notifier.Test(r.Context()); err != nil {
			channels[notifier.Name()] = "发送失败: " + err.Error()
			failed++
			s.log.Error(logger.Fields{"channel": notifier.Name(), "error": err.Error()}, "测试通知发送失败")
			continue
		}
		channels[notifier.Name()] = "发送成功"
	}

	response := map[string]any{
		"channels":        channels,
		"email_status":    channels["email"],
		"telegram_status": channels["telegram"],
		"timestamp":       time.Now(),
	}
	switch {
	case enabled == 0:
		response["status"] = "warning"
		response["message"] = "未启用任何通知方式，请先在通知页面配置并启用至少一个渠道"
	case failed == enabled:
		response["status"] = "error"
		response["message"] = "所有通知方式发送失败"
	case failed > 0:
		response["status"] = "warning"
		response["message"] = "部分通知方式发送失败"
	default:
		response["status"] = "success"
		response["message"] = "测试通知发送完成"
	}
	s.writeJSON(w, r, http.StatusOK, response)
}

// handleTestEmail 单独测试邮件
func (s *Server) handleTestEmail(w http.ResponseWriter, r *http.Request) {
	s.testSingleChannel(w, r, "email", s.config().SMTP.Enabled,
		"邮件通知未启用，请先在设置中启用并配置SMTP", "测试邮件发送成功，请检查您的邮箱")
}

// handleTestTelegram 单独测试 Telegram
func (s *Server) handleTestTelegram(w http.ResponseWriter, r *http.Request) {
	s.testSingleChannel(w, r, "telegram", s.config().Telegram.Enabled,
		"Telegram通知未启用，请先在设置中启用并配置Telegram Bot", "测试Telegram消息发送成功，请检查您的Telegram")
}

func (s *Server) testSingleChannel(w http.ResponseWriter, r *http.Request, name string, enabled bool, disabledMsg, okMsg string) {
	if r.Method != http.MethodPost {
		s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "error", "message": "不允许的请求方法"})
		return
	}
	if !enabled {
		s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "error", "message": disabledMsg})
		return
	}
	notifier := s.deps.Notification.Find(name)
	if notifier == nil {
		s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "error", "message": "未找到通知器"})
		return
	}
	if err := notifier.Test(r.Context()); err != nil {
		s.log.Error(logger.Fields{"channel": name, "error": err.Error()}, "测试通知发送失败")
		s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "error", "message": "测试发送失败: " + err.Error()})
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "success", "message": okMsg})
}

// handleNotificationHistory 通知历史
func (s *Server) handleNotificationHistory(w http.ResponseWriter, r *http.Request) {
	records, err := s.deps.Notifications.ListRecent(r.Context(), 100)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"notifications": records})
}
