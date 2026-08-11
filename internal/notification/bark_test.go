package notification

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"DomainHunter/internal/config"
)

func TestCompactBarkBodyRemovesVerboseSections(t *testing.T) {
	body := "域名: example.com\n时间: 2026-08-12 00:00:00\n状态变化: registered → available\n详细信息: " + strings.Repeat("verbose ", 100) + "\n=== WHOIS/RDAP 信息 ===\n" + strings.Repeat("raw ", 100)

	got := compactBarkBody(body)
	if strings.Contains(got, "详细信息:") || strings.Contains(got, "WHOIS/RDAP") || strings.Contains(got, "此消息由") {
		t.Fatalf("Bark 正文仍包含冗余内容: %q", got)
	}
	if len([]rune(got)) > barkBodyLimit+20 {
		t.Fatalf("Bark 正文过长: %d 字符", len([]rune(got)))
	}
}

func TestBarkNotifierSendsCompactedBody(t *testing.T) {
	var received barkPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("解析 Bark 请求失败: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code": 200, "message": "success"}`))
	}))
	defer server.Close()

	notifier := NewBarkNotifier(config.BarkConfig{URL: server.URL, Enabled: true})
	err := notifier.Send(context.Background(), Event{
		Subject: "example.com 状态变化",
		Body:    "域名: example.com\n时间: 2026-08-12 00:00:00\n状态变化: registered → available\n详细信息: " + strings.Repeat("long ", 200) + "\n=== WHOIS/RDAP 信息 ===\nraw",
	})
	if err != nil {
		t.Fatalf("Bark 发送失败: %v", err)
	}
	if received.Title != "example.com 状态变化" {
		t.Fatalf("Bark 标题错误: %q", received.Title)
	}
	if strings.Contains(received.Body, "详细信息:") || strings.Contains(received.Body, "WHOIS/RDAP") {
		t.Fatalf("Bark 请求仍携带冗余正文: %q", received.Body)
	}
}
