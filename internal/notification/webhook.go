package notification

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"DomainHunter/internal/config"
	"DomainHunter/internal/httpx"
)

// WebhookNotifier 把事件原样 POST 到自定义地址，用于对接 n8n、企业微信中转、
// 自建脚本等场景。配置 Secret 后会带上 X-DomainHunter-Signature 头。
type WebhookNotifier struct {
	mu  sync.RWMutex
	cfg config.WebhookConfig
}

// NewWebhookNotifier 创建 Webhook 通知器
func NewWebhookNotifier(cfg config.WebhookConfig) *WebhookNotifier {
	return &WebhookNotifier{cfg: cfg}
}

// Name 实现 Notifier
func (w *WebhookNotifier) Name() string { return "webhook" }

// Enabled 实现 Notifier
func (w *WebhookNotifier) Enabled() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.cfg.Enabled && strings.TrimSpace(w.cfg.URL) != ""
}

// UpdateConfig 热更新配置
func (w *WebhookNotifier) UpdateConfig(cfg config.WebhookConfig) {
	w.mu.Lock()
	w.cfg = cfg
	w.mu.Unlock()
}

// Send 实现 Notifier
func (w *WebhookNotifier) Send(ctx context.Context, event Event) error {
	return w.post(ctx, event)
}

// Test 发送测试事件
func (w *WebhookNotifier) Test(ctx context.Context) error {
	event := Event{
		Type:      "test",
		Domain:    "example.com",
		Status:    "registered",
		OldStatus: "unknown",
		Message:   "DomainHunter Webhook 测试",
		Timestamp: time.Now(),
	}
	event.Subject = formatSubject(event)
	event.Body = formatBody(event)
	return w.post(ctx, event)
}

func (w *WebhookNotifier) post(ctx context.Context, event Event) error {
	w.mu.RLock()
	cfg := w.cfg
	w.mu.RUnlock()

	if !cfg.Enabled {
		return fmt.Errorf("Webhook 通知未启用")
	}
	endpoint := strings.TrimSpace(cfg.URL)
	if endpoint == "" {
		return fmt.Errorf("Webhook 地址不能为空")
	}
	parsed, err := url.ParseRequestURI(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("Webhook 地址格式无效")
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("编码 Webhook 消息失败: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("创建 Webhook 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", "DomainHunter")
	if secret := strings.TrimSpace(cfg.Secret); secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(payload)
		req.Header.Set("X-DomainHunter-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := httpx.Client(30 * time.Second).Do(req)
	if err != nil {
		return fmt.Errorf("发送 Webhook 请求失败: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Webhook HTTP 错误 [%d]: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}
