package whoisls

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/query"
)

func TestMain(m *testing.M) {
	os.Setenv("DOMAINHUNTER_RDAP_BOOTSTRAP_URL", "http://127.0.0.1:1/rdap.json")
	os.Exit(m.Run())
}

func TestEndpointUsesJSONPath(t *testing.T) {
	endpoint, err := Endpoint("https://whois.ls/json/", "example.im")
	if err != nil {
		t.Fatalf("Endpoint 返回错误: %v", err)
	}
	if endpoint != "https://whois.ls/json/example.im" {
		t.Fatalf("拼接的地址不正确: %s", endpoint)
	}
}

func newTestProvider(t *testing.T, baseURL string) *Provider {
	t.Helper()
	t.Setenv("DOMAINHUNTER_WHOIS_LS_URL", baseURL)
	t.Setenv("DOMAINHUNTER_WHOIS_LS_TLDS", "im")
	return New(5 * time.Second)
}

func TestParsesIMExpiryWithoutCreationDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":false,"data":"Domain Name:\tregistered.im\nDomain Details\nExpiry Date: 30/06/2027 00:59:59\nName Server: ns1.example.test."}`))
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL+"/json/")
	req := query.Request{Domain: "registered.im", TLD: "im"}
	if !provider.Supports(context.Background(), req) {
		t.Fatal(".im 应当被 WHOIS.LS 支持")
	}

	result := provider.Query(context.Background(), req)
	if result.Status != domain.StatusRegistered {
		t.Fatalf("状态判定错误: %+v", result)
	}
	if result.CreatedAt != nil {
		t.Fatalf(".im 没有公开创建日期，必须保持为空: %v", result.CreatedAt)
	}
	if result.ExpiryAt == nil || result.ExpiryAt.Format("2006-01-02 15:04:05") != "2027-06-30 00:59:59" {
		t.Fatalf("到期日解析错误: %+v", result.ExpiryAt)
	}
	if result.Provider != query.ProviderWhoisLS || result.Raw == "" {
		t.Fatalf("查询来源信息丢失: %+v", result)
	}
}

func TestExplicitNotFoundIsAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":false,"data":"Domain Name:\tmissing.im\nThe domain missing.im was not found."}`))
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL+"/json/")
	result := provider.Query(context.Background(), query.Request{Domain: "missing.im", TLD: "im"})
	if result.Status != domain.StatusAvailable {
		t.Fatalf("明确的未找到响应应判定为可注册: %+v", result)
	}
}

func TestDisabledTLDIsNotSupported(t *testing.T) {
	provider := newTestProvider(t, "https://whois.ls/json/")
	if provider.Supports(context.Background(), query.Request{Domain: "example.com", TLD: "com"}) {
		t.Fatal("未启用的后缀不应走 WHOIS.LS")
	}
}

func TestUpstreamErrorBecomesQueryError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":true,"data":"rate limited"}`))
	}))
	defer server.Close()

	provider := newTestProvider(t, server.URL+"/json/")
	result := provider.Query(context.Background(), query.Request{Domain: "any.im", TLD: "im"})
	if result.Status != domain.StatusError || result.Err == nil {
		t.Fatalf("上游错误必须成为查询错误: %+v", result)
	}
}
