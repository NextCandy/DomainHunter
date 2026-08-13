package ai

import (
	"fmt"
	"strings"
)

func validateProfileInput(input ProfileInput) error {
	if strings.TrimSpace(input.Name) == "" || len([]rune(strings.TrimSpace(input.Name))) > 80 {
		return fmt.Errorf("AI 档案名称不能为空且不能超过 80 个字符")
	}
	if input.Provider != ProviderDeepSeek && input.Provider != ProviderOpenAICompatible {
		return fmt.Errorf("不支持的 AI Provider")
	}
	if strings.TrimSpace(input.BaseURL) == "" || len(input.BaseURL) > 512 {
		return fmt.Errorf("AI Base URL 无效")
	}
	if strings.TrimSpace(input.Model) == "" || len([]rune(input.Model)) > 120 {
		return fmt.Errorf("AI 模型名称无效")
	}
	if input.ThinkingType != ThinkingDisabled && input.ThinkingType != ThinkingEnabled {
		return fmt.Errorf("thinking_type 无效")
	}
	if input.ReasoningEffort != ReasoningLow && input.ReasoningEffort != ReasoningHigh && input.ReasoningEffort != ReasoningMax {
		return fmt.Errorf("reasoning_effort 无效")
	}
	if input.TimeoutSeconds < 5 || input.TimeoutSeconds > 120 {
		return fmt.Errorf("AI 超时必须在 5–120 秒之间")
	}
	if input.MaxTokens < 100 || input.MaxTokens > 4096 {
		return fmt.Errorf("AI 最大输出必须在 100–4096 tokens 之间")
	}
	if input.Concurrency < 1 || input.Concurrency > 5 {
		return fmt.Errorf("AI 并发必须在 1–5 之间")
	}
	if input.CacheTTLHours < 1 || input.CacheTTLHours > 168 {
		return fmt.Errorf("AI 缓存 TTL 必须在 1–168 小时之间")
	}
	return nil
}
