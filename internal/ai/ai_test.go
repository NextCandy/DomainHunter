package ai

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"DomainHunter/internal/domain"
)

func validOutput() Output {
	return Output{
		SchemaVersion: PromptVersion, Summary: "简短、易读的研究性摘要", QualityScore: 72, LiquidityScore: 54,
		RiskLevel: "medium", Confidence: "medium", IndicativeValueUSD: &ValueRange{Low: 100, High: 500, Currency: "USD"},
		Strengths: []string{"短字符"}, Risks: []string{"缺少可比成交数据"}, DataGaps: []string{"无实时市场数据"}, EvidenceUsed: []string{"domain", "status"},
		StatusGuard: "AI 不改变系统查询结论", Disclaimer: "仅供研究性排序与解释，不构成估值、投资、购买或法律建议。",
	}
}

func TestValidateOutput(t *testing.T) {
	if err := ValidateOutput(validOutput()); err != nil {
		t.Fatalf("expected valid output: %v", err)
	}
	bad := validOutput()
	bad.QualityScore = 101
	if err := ValidateOutput(bad); err == nil {
		t.Fatal("score > 100 must fail")
	}
	bad = validOutput()
	bad.IndicativeValueUSD.High = 99
	if err := ValidateOutput(bad); err == nil {
		t.Fatal("invalid value range must fail")
	}
	bad = validOutput()
	bad.SchemaVersion = "unknown"
	if err := ValidateOutput(bad); err == nil {
		t.Fatal("unknown schema must fail")
	}
}

func TestSanitizeInputExcludesSensitiveFields(t *testing.T) {
	expiry := time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC)
	watched := domain.Domain{Name: "alpha-test.com", Tags: []string{"高价值", "x"}, Priority: 100, Note: "必须绝不发送的私人备注"}
	info := domain.Info{Name: "alpha-test.com", Status: domain.Status("registered"), Confidence: domain.Confidence("high"), Registrar: "NameSilo", ExpiryDate: &expiry, EPPStatuses: []string{"clientTransferProhibited"}, WhoisRaw: "Registrant Email: private@example.com", Note: "个人信息"}
	input := SanitizeInput(watched, info)
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{"private@example.com", "私人备注", "个人信息", "whois_raw", "note"} {
		if contains(text, forbidden) {
			t.Fatalf("sanitized input leaked %q: %s", forbidden, text)
		}
	}
	if input.Domain != "alpha-test.com" || input.SystemFacts.Registrar != "NameSilo" {
		t.Fatal("required safe facts missing")
	}
}

func TestForbiddenIP(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "::1"} {
		if !forbiddenIP(net.ParseIP(raw)) {
			t.Fatalf("%s must be rejected", raw)
		}
	}
	if forbiddenIP(net.ParseIP("1.1.1.1")) {
		t.Fatal("public IP must not be rejected")
	}
}

func TestValidateProfileInput(t *testing.T) {
	input := DefaultDeepSeekProfile()
	if err := validateProfileInput(input); err != nil {
		t.Fatalf("default profile invalid: %v", err)
	}
	input.DailyLimit = 0
	if err := validateProfileInput(input); err == nil {
		t.Fatal("zero daily limit must fail")
	}
}

func contains(text, value string) bool {
	for i := 0; i+len(value) <= len(text); i++ {
		if text[i:i+len(value)] == value {
			return true
		}
	}
	return false
}
