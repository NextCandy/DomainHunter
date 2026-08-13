package notification

import (
	"bytes"
	"context"
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

// BarkNotifier 通过 Bark 推送到 iOS。
//
// 配置里的 URL 是完整的推送地址（含设备 key），既支持官方 api.day.app，
// 也支持自建服务器。
type BarkNotifier struct {
	mu  sync.RWMutex
	cfg config.BarkConfig
}

type barkPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Group string `json:"group,omitempty"`
	Sound string `json:"sound,omitempty"`
	Icon  string `json:"icon,omitempty"`
	Level string `json:"level,omitempty"`
}

type barkResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const barkBodyLimit = 480

// NewBarkNotifier 创建 Bark 通知器
func NewBarkNotifier(cfg config.BarkConfig) *BarkNotifier { return &BarkNotifier{cfg: cfg} }

// Name 实现 Notifier
func (b *BarkNotifier) Name() string { return "bark" }

// Enabled 实现 Notifier
func (b *BarkNotifier) Enabled() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.cfg.Enabled && strings.TrimSpace(b.cfg.URL) != ""
}

// UpdateConfig 热更新配置
func (b *BarkNotifier) UpdateConfig(cfg config.BarkConfig) {
	b.mu.Lock()
	b.cfg = cfg
	b.mu.Unlock()
}

// Send 实现 Notifier
func (b *BarkNotifier) Send(ctx context.Context, event Event) error {
	return b.push(ctx, event.Subject, event.Body)
}

// Test 发送测试推送
func (b *BarkNotifier) Test(ctx context.Context) error {
	return b.push(ctx, "DomainHunter 通知测试",
		"这是一条 Bark 测试消息。\n时间: "+time.Now().Format("2006-01-02 15:04:05"))
}

func (b *BarkNotifier) push(ctx context.Context, subject, body string) error {
	b.mu.RLock()
	cfg := b.cfg
	b.mu.RUnlock()

	if !cfg.Enabled {
		return fmt.Errorf("Bark 通知未启用")
	}
	endpoint := strings.TrimSpace(cfg.URL)
	if endpoint == "" {
		return fmt.Errorf("Bark 推送地址不能为空")
	}
	parsed, err := url.ParseRequestURI(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("Bark 推送地址格式无效")
	}

	group := strings.TrimSpace(cfg.Group)
	if group == "" {
		group = "DomainHunter"
	}
	payload, err := json.Marshal(barkPayload{
		Title: subject,
		Body:  compactBarkBody(body),
		Group: group,
		Sound: strings.TrimSpace(cfg.Sound),
		Icon:  strings.TrimSpace(cfg.Icon),
		Level: strings.TrimSpace(cfg.Level),
	})
	if err != nil {
		return fmt.Errorf("编码 Bark 消息失败: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("创建 Bark 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := httpx.Client(30 * time.Second).Do(req)
	if err != nil {
		return fmt.Errorf("发送 Bark 请求失败: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return fmt.Errorf("读取 Bark 响应失败: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Bark HTTP 错误 [%d]: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	var result barkResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		// 自建服务器可能返回非 JSON 的成功响应，HTTP 2xx 已经足够
		return nil
	}
	if result.Code != 0 && result.Code != 200 {
		return fmt.Errorf("Bark API 错误 [%d]: %s", result.Code, result.Message)
	}
	return nil
}

// compactBarkBody keeps Bark notifications readable on a phone. The full event
// remains available in email/Telegram and the notification history; Bark only
// needs the short status summary and should not carry WHOIS/RDAP payloads.
func compactBarkBody(body string) string {
	body = strings.TrimSpace(body)
	const marker = "\n=== WHOIS/RDAP 信息 ==="
	if markerIndex := strings.Index(body, marker); markerIndex >= 0 {
		body = strings.TrimSpace(body[:markerIndex])
	}

	lines := strings.Split(body, "\n")
	compact := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if blank {
				continue
			}
			blank = true
			compact = append(compact, line)
			continue
		}
		blank = false
		if strings.HasPrefix(line, "详细信息:") || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "此消息由") {
			continue
		}
		compact = append(compact, line)
	}
	return truncate(strings.TrimSpace(strings.Join(compact, "\n")), barkBodyLimit)
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	// 按 rune 截断，避免把多字节字符切成乱码
	runes := []rune(text)
	for len(string(runes)) > limit && len(runes) > 0 {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "\n…(已截断)"
}
