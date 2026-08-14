package spaceship

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/query"
)

func TestEndpoint(t *testing.T) {
	endpoint, err := Endpoint("https://spaceship.dev/api/v1/", "daydream.im")
	if err != nil {
		t.Fatalf("Endpoint 返回错误: %v", err)
	}
	if endpoint != "https://spaceship.dev/api/v1/domains/daydream.im/available" {
		t.Fatalf("Endpoint 不正确: %s", endpoint)
	}
}

func TestQueryMapsAvailableAndSendsCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/domains/daydream.im/available" {
			t.Fatalf("请求路径不正确: %s", r.URL.Path)
		}
		if r.Header.Get("X-API-Key") != "test-key" || r.Header.Get("X-API-Secret") != "test-secret" {
			t.Fatalf("Spaceship 认证头缺失或错误")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"domain":"daydream.im","result":"available","premiumPricing":[]}`))
	}))
	defer server.Close()

	provider := NewWithConfig(server.URL+"/api/v1", "test-key", "test-secret", time.Second)
	result := provider.Query(context.Background(), query.Request{Domain: "daydream.im", TLD: "im"})
	if result.Err != nil || result.Status != domain.StatusAvailable {
		t.Fatalf("可用响应解析错误: %+v", result)
	}
}

func TestQueryMapsUnavailableToRegistered(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"domain":"daydream.im","result":"unavailable"}`))
	}))
	defer server.Close()

	result := NewWithConfig(server.URL, "test-key", "test-secret", time.Second).
		Query(context.Background(), query.Request{Domain: "daydream.im", TLD: "im"})
	if result.Status != domain.StatusRegistered || result.Confidence != domain.ConfidenceHigh {
		t.Fatalf("不可用响应应按已注册处理: %+v", result)
	}
}

func TestUnknownResponseIsNotAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"domain":"daydream.im","result":"pending"}`))
	}))
	defer server.Close()

	result := NewWithConfig(server.URL, "test-key", "test-secret", time.Second).
		Query(context.Background(), query.Request{Domain: "daydream.im", TLD: "im"})
	if result.Err != nil || result.Status != domain.StatusUnknown {
		t.Fatalf("未知响应必须保持 unknown: %+v", result)
	}
}

func TestSupportsRequiresCredentialsAndTLD(t *testing.T) {
	provider := NewWithConfig("https://spaceship.dev/api/v1", "test-key", "test-secret", time.Second)
	if !provider.Supports(context.Background(), query.Request{Domain: "daydream.im", TLD: "im"}) {
		t.Fatal("完整凭据和 .im 应启用 Spaceship")
	}
	if provider.Supports(context.Background(), query.Request{Domain: "daydream.com", TLD: "com"}) {
		t.Fatal("未启用的 TLD 不应调用 Spaceship")
	}
	if NewWithConfig("https://spaceship.dev/api/v1", "", "test-secret", time.Second).
		Supports(context.Background(), query.Request{Domain: "daydream.im", TLD: "im"}) {
		t.Fatal("缺少 API Key 时不应启用 Spaceship")
	}
}
