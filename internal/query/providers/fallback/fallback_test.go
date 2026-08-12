package fallback

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

func TestEndpointUsesAPIPathAndQuery(t *testing.T) {
	endpoint, err := Endpoint("http://whois.example.test", "example.im")
	if err != nil {
		t.Fatalf("Endpoint 返回错误: %v", err)
	}
	if endpoint != "http://whois.example.test/api/?domain=example.im&rdap=1&whois=1" {
		t.Fatalf("拼接的地址不正确: %s", endpoint)
	}
}

func newTestProvider(t *testing.T, baseURL string) *Provider {
	t.Helper()
	// 使用旧变量名，同时验证 PUFF_* 兼容性没有被破坏
	t.Setenv("PUFF_WHOIS_FALLBACK_URL", baseURL)
	t.Setenv("PUFF_WHOIS_FALLBACK_TLDS", "im,do")
	return New(5 * time.Second)
}

func testServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("domain") {
		case "registered.im":
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"registered":true,"reserved":false,"unknown":false,"registrar":"Example Registrar","creationDate":"2020-01-02T03:04:05Z","expirationDate":"2030-01-02T03:04:05Z","updatedDate":"2025-01-02T03:04:05Z","nameServers":["ns1.example.test"],"status":["clientTransferProhibited"],"whoisData":"Domain Name: registered.im\nRegistrant Street: Redacted | Registry Policy"}}`))
		case "redemption.im":
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"registered":true,"reserved":false,"unknown":false,"status":["clientTransferProhibited","redemptionPeriod"],"whoisData":"Domain Status: redemptionPeriod"}}`))
		case "pending-delete.im":
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"registered":true,"reserved":false,"unknown":false,"status":[{"text":"Pending Delete","url":"https://icann.org/epp#pendingDelete"},{"text":"Inactive"}],"whoisData":"Domain Status: pendingDelete"}}`))
		case "available.do":
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"registered":false,"reserved":false,"unknown":false,"whoisData":"No match"}}`))
		case "reserved.do":
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"registered":false,"reserved":true,"unknown":false,"whoisData":"Domain Cannot Be Registered"}}`))
		case "missing-flags.im":
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"registered":false,"whoisData":"No match"}}`))
		default:
			_, _ = w.Write([]byte(`{"code":1,"msg":"timeout","data":null}`))
		}
	}))
}

func TestClassifiesResponses(t *testing.T) {
	server := testServer()
	defer server.Close()

	provider := newTestProvider(t, server.URL+"/api/")
	ctx := context.Background()

	registered := provider.Query(ctx, query.Request{Domain: "registered.im", TLD: "im"})
	if registered.Status != domain.StatusRegistered || registered.Provider != query.ProviderFallback || registered.Registrar == "" {
		t.Fatalf("已注册结果不正确: %+v", registered)
	}
	if registered.CreatedAt == nil || registered.ExpiryAt == nil {
		t.Fatalf("日期未解析: %+v", registered)
	}

	available := provider.Query(ctx, query.Request{Domain: "available.do", TLD: "do"})
	if available.Status != domain.StatusAvailable {
		t.Fatalf("三个标志齐全且为 false 时应判定可注册: %+v", available)
	}

	reserved := provider.Query(ctx, query.Request{Domain: "reserved.do", TLD: "do"})
	if reserved.Status != domain.StatusUnknown || reserved.Raw == "" {
		t.Fatalf("保留域名必须保持未知: %+v", reserved)
	}

	missing := provider.Query(ctx, query.Request{Domain: "missing-flags.im", TLD: "im"})
	if missing.Status == domain.StatusAvailable {
		t.Fatalf("结构化标志不完整时不得判定为可注册: %+v", missing)
	}

	failed := provider.Query(ctx, query.Request{Domain: "other.im", TLD: "im"})
	if failed.Status != domain.StatusError || failed.Err == nil {
		t.Fatalf("上游报错必须成为查询错误: %+v", failed)
	}

	redemption := provider.Query(ctx, query.Request{Domain: "redemption.im", TLD: "im"})
	if redemption.Status != domain.StatusRedemption {
		t.Fatalf("赎回期状态不应被 registered=true 覆盖: %+v", redemption)
	}

	pendingDelete := provider.Query(ctx, query.Request{Domain: "pending-delete.im", TLD: "im"})
	if pendingDelete.Status != domain.StatusPendingDelete {
		t.Fatalf("待删除状态不应被 registered=true 覆盖: %+v", pendingDelete)
	}
}

func TestPrivacyMarkerDoesNotOverrideRegistered(t *testing.T) {
	server := testServer()
	defer server.Close()

	provider := newTestProvider(t, server.URL+"/api/")
	result := provider.Query(context.Background(), query.Request{Domain: "registered.im", TLD: "im"})
	// whoisData 里含 "Registry Policy" 隐私标记，但 registered=true 必须优先
	if result.Status != domain.StatusRegistered {
		t.Fatalf("隐私标记不应把已注册域名判成保留: %+v", result)
	}
}

func TestDisabledTLDIsNotSupported(t *testing.T) {
	server := testServer()
	defer server.Close()

	provider := newTestProvider(t, server.URL+"/api/")
	if provider.Supports(context.Background(), query.Request{Domain: "example.com", TLD: "com"}) {
		t.Fatal("未启用的后缀不应走备用服务")
	}
}
