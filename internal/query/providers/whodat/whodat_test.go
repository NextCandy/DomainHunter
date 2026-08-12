package whodat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/query"
)

func TestQueryMapsRegisteredAndDates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/whois/example.com" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":"example.com","domain":"example.com","isRegistered":true,"registrar":{"name":"Example Registrar"},"status":["clientTransferProhibited"],"nameservers":[{"name":"NS1.EXAMPLE.COM."}],"dates":{"created":"2020-01-02T03:04:05Z","updated":null,"expires":"2030-01-02T03:04:05Z"}}`))
	}))
	defer server.Close()

	result := New(query.ProviderWhoDat, server.URL, "", time.Second).Query(context.Background(), query.Request{Domain: "example.com", TLD: "com"})
	if result.Err != nil || result.Status != domain.StatusRegistered {
		t.Fatalf("registered who-dat result invalid: %+v", result)
	}
	if result.Registrar != "Example Registrar" || result.ExpiryAt == nil || len(result.NameServers) != 1 || result.NameServers[0] != "ns1.example.com" {
		t.Fatalf("who-dat fields were not normalized: %+v", result)
	}
}

func TestQueryMapsAvailableUnlessReserved(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "reserved.example") {
			_, _ = w.Write([]byte(`{"query":"reserved.example","domain":"reserved.example","isRegistered":false,"registrar":{"name":"Registry Policy"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"query":"free.example","domain":"free.example","isRegistered":false}`))
	}))
	defer server.Close()

	provider := New(query.ProviderWhoDat, server.URL, "", time.Second)
	available := provider.Query(context.Background(), query.Request{Domain: "free.example", TLD: "example"})
	if available.Status != domain.StatusAvailable {
		t.Fatalf("explicit isRegistered=false should be available: %+v", available)
	}
	reserved := provider.Query(context.Background(), query.Request{Domain: "reserved.example", TLD: "example"})
	if reserved.Status != domain.StatusUnknown {
		t.Fatalf("reserved marker must remain unknown: %+v", reserved)
	}
}

func TestQueryRequiresRegistrationFlag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":"example.com","domain":"example.com"}`))
	}))
	defer server.Close()

	result := New(query.ProviderWhoDat, server.URL, "", time.Second).Query(context.Background(), query.Request{Domain: "example.com", TLD: "com"})
	if result.Err == nil || query.KindOf(result.Err) != query.KindUnknownResponse {
		t.Fatalf("missing isRegistered must be an unknown-response error: %+v", result)
	}
}
