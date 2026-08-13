package p1

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/storage/sqlite"
)

func TestParseFilterAndEvaluate(t *testing.T) {
	node, err := ParseFilter(`{"version":1,"logic":"and","conditions":[{"field":"status","op":"in","value":["available","registered"]},{"field":"ai_quality","op":"gte","value":70}]}`)
	if err != nil {
		t.Fatalf("ParseFilter() error = %v", err)
	}
	now := time.Now()
	item := richDomain{Info: domain.Info{Name: "daydream.im", Status: domain.StatusAvailable, LastChecked: now}, AI: &Valuation{QualityScore: 82}}
	if !filterMatches(node, item) {
		t.Fatal("expected filter to match")
	}
	item.AI.QualityScore = 60
	if filterMatches(node, item) {
		t.Fatal("expected quality condition to reject item")
	}
	if _, err := ParseFilter(`{"version":1,"conditions":[{"field":"whois_raw","op":"contains","value":"secret"}]}`); err == nil {
		t.Fatal("unknown/private field should be rejected")
	}
}

func TestAIBaseURLRequiresExplicitLocalHTTP(t *testing.T) {
	if err := validateAIBaseURL("http://127.0.0.1:18080"); err == nil {
		t.Fatal("local HTTP should be rejected by default")
	}
	t.Setenv("DOMAINHUNTER_AI_ALLOW_INSECURE_LOCAL", "true")
	if err := validateAIBaseURL("http://127.0.0.1:18080"); err != nil {
		t.Fatalf("explicit local HTTP should pass: %v", err)
	}
	t.Setenv("DOMAINHUNTER_AI_ALLOWED_HOSTS", "example.com")
	if err := validateAIBaseURL("https://api.deepseek.com"); err == nil {
		t.Fatal("host outside allowlist should be rejected")
	}
}

func TestValuationSchemaRejectsUnsafeResponses(t *testing.T) {
	valid := `{"analysis_version":"p1-valuation-v1","quality_score":80,"liquidity_score":60,"risk_level":"medium","value_low":100,"value_high":500,"confidence":"low","strengths":["short"],"limitations":["none"],"data_gaps":["comparables"],"rationale":"research only","disclaimer":"not a transaction guarantee"}`
	if _, err := validateValuation(valid); err != nil {
		t.Fatalf("valid valuation rejected: %v", err)
	}
	if _, err := validateValuation(`<html>not json</html>`); err == nil {
		t.Fatal("HTML response should be rejected")
	}
	if _, err := validateValuation(strings.Replace(valid, `"value_high":500`, `"value_high":-1`, 1)); err == nil {
		t.Fatal("invalid value range should be rejected")
	}
	if _, err := validateValuation(strings.Replace(valid, `"rationale":"research only"`, `"rationale":"research only","api_key":"leak"`, 1)); err == nil {
		t.Fatal("unknown secret field should be rejected")
	}
}

func TestEncryptedAPIKeyAndDryRunHaveNoDomainSideEffect(t *testing.T) {
	t.Setenv("DOMAINHUNTER_SECRET_KEY", "unit-test-secret")
	t.Setenv("DOMAINHUNTER_AI_ALLOW_INSECURE_LOCAL", "true")
	db, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	service := New(db)
	settings, err := service.SaveAISettings(context.Background(), AISettingsInput{Provider: "deepseek", BaseURL: "http://127.0.0.1:18080", Model: "test-model", APIKey: "sk-secret-value", TimeoutSeconds: 5, Concurrency: 1, MaxOutputTokens: 128, DailyLimit: 10, CacheTTLSeconds: 300, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if !settings.APIKeySet || settings.KeySource != "encrypted" {
		t.Fatalf("unexpected key metadata: %+v", settings)
	}
	if settings.DailyLimit != 0 {
		t.Fatalf("AI daily limit must be disabled, got %d", settings.DailyLimit)
	}
	var encrypted string
	if err := db.QueryRow(`SELECT encrypted_api_key FROM ai_provider_settings WHERE id=1`).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if encrypted == "" || strings.Contains(encrypted, "sk-secret-value") {
		t.Fatalf("API key was not encrypted: %q", encrypted)
	}

	if _, err := db.Exec(`INSERT INTO domains(name,enabled,notify) VALUES('example.com',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO domain_results(domain,status,registrar,whois_raw) VALUES('example.com','available','', 'private whois must not be sent')`); err != nil {
		t.Fatal(err)
	}
	rule, err := service.CreateAutomationRule(context.Background(), AutomationRule{Name: "dry", DryRun: true, Trigger: FilterNode{}, Conditions: FilterNode{Version: 1, Logic: "and", Conditions: []FilterNode{{Field: "status", Op: "eq", Value: "available"}}}, Actions: []map[string]any{{"type": "tag", "tag": "candidate"}}, CooldownSeconds: 60, DailyRunCap: 10})
	if err != nil {
		t.Fatal(err)
	}
	runs, err := service.DryRunAutomation(context.Background(), rule.ID, AutomationEvent{ID: "evt-1", Type: "status_changed"})
	if err != nil || len(runs) != 1 {
		t.Fatalf("DryRunAutomation() runs=%d err=%v", len(runs), err)
	}
	var tags string
	if err := db.QueryRow(`SELECT COALESCE(tags,'') FROM domains WHERE name='example.com'`).Scan(&tags); err != nil {
		t.Fatal(err)
	}
	if tags != "" {
		t.Fatalf("dry-run changed domain tags: %q", tags)
	}
	input, fingerprint, err := service.ai.inputForDomain(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint == "" {
		t.Fatal("missing input fingerprint")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(input), &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["whois_raw"]; ok {
		t.Fatal("raw WHOIS must not be included in AI input")
	}
}

func TestAIProviderProfilesCanSwitchDefaultWithoutLeakingKeys(t *testing.T) {
	t.Setenv("DOMAINHUNTER_SECRET_KEY", "profile-test-secret")
	t.Setenv("DOMAINHUNTER_AI_ALLOW_INSECURE_LOCAL", "true")
	db, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	service := New(db)
	base := AISettingsInput{Provider: "openai_compatible", Name: "OpenAI Compatible", BaseURL: "http://127.0.0.1:18080", Model: "deepseek-v4-flash-free", TimeoutSeconds: 5, Concurrency: 1, MaxOutputTokens: 128, DailyLimit: 10, CacheTTLSeconds: 300, Enabled: true, IsDefault: true}
	if _, err := service.SaveAISettings(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	profile, err := service.CreateAIProviderProfile(context.Background(), AISettingsInput{Provider: "deepseek", Name: "备用 DeepSeek", BaseURL: "http://127.0.0.1:18080", Model: "deepseek-test", TimeoutSeconds: 5, Concurrency: 1, MaxOutputTokens: 128, DailyLimit: 10, CacheTTLSeconds: 300, APIKey: "secondary-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if profile.IsDefault || !profile.APIKeySet || profile.KeySource != "encrypted" {
		t.Fatalf("unexpected secondary profile metadata: %+v", profile)
	}
	var encrypted string
	if err := db.QueryRow(`SELECT encrypted_api_key FROM ai_provider_profiles WHERE id=?`, profile.ProfileID).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if encrypted == "" || strings.Contains(encrypted, "secondary-secret") {
		t.Fatalf("secondary API key was not encrypted: %q", encrypted)
	}

	base.ProfileID = profile.ProfileID
	base.Name = "备用 DeepSeek"
	base.Provider = "deepseek"
	base.Model = "deepseek-test"
	base.IsDefault = true
	base.APIKey = ""
	if _, err := service.SaveAISettings(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	settings, err := service.AISettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if settings.ProfileID != profile.ProfileID || settings.Provider != "deepseek" || settings.ProfileName != "备用 DeepSeek" {
		t.Fatalf("default profile was not switched: %+v", settings)
	}
	profiles, err := service.ai.ListProviderProfiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defaults := 0
	for _, item := range profiles {
		if item.IsDefault {
			defaults++
		}
	}
	if len(profiles) != 2 || defaults != 1 {
		t.Fatalf("unexpected profiles: %+v", profiles)
	}
}

func TestAIEnqueueDoesNotApplyDailyLimit(t *testing.T) {
	t.Setenv("DOMAINHUNTER_AI_API_KEY", "test-key")
	t.Setenv("DOMAINHUNTER_AI_ALLOW_INSECURE_LOCAL", "true")
	db, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	service := New(db)
	if _, err := service.SaveAISettings(context.Background(), AISettingsInput{
		Provider: "deepseek", BaseURL: "http://127.0.0.1:18080", Model: "test-model",
		TimeoutSeconds: 5, Concurrency: 1, MaxOutputTokens: 128, DailyLimit: 1,
		CacheTTLSeconds: 300, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 51)
	for i := range names {
		name := fmt.Sprintf("unlimited-%02d.example", i)
		names[i] = name
		if _, err := db.Exec(`INSERT INTO domains(name,enabled,notify) VALUES(?,?,?)`, name, 1, 1); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO domain_results(domain,status) VALUES(?,?)`, name, "available"); err != nil {
			t.Fatal(err)
		}
	}
	queued, err := service.EnqueueAI(context.Background(), names)
	if err != nil {
		t.Fatal(err)
	}
	if queued != len(names) {
		t.Fatalf("daily AI limit must not defer tasks: queued=%d want=%d", queued, len(names))
	}
}

func TestFilterValueHelpers(t *testing.T) {
	if !compareValue([]string{"High Value", "im"}, "contains", "high") {
		t.Fatal("tag contains comparison failed")
	}
	if !compareValue(80, "gte", 70.0) {
		t.Fatal("numeric comparison failed")
	}
	if !isEmptyValue(nil) {
		t.Fatal("nil should be empty")
	}
	if isEmptyValue("value") {
		t.Fatal("non-empty string classified as empty")
	}
}

func TestBulkExecuteWritesAudit(t *testing.T) {
	db, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO domains(name,enabled,notify) VALUES('audit.example',1,1)`); err != nil {
		t.Fatal(err)
	}
	service := New(db)
	result, err := service.ExecuteBulk(context.Background(), BulkAction{Type: "tag", Tag: "review", Domains: []string{"audit.example"}})
	if err != nil {
		t.Fatal(err)
	}
	if result["updated"] != 1 {
		t.Fatalf("unexpected bulk result: %#v", result)
	}
	var tags string
	if err := db.QueryRow(`SELECT COALESCE(tags,'') FROM domains WHERE name='audit.example'`).Scan(&tags); err != nil {
		t.Fatal(err)
	}
	if tags != "review" {
		t.Fatalf("bulk tag was not applied: %q", tags)
	}
	audits, err := service.ListBulkAudits(context.Background(), 10)
	if err != nil || len(audits) != 1 {
		t.Fatalf("ListBulkAudits() len=%d err=%v", len(audits), err)
	}
	if audits[0].ActionType != "tag" || audits[0].Matched != 1 || audits[0].TaskCount != 1 {
		t.Fatalf("unexpected bulk audit: %+v", audits[0])
	}
}

func TestAutomationEventBridgeProcessesNewDomain(t *testing.T) {
	db, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	service := New(db)
	rule, err := service.CreateAutomationRule(context.Background(), AutomationRule{
		Name:    "new-domain-tag",
		Enabled: true,
		Trigger: FilterNode{Field: "event_type", Op: "eq", Value: "domain_added"},
		Conditions: FilterNode{Version: 1, Logic: "and", Conditions: []FilterNode{{
			Field: "status", Op: "eq", Value: "available",
		}}},
		Actions:         []map[string]any{{"type": "tag", "tag": "new"}},
		CooldownSeconds: 0,
		DailyRunCap:     10,
		DryRun:          false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.initializeAutomationCursors(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO domains(name,enabled,notify) VALUES('new.example',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO domain_results(domain,status) VALUES('new.example','available')`); err != nil {
		t.Fatal(err)
	}
	if err := service.scanAutomationEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	var tags string
	if err := db.QueryRow(`SELECT COALESCE(tags,'') FROM domains WHERE name='new.example'`).Scan(&tags); err != nil {
		t.Fatal(err)
	}
	if tags != "new" {
		t.Fatalf("event bridge did not apply tag: %q", tags)
	}
	var runs int
	if err := db.QueryRow(`SELECT COUNT(*) FROM automation_runs WHERE rule_id=? AND event_id='domain:1' AND status='succeeded'`, rule.ID).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if runs != 1 {
		t.Fatalf("expected one succeeded automation run, got %d", runs)
	}
	if err := service.scanAutomationEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM automation_runs WHERE rule_id=? AND event_id='domain:1'`, rule.ID).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if runs != 1 {
		t.Fatalf("event bridge was not idempotent, got %d runs", runs)
	}
}
