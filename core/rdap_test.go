package core

import (
	"testing"
	"time"
)

func TestParseRDAPResponseTreatsReserved404AsUnknown(t *testing.T) {
	client := NewRDAPClient(time.Second)
	result := client.ParseRDAPResponse("example.do", &RDAPResponse{
		ErrorCode: 404,
		Title:     "Domain Cannot Be Registered",
	}, `{"errorCode":404,"title":"Domain Cannot Be Registered"}`)

	if result.Status != StatusUnknown {
		t.Fatalf("expected reserved 404 to remain unknown, got %s", result.Status)
	}
}

func TestParseRDAPResponseTreatsAuthoritative404AsAvailable(t *testing.T) {
	client := NewRDAPClient(time.Second)
	result := client.ParseRDAPResponse("new-domain.example", &RDAPResponse{
		ErrorCode:   404,
		Title:       "Not Found",
		Description: []string{"Domain not found"},
	}, `{"errorCode":404,"title":"Not Found"}`)

	if result.Status != StatusAvailable {
		t.Fatalf("expected ordinary 404 to be available, got %s", result.Status)
	}
}

func TestParseRDAPResponseDoesNotInferAvailabilityFromEmptyObject(t *testing.T) {
	client := NewRDAPClient(time.Second)
	result := client.ParseRDAPResponse("ambiguous.example", &RDAPResponse{
		ObjectClassName: "domain",
	}, `{"objectClassName":"domain"}`)

	if result.Status == StatusAvailable {
		t.Fatalf("empty RDAP object was incorrectly classified as available: %+v", result)
	}
}

func TestParseRDAPResponseRecognizesConfiguredReservedPhrase(t *testing.T) {
	client := NewRDAPClient(time.Second)
	result := client.ParseRDAPResponse("example.do", &RDAPResponse{
		ErrorCode:   404,
		Title:       "Registration is prohibited",
		Description: []string{"Registry policy"},
	}, `{"errorCode":404,"title":"Registration is prohibited"}`)

	if result.Status == StatusAvailable {
		t.Fatalf("configured reserved phrase was incorrectly classified as available: %+v", result)
	}
}

func TestParseRDAPStatusMapsHoldAndTransferLock(t *testing.T) {
	client := NewRDAPClient(time.Second)
	if got := client.parseRDAPStatus([]string{"client hold"}); got != StatusHold {
		t.Fatalf("expected hold status, got %s", got)
	}
	if got := client.parseRDAPStatus([]string{"client transfer prohibited"}); got != StatusTransferLocked {
		t.Fatalf("expected transfer lock status, got %s", got)
	}
}
