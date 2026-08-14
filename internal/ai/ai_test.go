package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"DomainHunter/internal/domain"
)

func validOutput() Output {
	return Output{
		SchemaVersion: PromptVersion, Summary: "简短、易读的研究性摘要", Score: 72, LiquidityScore: 54,
		RiskLevel: "medium", Confidence: "medium", IndicativeValueUSD: &ValueRange{Low: 100, High: 500, Currency: "USD"},
		PriceEvaluationCNY: &ValueRange{Low: 1800, High: 6800, Currency: "CNY"},
		CoreAnalysis:       "前缀短且易读，后缀适合品牌与工具类用途；缺少实时可比成交数据，价格区间按研究性用途保守给出。",
		Strengths:          []string{"短字符"}, Risks: []string{"缺少可比成交数据"}, DataGaps: []string{"无实时市场数据"}, EvidenceUsed: []string{"domain", "status"},
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
	bad = validOutput()
	bad.PriceEvaluationCNY.Currency = "USD"
	if err := ValidateOutput(bad); err == nil {
		t.Fatal("price evaluation must use CNY")
	}
	bad = validOutput()
	bad.CoreAnalysis = ""
	if err := ValidateOutput(bad); err == nil {
		t.Fatal("core analysis must be present")
	}
}

func TestSystemPromptContainsRequestedReportRules(t *testing.T) {
	prompt := SystemPrompt()
	for _, required := range []string{"域名鉴定师", `{"low":整数,"high":整数,"currency":"CNY"}`, "core_analysis", "谐音", "完整的词"} {
		if !contains(prompt, required) {
			t.Fatalf("system prompt missing %q", required)
		}
	}
}

func TestEvaluateParsesChineseReportContract(t *testing.T) {
	const content = `{"schema_version":"domainhunter.ai-valuation.v2-report","summary":"短、易记，适合品牌用途","score":88,"liquidity_score":71,"risk_level":"medium","confidence":"medium","indicative_value_usd":null,"price_evaluation_cny":{"low":1800,"high":6800,"currency":"CNY"},"core_analysis":"前缀 daydream 语义完整，.im 可作为个人品牌与创意项目后缀；记忆点明确，但买方范围较窄，溢价取决于用途匹配。","strengths":["语义完整"],"risks":["后缀买方范围较窄"],"data_gaps":["缺少实时可比成交数据"],"evidence_used":["domain","tld","lexical"],"status_guard":"AI 不改变系统查询结论","disclaimer":"仅供研究性排序与解释，不构成估值、投资、购买或法律建议。"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Fatalf("unexpected AI request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		var payload struct {
			Thinking map[string]string `json:"thinking"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode AI request: %v", err)
		}
		if payload.Thinking["type"] != "disabled" {
			t.Fatalf("DeepSeek V4 must explicitly disable thinking, got %#v", payload.Thinking)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"test-model","choices":[{"message":{"content":` + mustJSONString(content) + `}}]}`))
	}))
	defer server.Close()

	client := DeepSeekCompatibleClient{Policy: BaseURLPolicy{AllowInsecureLocal: true}}
	output, _, err := client.Evaluate(context.Background(), Profile{BaseURL: server.URL, Model: "deepseek-v4-flash", ThinkingType: ThinkingDisabled, TimeoutSeconds: 5, MaxTokens: 600}, "test-key", SanitizedInput{Domain: "daydream.im", TLD: ".im"})
	if err != nil {
		t.Fatalf("evaluate failed: %v", err)
	}
	if output.Score != 88 || output.PriceEvaluationCNY == nil || output.PriceEvaluationCNY.Currency != "CNY" || output.CoreAnalysis == "" {
		t.Fatalf("report fields not parsed: %+v", output)
	}
}

func TestEvaluateParsesCompatibleContentPartsAndCodeFence(t *testing.T) {
	const content = `{"schema_version":"domainhunter.ai-valuation.v2-report","summary":"短、易记，适合品牌用途","score":88,"liquidity_score":71,"risk_level":"medium","confidence":"medium","indicative_value_usd":null,"price_evaluation_cny":{"low":1800,"high":6800,"currency":"CNY"},"core_analysis":"前缀 daydream 语义完整，.im 可作为个人品牌与创意项目后缀；记忆点明确，但买方范围较窄，溢价取决于用途匹配。","strengths":["语义完整"],"risks":["后缀买方范围较窄"],"data_gaps":["缺少实时可比成交数据"],"evidence_used":["domain","tld","lexical"],"status_guard":"AI 不改变系统查询结论","disclaimer":"仅供研究性排序与解释，不构成估值、投资、购买或法律建议。"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fenced := "```json\n" + content + "\n```"
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":[{"type":"text","text":` + mustJSONString(fenced) + `}]}}]}`))
	}))
	defer server.Close()

	client := DeepSeekCompatibleClient{Policy: BaseURLPolicy{AllowInsecureLocal: true}}
	output, _, err := client.Evaluate(context.Background(), Profile{BaseURL: server.URL, Model: "test-model", TimeoutSeconds: 5, MaxTokens: 600}, "test-key", SanitizedInput{Domain: "daydream.im", TLD: ".im"})
	if err != nil {
		t.Fatalf("content parts should parse: %v", err)
	}
	if output.Score != 88 || output.PriceEvaluationCNY == nil || output.CoreAnalysis == "" {
		t.Fatalf("unexpected parsed report: %+v", output)
	}
}

func TestEvaluateMapsUnauthorizedToActionableError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := DeepSeekCompatibleClient{Policy: BaseURLPolicy{AllowInsecureLocal: true}}
	_, _, err := client.Evaluate(context.Background(), Profile{BaseURL: server.URL, Model: "test-model", TimeoutSeconds: 5, MaxTokens: 600}, "test-key", SanitizedInput{Domain: "daydream.im", TLD: ".im"})
	if err == nil || !strings.Contains(err.Error(), "API Key 无效或已过期") {
		t.Fatalf("expected actionable unauthorized error, got %v", err)
	}
	if !errors.Is(err, ErrProviderAuth) {
		t.Fatalf("expected provider auth sentinel, got %v", err)
	}
}

func TestEvaluateIncludesSafeProviderRateLimitType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"type":"FreeUsageLimitError","message":"private upstream details"}}`))
	}))
	defer server.Close()

	client := DeepSeekCompatibleClient{Policy: BaseURLPolicy{AllowInsecureLocal: true}}
	_, _, err := client.Evaluate(context.Background(), Profile{BaseURL: server.URL, Model: "test-model", TimeoutSeconds: 5, MaxTokens: 600}, "test-key", SanitizedInput{Domain: "daydream.im", TLD: ".im"})
	if err == nil || !strings.Contains(err.Error(), "FreeUsageLimitError") || strings.Contains(err.Error(), "private upstream details") {
		t.Fatalf("expected safe rate limit classification, got %v", err)
	}
}

func TestDeepSeekV4ReasoningEffortUsesSupportedValues(t *testing.T) {
	if got := deepSeekReasoningEffort(ReasoningLow); got != "high" {
		t.Fatalf("DeepSeek V4 low effort must map to high, got %q", got)
	}
	if got := deepSeekReasoningEffort(ReasoningMax); got != "max" {
		t.Fatalf("DeepSeek V4 max effort must remain max, got %q", got)
	}
}

func TestNormalizeEvidenceUsedDropsModelFreeText(t *testing.T) {
	got := normalizeEvidenceUsed([]string{"domain", "WHOIS.LS 实时结果", "tld", "domain", "market data"})
	if len(got) != 2 || got[0] != "domain" || got[1] != "tld" {
		t.Fatalf("unexpected normalized evidence: %#v", got)
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

func TestValuationInfoAllowsUnqueriedDomain(t *testing.T) {
	info := valuationInfo("brand-new.example", nil, time.Now().UTC())
	if info.Name != "brand-new.example" || info.Status != domain.StatusUnknown {
		t.Fatalf("unqueried domain should retain its name and unknown fact state: %#v", info)
	}
	if info.Review == nil || !info.Review.Required {
		t.Fatalf("missing query evidence should be recorded as a limitation, not dropped: %#v", info.Review)
	}

	input := SanitizeInput(domain.Domain{Name: info.Name}, *info)
	if input.Domain != "brand-new.example" || input.SystemFacts.ProviderConsensus != "no_result" {
		t.Fatalf("unqueried domain was not represented as a name-only valuation input: %#v", input)
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
	if err := validateProfileInput(input); err != nil {
		t.Fatalf("zero daily limit should be accepted as unlimited compatibility value: %v", err)
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

func mustJSONString(value string) string {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(raw)
}
