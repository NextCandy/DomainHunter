package query

import (
	"context"
	"os"
	"testing"
	"time"

	"DomainHunter/internal/domain"
)

func TestMain(m *testing.M) {
	os.Setenv("DOMAINHUNTER_RDAP_BOOTSTRAP_URL", "http://127.0.0.1:1/rdap.json")
	os.Exit(m.Run())
}

// fakeProvider 用固定结果替代真实网络查询，便于测试 Policy 的决策逻辑
type fakeProvider struct {
	name      string
	supports  bool
	result    Result
	callCount int
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Supports(context.Context, Request) bool { return f.supports }

func (f *fakeProvider) Query(context.Context, Request) Result {
	f.callCount++
	res := f.result
	res.Provider = f.name
	res.StartedAt = time.Now()
	res.FinishedAt = time.Now()
	return res
}

func provider(name string, status domain.Status) *fakeProvider {
	return &fakeProvider{
		name:     name,
		supports: true,
		result:   Result{Status: status, Confidence: domain.ConfidenceMedium},
	}
}

func failing(name string, kind Kind) *fakeProvider {
	return &fakeProvider{
		name:     name,
		supports: true,
		result: Result{
			Status:     domain.StatusError,
			Confidence: domain.ConfidenceLow,
			Err:        NewError(kind, name, "%s 查询失败", name),
		},
	}
}

func disabled(name string) *fakeProvider {
	return &fakeProvider{name: name, supports: false}
}

func newEngine(cfg Config, providers ...Provider) *Engine {
	return NewEngine(NewRegistry(providers...), NewPolicy(cfg))
}

func TestRDAPRegisteredEndsQueryChain(t *testing.T) {
	rdap := provider(ProviderRDAP, domain.StatusRegistered)
	whois := provider(ProviderWhois, domain.StatusAvailable)

	out := newEngine(Config{}, disabled(ProviderWhoisLS), disabled(ProviderFallback), rdap, whois).
		Query(context.Background(), "example.com")

	if out.Winner.Status != domain.StatusRegistered {
		t.Fatalf("期望 registered，实际 %s", out.Winner.Status)
	}
	if whois.callCount != 0 {
		t.Fatal("已经拿到明确结论后不应再查询 WHOIS")
	}
}

func TestGenericTLDTrustsAvailable(t *testing.T) {
	out := newEngine(Config{},
		disabled(ProviderWhoisLS), disabled(ProviderFallback),
		provider(ProviderRDAP, domain.StatusAvailable),
		provider(ProviderWhois, domain.StatusRegistered),
	).Query(context.Background(), "example.com")

	if out.Winner.Status != domain.StatusAvailable {
		t.Fatalf("没有专用查询源时应直接采信 available，实际 %s", out.Winner.Status)
	}
}

func TestWhoisFallbackAfterRDAPError(t *testing.T) {
	rdap := failing(ProviderRDAP, KindTimeout)
	whois := provider(ProviderWhois, domain.StatusRegistered)

	out := newEngine(Config{}, disabled(ProviderWhoisLS), disabled(ProviderFallback), rdap, whois).
		Query(context.Background(), "example.com")

	if out.Winner.Status != domain.StatusRegistered {
		t.Fatalf("RDAP 失败后应回退到 WHOIS，实际 %s", out.Winner.Status)
	}
	if whois.callCount != 1 {
		t.Fatalf("WHOIS 应被调用一次，实际 %d", whois.callCount)
	}
	if len(out.Evidence) != 2 {
		t.Fatalf("应记录两条证据，实际 %d", len(out.Evidence))
	}
}

func TestOptInProviderErrorWinsOverGenericUnknown(t *testing.T) {
	// 复刻重构前的行为：.im 显式启用了 WHOIS.LS，它报错时必须进入错误重试路径，
	// 不能被 RDAP/WHOIS 的 unknown 吞掉后长期缓存。
	out := newEngine(Config{},
		failing(ProviderWhoisLS, KindTimeout),
		disabled(ProviderFallback),
		provider(ProviderRDAP, domain.StatusAvailable),
		provider(ProviderWhois, domain.StatusUnknown),
	).Query(context.Background(), "example.im")

	if out.Winner.Status != domain.StatusError {
		t.Fatalf("专用查询源报错时必须返回错误，实际 %s", out.Winner.Status)
	}
	if out.Winner.Provider != ProviderWhoisLS {
		t.Fatalf("错误来源应为 whois_ls，实际 %s", out.Winner.Provider)
	}
}

func TestGenericAvailableIsDistrustedWhenOptInConfigured(t *testing.T) {
	out := newEngine(Config{},
		provider(ProviderWhoisLS, domain.StatusUnknown),
		disabled(ProviderFallback),
		provider(ProviderRDAP, domain.StatusAvailable),
		provider(ProviderWhois, domain.StatusUnknown),
	).Query(context.Background(), "example.im")

	if out.Winner.Status == domain.StatusAvailable {
		t.Fatalf("配置了专用查询源时，通用源的 available 不应被单独采信: %+v", out.Winner)
	}
	if out.Winner.Status != domain.StatusUnknown {
		t.Fatalf("期望降级为 unknown，实际 %s", out.Winner.Status)
	}
}

func TestOptInProviderAvailableIsTrusted(t *testing.T) {
	whois := provider(ProviderWhois, domain.StatusRegistered)
	out := newEngine(Config{},
		provider(ProviderWhoisLS, domain.StatusAvailable),
		disabled(ProviderFallback),
		provider(ProviderRDAP, domain.StatusUnknown),
		whois,
	).Query(context.Background(), "example.im")

	if out.Winner.Status != domain.StatusAvailable {
		t.Fatalf("专用查询源的 available 应被采信，实际 %s", out.Winner.Status)
	}
	if whois.callCount != 0 {
		t.Fatal("拿到结论后不应继续查询")
	}
}

func TestConfirmModeRequiresTwoVotes(t *testing.T) {
	cfg := Config{TLDs: map[string]TLDPolicy{
		"com": {
			Providers:         []string{ProviderRDAP, ProviderWhois},
			ValidateAvailable: AvailableSetting{Set: true, Mode: AvailableConfirm},
		},
	}}

	// 只有 RDAP 说可注册 → 不足以确认
	single := newEngine(cfg,
		provider(ProviderRDAP, domain.StatusAvailable),
		provider(ProviderWhois, domain.StatusUnknown),
	).Query(context.Background(), "example.com")
	if single.Winner.Status == domain.StatusAvailable {
		t.Fatalf("confirm 模式下单一来源不足以判定可注册: %+v", single.Winner)
	}

	// 两个来源都说可注册 → 采信
	double := newEngine(cfg,
		provider(ProviderRDAP, domain.StatusAvailable),
		provider(ProviderWhois, domain.StatusAvailable),
	).Query(context.Background(), "example.com")
	if double.Winner.Status != domain.StatusAvailable {
		t.Fatalf("两个来源互相印证时应判定可注册，实际 %s", double.Winner.Status)
	}
	if double.Winner.Confidence != domain.ConfidenceHigh {
		t.Fatalf("交叉验证后的可信度应为 high，实际 %s", double.Winner.Confidence)
	}
}

func TestReservedUnknownStaysUnknown(t *testing.T) {
	out := newEngine(Config{},
		disabled(ProviderWhoisLS), disabled(ProviderFallback),
		provider(ProviderRDAP, domain.StatusUnknown),
		provider(ProviderWhois, domain.StatusUnknown),
	).Query(context.Background(), "example.com")

	if out.Winner.Status != domain.StatusUnknown {
		t.Fatalf("期望 unknown，实际 %s", out.Winner.Status)
	}
}

func TestAllSkippedBecomesSkipped(t *testing.T) {
	out := newEngine(Config{},
		disabled(ProviderWhoisLS), disabled(ProviderFallback),
		provider(ProviderRDAP, domain.StatusSkipped),
		provider(ProviderWhois, domain.StatusSkipped),
	).Query(context.Background(), "example.com")

	if out.Winner.Status != domain.StatusSkipped {
		t.Fatalf("没有可用查询源时应为 skipped，实际 %s", out.Winner.Status)
	}
}

func TestNoProviderAvailableIsSkipped(t *testing.T) {
	out := newEngine(Config{},
		disabled(ProviderWhoisLS), disabled(ProviderFallback),
		disabled(ProviderRDAP), disabled(ProviderWhois),
	).Query(context.Background(), "example.com")

	if out.Winner.Status != domain.StatusSkipped {
		t.Fatalf("没有任何 Provider 时应为 skipped，实际 %s", out.Winner.Status)
	}
}

func TestUnmatchedTLDIsSkipped(t *testing.T) {
	out := newEngine(Config{}, provider(ProviderRDAP, domain.StatusAvailable)).
		Query(context.Background(), "example.thistlddoesnotexist")

	if out.Winner.Status != domain.StatusSkipped {
		t.Fatalf("没有匹配的查询配置时应为 skipped，实际 %s", out.Winner.Status)
	}
}

func TestFallbackTimeoutBecomesError(t *testing.T) {
	out := newEngine(Config{},
		disabled(ProviderWhoisLS),
		failing(ProviderFallback, KindTimeout),
		failing(ProviderRDAP, KindTimeout),
		failing(ProviderWhois, KindTimeout),
	).Query(context.Background(), "example.do")

	if out.Winner.Status != domain.StatusError {
		t.Fatalf("全部超时时必须返回错误而不是可注册，实际 %s", out.Winner.Status)
	}
	if !Retryable(out.Winner.Err) {
		t.Fatal("超时错误应当可重试")
	}
}

func TestProviderCanBeDisabledByConfig(t *testing.T) {
	rdap := provider(ProviderRDAP, domain.StatusAvailable)
	enabled := false
	cfg := Config{Providers: map[string]ProviderSetting{ProviderRDAP: {Enabled: &enabled}}}

	out := newEngine(cfg,
		disabled(ProviderWhoisLS), disabled(ProviderFallback),
		rdap, provider(ProviderWhois, domain.StatusRegistered),
	).Query(context.Background(), "example.com")

	if rdap.callCount != 0 {
		t.Fatal("被禁用的 Provider 不应被调用")
	}
	if out.Winner.Status != domain.StatusRegistered {
		t.Fatalf("期望 registered，实际 %s", out.Winner.Status)
	}
}

func TestPlanRespectsExplicitTLDOrder(t *testing.T) {
	cfg := Config{TLDs: map[string]TLDPolicy{
		"do": {Providers: []string{ProviderFallback, ProviderRDAP, ProviderWhois}},
	}}
	registryOf := NewRegistry(
		disabled(ProviderWhoisLS),
		provider(ProviderFallback, domain.StatusRegistered),
		provider(ProviderRDAP, domain.StatusRegistered),
		provider(ProviderWhois, domain.StatusRegistered),
	)

	steps := NewPolicy(cfg).Plan(context.Background(), Request{Domain: "a.do", TLD: "do"}, registryOf)
	if len(steps) != 3 {
		t.Fatalf("计划步骤数量不对: %d", len(steps))
	}
	want := []string{ProviderFallback, ProviderRDAP, ProviderWhois}
	for i, step := range steps {
		if step.Provider != want[i] {
			t.Fatalf("第 %d 步应为 %s，实际 %s", i, want[i], step.Provider)
		}
	}
}

func TestLoadConfigAcceptsBooleanAndString(t *testing.T) {
	cfg, err := LoadConfig(`{"tlds":{"im":{"providers":["whois_ls","rdap"],"validate_available":true}}}`)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if cfg.TLDs["im"].ValidateAvailable.Mode != AvailableDistrust {
		t.Fatalf("true 应映射为 distrust，实际 %s", cfg.TLDs["im"].ValidateAvailable.Mode)
	}

	cfg, err = LoadConfig(`{"tlds":{"do":{"validate_available":"confirm"}}}`)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if cfg.TLDs["do"].ValidateAvailable.Mode != AvailableConfirm {
		t.Fatalf("confirm 解析错误: %s", cfg.TLDs["do"].ValidateAvailable.Mode)
	}

	if _, err := LoadConfig(`{"tlds":{"do":{"validate_available":"nonsense"}}}`); err == nil {
		t.Fatal("非法取值应当报错")
	}
}
