package core

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWhoisLSEndpointUsesJSONPath(t *testing.T) {
	endpoint, err := whoisLSEndpoint("https://whois.ls/json/", "example.im")
	if err != nil {
		t.Fatalf("whoisLSEndpoint returned error: %v", err)
	}
	if endpoint != "https://whois.ls/json/example.im" {
		t.Fatalf("unexpected endpoint: %s", endpoint)
	}
}

func TestWhoisLSParsesIMExpiryWithoutCreationDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":false,"data":"Domain Name:\tregistered.im\nDomain Details\nExpiry Date: 30/06/2027 00:59:59\nName Server: ns1.example.test."}`))
	}))
	defer server.Close()

	t.Setenv("DOMAINHUNTER_WHOIS_LS_URL", server.URL+"/json/")
	t.Setenv("DOMAINHUNTER_WHOIS_LS_TLDS", "im")
	client := NewWhoisLSClient(5 * time.Second)
	checker := &DomainChecker{
		whoisClient:   NewWhoisClient(5 * time.Second),
		whoisLSClient: client,
	}

	info := checker.tryWhoisLSQuery("registered.im", "im")
	if info == nil || info.Status != StatusRegistered {
		t.Fatalf("unexpected WHOIS.LS status: %+v", info)
	}
	if info.CreatedDate != nil {
		t.Fatalf(".im creation date should remain unavailable: %v", info.CreatedDate)
	}
	if info.ExpiryDate == nil || info.ExpiryDate.Format("2006-01-02 15:04:05") != "2027-06-30 00:59:59" {
		t.Fatalf("unexpected .im expiry date: %+v", info.ExpiryDate)
	}
	if info.QueryMethod != "whois-ls" || info.WhoisRaw == "" {
		t.Fatalf("WHOIS.LS provenance was not preserved: %+v", info)
	}
}

func TestWhoisLSExplicitNotFoundIsAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":false,"data":"Domain Name:\tmissing.im\nThe domain missing.im was not found."}`))
	}))
	defer server.Close()

	t.Setenv("DOMAINHUNTER_WHOIS_LS_URL", server.URL+"/json/")
	t.Setenv("DOMAINHUNTER_WHOIS_LS_TLDS", "im")
	checker := &DomainChecker{
		whoisClient:   NewWhoisClient(5 * time.Second),
		whoisLSClient: NewWhoisLSClient(5 * time.Second),
	}

	info := checker.tryWhoisLSQuery("missing.im", "im")
	if info == nil || info.Status != StatusAvailable {
		t.Fatalf("explicit WHOIS.LS not-found response was not classified as available: %+v", info)
	}
}
