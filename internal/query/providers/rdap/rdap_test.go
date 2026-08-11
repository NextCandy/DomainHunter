package rdap

import (
	"os"
	"testing"

	"DomainHunter/internal/domain"
)

func TestMain(m *testing.M) {
	// 测试不应依赖公网：指向一个必然失败的地址，registry 会回落到内置静态映射。
	os.Setenv("DOMAINHUNTER_RDAP_BOOTSTRAP_URL", "http://127.0.0.1:1/rdap.json")
	os.Exit(m.Run())
}

func TestParseResponseTreatsReserved404AsUnknown(t *testing.T) {
	result := ParseResponse("example.do", &Response{
		ErrorCode: 404,
		Title:     "Domain Cannot Be Registered",
	}, `{"errorCode":404,"title":"Domain Cannot Be Registered"}`)

	if result.Status != domain.StatusUnknown {
		t.Fatalf("保留域名的 404 必须保持未知，实际为 %s", result.Status)
	}
}

func TestParseResponseTreatsAuthoritative404AsAvailable(t *testing.T) {
	result := ParseResponse("new-domain.example", &Response{
		ErrorCode:   404,
		Title:       "Not Found",
		Description: []string{"Domain not found"},
	}, `{"errorCode":404,"title":"Not Found"}`)

	if result.Status != domain.StatusAvailable {
		t.Fatalf("明确的 404 应判定为可注册，实际为 %s", result.Status)
	}
	if result.Confidence != domain.ConfidenceHigh {
		t.Fatalf("权威 404 的可信度应为 high，实际为 %s", result.Confidence)
	}
}

func TestParseResponseDoesNotInferAvailabilityFromEmptyObject(t *testing.T) {
	result := ParseResponse("ambiguous.example", &Response{ObjectClassName: "domain"},
		`{"objectClassName":"domain"}`)

	if result.Status == domain.StatusAvailable {
		t.Fatalf("空 RDAP 对象被错误判定为可注册: %+v", result)
	}
}

func TestParseResponseRecognizesConfiguredReservedPhrase(t *testing.T) {
	result := ParseResponse("example.do", &Response{
		ErrorCode:   404,
		Title:       "Registration is prohibited",
		Description: []string{"Registry policy"},
	}, `{"errorCode":404,"title":"Registration is prohibited"}`)

	if result.Status == domain.StatusAvailable {
		t.Fatalf("配置中的保留词被错误判定为可注册: %+v", result)
	}
}

func TestParseStatusMapsHoldAndTransferLock(t *testing.T) {
	if got := ParseStatus([]string{"client hold"}); got != domain.StatusHold {
		t.Fatalf("期望 hold，实际 %s", got)
	}
	if got := ParseStatus([]string{"client transfer prohibited"}); got != domain.StatusRegistered {
		t.Fatalf("转移锁定主状态应为 registered，实际 %s", got)
	}
}

func TestParseResponseKeepsRegisteredWhenEventsPresent(t *testing.T) {
	result := ParseResponse("registered.example", &Response{
		Status: []string{"active"},
		Entities: []Entity{{
			Roles:      []string{"registrar"},
			VCardArray: []any{"vcard", []any{[]any{"fn", map[string]any{}, "text", "Example Registrar"}}},
		}},
		Events: []Event{{EventAction: "registration"}},
	}, `{"objectClassName":"domain"}`)

	if result.Status != domain.StatusRegistered {
		t.Fatalf("期望 registered，实际 %s", result.Status)
	}
	if result.Registrar != "Example Registrar" {
		t.Fatalf("注册商解析错误: %q", result.Registrar)
	}
	if result.Confidence != domain.ConfidenceHigh {
		t.Fatalf("结构化 RDAP 数据的可信度应为 high，实际 %s", result.Confidence)
	}
}
