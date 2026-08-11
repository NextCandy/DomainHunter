package whois

import (
	"os"
	"testing"

	"DomainHunter/internal/domain"
)

func TestMain(m *testing.M) {
	os.Setenv("DOMAINHUNTER_RDAP_BOOTSTRAP_URL", "http://127.0.0.1:1/rdap.json")
	os.Exit(m.Run())
}

func TestParseDoesNotTreatReservedTextAsAvailable(t *testing.T) {
	response := "Domain Name: example.do\n" +
		"Domain Status: Prohibited String - Domain Cannot Be Registered\n" +
		"Notes: This domain is not allowed under registry policy.\n"

	result := Parse("example.do", response)
	if result.Status == domain.StatusAvailable {
		t.Fatalf("保留域名被错误判定为可注册: %+v", result)
	}
	if result.Status != domain.StatusUnknown {
		t.Fatalf("保留域名应保持未知，实际 %s", result.Status)
	}
}

func TestParseRequiresExplicitAvailabilitySignal(t *testing.T) {
	result := Parse("example.test", "WHOIS server error: temporary response not found in upstream cache")
	if result.Status == domain.StatusAvailable {
		t.Fatalf("含糊的 WHOIS 响应被错误判定为可注册: %+v", result)
	}
}

func TestParseMapsHoldAndTransferLock(t *testing.T) {
	if got := Parse("hold.example", "Domain Status: server hold\n").Status; got != domain.StatusHold {
		t.Fatalf("期望 hold，实际 %s", got)
	}
	result := Parse("locked.example", "Domain Status: clientTransferProhibited\n")
	if result.Status != domain.StatusRegistered {
		t.Fatalf("转移锁定主状态应为 registered，实际 %s", result.Status)
	}
	if len(result.EPPStatuses) != 1 || result.EPPStatuses[0] != "clientTransferProhibited" {
		t.Fatalf("转移锁定应作为 EPP 附加状态保留: %+v", result.EPPStatuses)
	}
}

func TestParseCollectsEPPStatuses(t *testing.T) {
	result := Parse("example.com",
		"Domain Name: example.com\nDomain Status: clientTransferProhibited https://icann.org/epp#clientTransferProhibited\n")
	if len(result.EPPStatuses) == 0 {
		t.Fatal("应当收集到 EPP 状态作为证据")
	}
	if result.EPPStatuses[0] != "clientTransferProhibited" {
		t.Fatalf("EPP 状态解析错误: %q", result.EPPStatuses[0])
	}
}

func TestParseDateTimeSupportsIMExpiryFormat(t *testing.T) {
	parsed := ParseDateTime("30/06/2027 00:59:59")
	if parsed == nil {
		t.Fatal(".im 的到期日格式解析失败")
	}
	if got := parsed.Format("2006-01-02 15:04:05"); got != "2027-06-30 00:59:59" {
		t.Fatalf("到期日解析错误: %s", got)
	}
}

func TestParseDateTimeRejectsUnknownFormat(t *testing.T) {
	if parsed := ParseDateTime("not a date"); parsed != nil {
		t.Fatalf("无法识别的日期必须返回 nil，实际 %v", parsed)
	}
}
