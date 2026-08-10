package core

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFallbackEndpointUsesAPIPathAndQuery(t *testing.T) {
	endpoint, err := fallbackEndpoint("http://whois.example.test", "example.im")
	if err != nil {
		t.Fatalf("fallbackEndpoint returned error: %v", err)
	}
	if endpoint != "http://whois.example.test/api/?domain=example.im&rdap=1&whois=1" {
		t.Fatalf("unexpected endpoint: %s", endpoint)
	}
}

func TestWhoisFallbackClientClassifiesResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("domain") {
		case "registered.im":
			_, _ = w.Write([]byte(`{"code":0,"msg":"Query successful","data":{"registered":true,"reserved":false,"unknown":false,"registrar":"Example Registrar","creationDate":"2020-01-02T03:04:05Z","expirationDate":"2030-01-02T03:04:05Z","updatedDate":"2025-01-02T03:04:05Z","nameServers":["ns1.example.test"],"whoisData":"Domain Name: registered.im\nRegistrant Street: Redacted | Registry Policy"}}`))
		case "available.do":
			_, _ = w.Write([]byte(`{"code":0,"msg":"Query successful","data":{"registered":false,"reserved":false,"unknown":false,"whoisData":"No match"}}`))
		case "reserved.do":
			_, _ = w.Write([]byte(`{"code":0,"msg":"Query successful","data":{"registered":false,"reserved":true,"unknown":false,"whoisData":"Domain Cannot Be Registered"}}`))
		default:
			_, _ = w.Write([]byte(`{"code":1,"msg":"timeout","data":null}`))
		}
	}))
	defer server.Close()

	t.Setenv("PUFF_WHOIS_FALLBACK_URL", server.URL+"/api/")
	t.Setenv("PUFF_WHOIS_FALLBACK_TLDS", "im,do")
	client := NewWhoisFallbackClient(5 * time.Second)

	checker := &DomainChecker{whoisClient: NewWhoisClient(5 * time.Second)}
	checker.fallbackClient = client

	registered := checker.tryWhoisFallbackQuery("registered.im", "im")
	if registered.Status != StatusRegistered || registered.QueryMethod != "whois-fallback" || registered.Registrar == "" {
		t.Fatalf("unexpected registered result: %+v", registered)
	}
	if registered.CreatedDate == nil || registered.ExpiryDate == nil {
		t.Fatalf("expected parsed dates: %+v", registered)
	}

	available := checker.tryWhoisFallbackQuery("available.do", "do")
	if available.Status != StatusAvailable {
		t.Fatalf("expected available result, got: %+v", available)
	}

	reserved := checker.tryWhoisFallbackQuery("reserved.do", "do")
	if reserved.Status != StatusUnknown || reserved.WhoisRaw == "" {
		t.Fatalf("reserved result must remain unknown: %+v", reserved)
	}

	missingFlags := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"Query successful","data":{"registered":false,"whoisData":"No match"}}`))
	}))
	defer missingFlags.Close()
	t.Setenv("PUFF_WHOIS_FALLBACK_URL", missingFlags.URL+"/api/")
	missingClient := NewWhoisFallbackClient(5 * time.Second)
	missingChecker := &DomainChecker{whoisClient: NewWhoisClient(5 * time.Second), fallbackClient: missingClient}
	missing := missingChecker.tryWhoisFallbackQuery("missing-flags.im", "im")
	if missing.Status == StatusAvailable {
		t.Fatalf("missing structured flags were incorrectly classified as available: %+v", missing)
	}

	if got := checker.tryWhoisFallbackQuery("example.com", "com"); got != nil {
		t.Fatalf("unexpected fallback for disabled TLD: %+v", got)
	}

}
