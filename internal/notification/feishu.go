package notification

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"DomainHunter/internal/config"
	"DomainHunter/internal/httpx"
)

// FeishuNotifier 通过飞书自定义机器人 webhook 推送。
//
// 机器人安全设置里如果开启了"签名校验"，需要同时填写 Secret；
// 只用"关键词"或"IP 白名单"时留空即可。
type FeishuNotifier struct {
	mu  sync.RWMutex
	cfg config.FeishuConfig
}

type feishuPayload struct {
	MsgType   string            `json:"msg_type"`
	Content   feishuTextContent `json:"content"`
	Timestamp string            `json:"timestamp,omitempty"`
	Sign      string            `json:"sign,omitempty"`
}

type feishuTextContent struct {
	Text string `json:"text"`
}

type feishuResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	// 部分错误返回的是 StatusCode/StatusMessage
	StatusCode    int    `json:"StatusCode"`
	StatusMessage string `json:"StatusMessage"`
}

// NewFeishuNotifier 创建飞书通知器
func NewFeishuNotifier(cfg config.FeishuConfig) *FeishuNotifier { return &FeishuNotifier{cfg: cfg} }

// Name 实现 Notifier
func (f *FeishuNotifier) Name() string { return "feishu" }

// Enabled 实现 Notifier
func (f *FeishuNotifier) Enabled() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.cfg.Enabled && strings.TrimSpace(f.cfg.Webhook) != ""
}

// UpdateConfig 热更新配置
func (f *FeishuNotifier) UpdateConfig(cfg config.FeishuConfig) {
	f.mu.Lock()
	f.cfg = cfg
	f.mu.Unlock()
}

// Send 实现 Notifier
func (f *FeishuNotifier) Send(ctx context.Context, event Event) error {
	text := event.Subject
	if event.Body != "" {
		text += "\n" + event.Body
	}
	return f.push(ctx, text)
}

// Test 发送测试消息
func (f *FeishuNotifier) Test(ctx context.Context) error {
	return f.push(ctx, "DomainHunter 通知测试\n这是一条飞书测试消息。\n时间: "+
		time.Now().Format("2006-01-02 15:04:05"))
}

func (f *FeishuNotifier) push(ctx context.Context, text string) error {
	f.mu.RLock()
	cfg := f.cfg
	f.mu.RUnlock()

	if !cfg.Enabled {
		return fmt.Errorf("飞书通知未启用")
	}
	endpoint := strings.TrimSpace(cfg.Webhook)
	if endpoint == "" {
		return fmt.Errorf("飞书 Webhook 地址不能为空")
	}
	parsed, err := url.ParseRequestURI(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("飞书 Webhook 地址格式无效")
	}

	body := feishuPayload{
		MsgType: "text",
		Content: feishuTextContent{Text: truncate(text, 8000)},
	}
	if secret := strings.TrimSpace(cfg.Secret); secret != "" {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		body.Timestamp = timestamp
		body.Sign = feishuSign(timestamp, secret)
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("编码飞书消息失败: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("创建飞书请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := httpx.Client(30 * time.Second).Do(req)
	if err != nil {
		return fmt.Errorf("发送飞书请求失败: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return fmt.Errorf("读取飞书响应失败: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("飞书 HTTP 错误 [%d]: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var result feishuResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("解析飞书响应失败: %w", err)
	}
	if result.Code != 0 {
		return fmt.Errorf("飞书接口错误 [%d]: %s", result.Code, result.Msg)
	}
	if result.StatusCode != 0 {
		return fmt.Errorf("飞书接口错误 [%d]: %s", result.StatusCode, result.StatusMessage)
	}
	return nil
}

// feishuSign 按飞书文档计算签名：
// 以 "{timestamp}\n{secret}" 作为 HMAC-SHA256 的密钥，对空字符串求值后 base64。
func feishuSign(timestamp, secret string) string {
	mac := hmac.New(sha256.New, []byte(timestamp+"\n"+secret))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
