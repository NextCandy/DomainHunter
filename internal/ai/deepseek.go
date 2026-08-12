package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const PromptVersion = "domainhunter.ai-valuation.v1"

type SanitizedInput struct {
	Domain  string `json:"domain"`
	TLD     string `json:"tld"`
	Lexical struct {
		Length    int  `json:"length"`
		HasHyphen bool `json:"has_hyphen"`
		HasDigits bool `json:"has_digits"`
		IsIDN     bool `json:"is_idn"`
	} `json:"lexical"`
	SystemFacts struct {
		Status            string   `json:"status"`
		Confidence        string   `json:"confidence"`
		ExpiryDate        *string  `json:"expiry_date,omitempty"`
		Registrar         string   `json:"registrar,omitempty"`
		EPPStatuses       []string `json:"epp_statuses,omitempty"`
		ProviderConsensus string   `json:"provider_consensus"`
		ReviewRequired    bool     `json:"review_required"`
		ReviewReasons     []string `json:"review_reasons,omitempty"`
	} `json:"system_facts"`
	UserMetadata struct {
		Tags     []string `json:"tags,omitempty"`
		Priority int      `json:"priority"`
	} `json:"user_metadata"`
}

type Output struct {
	SchemaVersion      string      `json:"schema_version"`
	Summary            string      `json:"summary"`
	QualityScore       int         `json:"quality_score"`
	LiquidityScore     int         `json:"liquidity_score"`
	RiskLevel          string      `json:"risk_level"`
	Confidence         string      `json:"confidence"`
	IndicativeValueUSD *ValueRange `json:"indicative_value_usd"`
	Strengths          []string    `json:"strengths"`
	Risks              []string    `json:"risks"`
	DataGaps           []string    `json:"data_gaps"`
	EvidenceUsed       []string    `json:"evidence_used"`
	StatusGuard        string      `json:"status_guard"`
	Disclaimer         string      `json:"disclaimer"`
}

type Client interface {
	Evaluate(ctx context.Context, profile Profile, apiKey string, input SanitizedInput) (Output, int64, error)
}

type DeepSeekCompatibleClient struct {
	Policy BaseURLPolicy
}

func SystemPrompt() string {
	return `你是 DomainHunter 的“域名研究性估价分析器”。任务是根据给定的最小化结构化事实，输出面向运营排序的研究性评估；不是交易估值、购买建议、投资建议、法律意见，也不保证可注册或可成交。

硬性规则：
1. status、confidence 和 review 是系统事实，AI 不得改变、推测或强化域名可注册结论；review_required=true 时必须将数据需复核列为主要限制。
2. 不得使用外部浏览、未提供的实时市场数据、商标数据库或隐含可比成交信息。
3. 只能基于输入字段作条件性推断；数据不足时降低 confidence 并列入 data_gaps。
4. 不得提供购买、竞价、投资或法律行动指令。
5. 只输出一个合法 JSON 对象，不输出 Markdown 或额外文字。
6. indicative_value_usd 仅能作为宽泛研究性指示区间；数据不足时为 null。

返回字段必须是 schema_version、summary、quality_score、liquidity_score、risk_level、confidence、indicative_value_usd、strengths、risks、data_gaps、evidence_used、status_guard、disclaimer。`
}

func (c DeepSeekCompatibleClient) Evaluate(ctx context.Context, profile Profile, apiKey string, input SanitizedInput) (Output, int64, error) {
	if strings.TrimSpace(apiKey) == "" {
		return Output{}, 0, errors.New("AI API Key 未配置")
	}
	endpoint, err := NormalizeBaseURL(ctx, profile.BaseURL, c.Policy)
	if err != nil {
		return Output{}, 0, err
	}
	content, err := json.Marshal(map[string]any{
		"task":  "research_only_domain_valuation",
		"input": input,
		"output_constraints": map[string]any{
			"language": "zh-CN", "json_only": true, "max_strengths": 3, "max_risks": 3, "max_data_gaps": 3,
		},
	})
	if err != nil {
		return Output{}, 0, fmt.Errorf("编码 AI 输入失败: %w", err)
	}
	payload := map[string]any{
		"model": profile.Model,
		"messages": []map[string]string{
			{"role": "system", "content": SystemPrompt()},
			{"role": "user", "content": string(content)},
		},
		"temperature":     0.15,
		"max_tokens":      profile.MaxTokens,
		"stream":          false,
		"response_format": map[string]string{"type": "json_object"},
	}
	if profile.ThinkingType == ThinkingEnabled {
		payload["thinking"] = map[string]string{"type": "enabled"}
		payload["reasoning_effort"] = profile.ReasoningEffort
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Output{}, 0, fmt.Errorf("编码 AI 请求失败: %w", err)
	}
	client := NewSafeHTTPClient(c.Policy, time.Duration(profile.TimeoutSeconds)*time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return Output{}, 0, fmt.Errorf("创建 AI 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	started := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(started).Milliseconds()
	if err != nil {
		return Output{}, latency, fmt.Errorf("AI 请求失败: %w", err)
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, 512<<10)
	responseBody, err := io.ReadAll(limited)
	if err != nil {
		return Output{}, latency, fmt.Errorf("读取 AI 响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Output{}, latency, fmt.Errorf("AI 服务返回 HTTP %d", resp.StatusCode)
	}
	var decoded struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return Output{}, latency, errors.New("AI 服务返回了无效 JSON")
	}
	if len(decoded.Choices) == 0 || strings.TrimSpace(decoded.Choices[0].Message.Content) == "" {
		return Output{}, latency, errors.New("AI 服务未返回有效内容")
	}
	var output Output
	decoder := json.NewDecoder(strings.NewReader(decoded.Choices[0].Message.Content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&output); err != nil {
		return Output{}, latency, errors.New("AI 结果不符合 JSON 输出契约")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Output{}, latency, errors.New("AI 结果包含多余内容")
	}
	if err := ValidateOutput(output); err != nil {
		return Output{}, latency, err
	}
	return output, latency, nil
}

func ValidateOutput(output Output) error {
	if output.SchemaVersion != PromptVersion {
		return errors.New("AI 输出 schema_version 不匹配")
	}
	if strings.TrimSpace(output.Summary) == "" || len([]rune(output.Summary)) > 80 {
		return errors.New("AI 输出 summary 无效")
	}
	if output.QualityScore < 0 || output.QualityScore > 100 || output.LiquidityScore < 0 || output.LiquidityScore > 100 {
		return errors.New("AI 输出评分超出 0–100 范围")
	}
	if !oneOf(output.RiskLevel, "low", "medium", "high") || !oneOf(output.Confidence, "low", "medium", "high") {
		return errors.New("AI 输出枚举无效")
	}
	if output.IndicativeValueUSD != nil {
		if output.IndicativeValueUSD.Currency != "USD" || output.IndicativeValueUSD.Low < 0 || output.IndicativeValueUSD.High < output.IndicativeValueUSD.Low {
			return errors.New("AI 输出金额区间无效")
		}
	}
	for _, list := range [][]string{output.Strengths, output.Risks, output.DataGaps, output.EvidenceUsed} {
		if len(list) > 3 {
			return errors.New("AI 输出列表超过最大数量")
		}
		for _, value := range list {
			if len([]rune(value)) > 180 {
				return errors.New("AI 输出字段过长")
			}
		}
	}
	allowedEvidence := map[string]bool{
		"domain": true, "tld": true, "lexical": true, "system_facts": true, "user_metadata": true,
		"status": true, "confidence": true, "expiry_date": true, "registrar": true,
		"epp_statuses": true, "provider_consensus": true, "review_required": true, "review_reasons": true,
		"tags": true, "priority": true,
	}
	for _, field := range output.EvidenceUsed {
		if !allowedEvidence[field] {
			return errors.New("AI 输出 evidence_used 包含未允许字段")
		}
	}
	if strings.TrimSpace(output.StatusGuard) == "" || !strings.Contains(output.StatusGuard, "不改变") ||
		strings.TrimSpace(output.Disclaimer) == "" || !strings.Contains(output.Disclaimer, "研究性") {
		return errors.New("AI 输出缺少状态护栏或免责声明")
	}
	return nil
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}
