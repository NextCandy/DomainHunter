package core

import (
	"testing"
	"time"
)

func TestParseWhoisResponseDoesNotTreatReservedTextAsAvailable(t *testing.T) {
	client := NewWhoisClient(5 * time.Second)
	response := "Domain Name: example.do\n" +
		"Domain Status: Prohibited String - Domain Cannot Be Registered\n" +
		"Notes: This domain is not allowed under registry policy.\n"

	result := client.ParseWhoisResponse("example.do", response)
	if result.Status == StatusAvailable {
		t.Fatalf("reserved domain was incorrectly classified as available: %+v", result)
	}
	if result.Status != StatusUnknown {
		t.Fatalf("expected reserved domain to remain unknown, got %s", result.Status)
	}
}

func TestParseWhoisResponseRequiresExplicitAvailabilitySignal(t *testing.T) {
	client := NewWhoisClient(5 * time.Second)
	response := "WHOIS server error: temporary response not found in upstream cache"

	result := client.ParseWhoisResponse("example.test", response)
	if result.Status == StatusAvailable {
		t.Fatalf("ambiguous WHOIS response was incorrectly classified as available: %+v", result)
	}
}

func TestParseWhoisResponseMapsHoldAndTransferLock(t *testing.T) {
	client := NewWhoisClient(5 * time.Second)
	if got := client.ParseWhoisResponse("hold.example", "Domain Status: server hold\n").Status; got != StatusHold {
		t.Fatalf("expected hold status, got %s", got)
	}
	if got := client.ParseWhoisResponse("locked.example", "Domain Status: clientTransferProhibited\n").Status; got != StatusTransferLocked {
		t.Fatalf("expected transfer lock status, got %s", got)
	}
}
