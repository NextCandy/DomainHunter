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

const PromptVersion = "domainhunter.ai-valuation.v2-report"

var (
	ErrProviderAuth   = errors.New("AI Provider 认证失败")
	ErrProviderConfig = errors.New("AI Provider 配置无效")
)

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
	SchemaVersion string `json:"schema_version"`
	Summary       string `json:"summary"`
	// Score is the single user-facing domain score. QualityScore remains accepted
	// for compatibility with older test doubles, but the v2 prompt asks for score.
	Score              int         `json:"score"`
	QualityScore       int         `json:"quality_score,omitempty"`
	LiquidityScore     int         `json:"liquidity_score"`
	RiskLevel          string      `json:"risk_level"`
	Confidence         string      `json:"confidence"`
	IndicativeValueUSD *ValueRange `json:"indicative_value_usd"`
	PriceEvaluationCNY *ValueRange `json:"price_evaluation_cny"`
	CoreAnalysis       string      `json:"core_analysis"`
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

// compatibleResponse keeps the provider envelope deliberately loose. OpenAI
// compatible gateways generally return content as a string, but some models
// return an array of text parts or place the final text in reasoning_content.
// Keeping those fields as RawMessage lets us accept those harmless wire-format
// differences without weakening the strict report JSON contract below.
type compatibleResponse struct {
	Choices []struct {
		Message struct {
			Content          json.RawMessage `json:"content"`
			ReasoningContent json.RawMessage `json:"reasoning_content"`
			ToolCalls        json.RawMessage `json:"tool_calls"`
		} `json:"message"`
		Text json.RawMessage `json:"text"`
	} `json:"choices"`
}

func SystemPrompt() string {
	return `你是域名鉴定师，拥有丰富的鉴定经验，能根据域名市场价格和各渠道常见交易价格区间进行价格鉴定及用途鉴定。你正在为 DomainHunter 生成研究性域名估价报告；不是交易估值、购买建议、投资建议、法律意见，也不保证可注册或可成交。

硬性规则：
1. status、confidence 和 review 是系统事实，AI 不得改变、推测或强化域名可注册结论；review_required=true 时必须将数据需复核列为主要限制。
2. 结合域名长度、字符结构、语义、记忆点、前缀习惯、后缀适用性、用途和市场需求进行判断。可以使用你掌握的行业常识和常见交易区间，但不得编造某一笔具体成交、实时挂牌价、商标结论或未提供的事实。
3. 若检测到域名后缀的谐音与前缀能够拼成一个完整的词，将其按完整词语分析；这个“按全称理解”的判断不额外抬高或压低价格，价格仍由整体稀缺性、商业用途和市场需求决定。
4. 只能基于输入字段作条件性推断；数据不足时降低 confidence、扩大价格区间并列入 data_gaps。
5. 即使没有查询结果或存在数据复核标记，也必须仅根据域名本身完成研究性估价；把缺失事实写入 data_gaps，不得因为未检查而拒绝给出价格区间。
6. 不得提供购买、竞价、投资或法律行动指令。
7. 只输出一个合法 JSON 对象，不输出 Markdown、列表符号或额外文字；summary 不超过 80 个汉字。
8. price_evaluation_cny 必须严格是对象 {"low":整数,"high":整数,"currency":"CNY"}，三个字段均不可缺少，不能写成字符串或 null；即使信息有限也要给出保守区间。
9. core_analysis 必须是一段中文综合分析，说明域名组成、语义、记忆点、后缀适用性、前缀习惯、适合用途、市场需求和溢价空间；不得声称有未提供的具体成交证据。

严格按以下类型返回；示例中的 0 必须替换为合理整数：
{"schema_version":"domainhunter.ai-valuation.v2-report","summary":"不超过80字","score":0,"liquidity_score":0,"risk_level":"low|medium|high","confidence":"low|medium|high","price_evaluation_cny":{"low":0,"high":0,"currency":"CNY"},"core_analysis":"中文综合分析","strengths":[],"risks":[],"data_gaps":[],"evidence_used":[],"status_guard":"AI 不改变系统查询结论","disclaimer":"仅供研究性用途"}
不要返回未声明字段。`
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
		"task":  "domain_expert_valuation_report",
		"input": input,
		"output_constraints": map[string]any{
			"language": "zh-CN", "json_only": true, "report_order": []string{"domain", "score", "price_evaluation", "core_analysis"}, "max_strengths": 3, "max_risks": 3, "max_data_gaps": 3,
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
	if isDeepSeekV4(profile.Model) {
		if profile.ThinkingType == ThinkingEnabled {
			payload["thinking"] = map[string]string{"type": "enabled"}
			payload["reasoning_effort"] = deepSeekReasoningEffort(profile.ReasoningEffort)
		} else {
			// DeepSeek V4 defaults to thinking mode. Explicitly disable it when the
			// profile says so; otherwise reasoning can consume the whole completion
			// allowance before the compact JSON report is finished.
			payload["thinking"] = map[string]string{"type": "disabled"}
		}
	} else if profile.ThinkingType == ThinkingEnabled {
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
		providerMessage := compatibleProviderError(responseBody)
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return Output{}, latency, fmt.Errorf("%w：默认 AI 的 API Key 无效或已过期，请在 AI 与自动化中更新 Key", ErrProviderAuth)
		case http.StatusForbidden:
			return Output{}, latency, fmt.Errorf("%w：默认 AI API Key 没有调用权限，请检查 DeepSeek 账户权限或更换 Key", ErrProviderAuth)
		case http.StatusTooManyRequests:
			if providerMessage != "" {
				return Output{}, latency, fmt.Errorf("默认 AI Provider 已限流，请稍后重试：%s", providerMessage)
			}
			return Output{}, latency, errors.New("默认 AI Provider 已限流，请稍后重试")
		case http.StatusNotFound:
			return Output{}, latency, fmt.Errorf("%w：默认 AI 模型或接口地址不存在，请检查模型配置", ErrProviderConfig)
		}
		return Output{}, latency, fmt.Errorf("AI 服务返回 HTTP %d", resp.StatusCode)
	}
	var decoded compatibleResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return Output{}, latency, errors.New("AI 服务返回了无效 JSON")
	}
	if len(decoded.Choices) == 0 {
		return Output{}, latency, errors.New("AI 服务未返回有效内容")
	}
	responseText := firstCompatibleText(decoded.Choices[0].Message.Content, decoded.Choices[0].Message.ReasoningContent, decoded.Choices[0].Text)
	if responseText == "" {
		if len(decoded.Choices[0].Message.ToolCalls) > 0 && string(decoded.Choices[0].Message.ToolCalls) != "null" {
			return Output{}, latency, errors.New("AI 模型返回了工具调用而不是估价报告")
		}
		return Output{}, latency, errors.New("AI 服务未返回有效文本内容")
	}
	var output Output
	decoder := json.NewDecoder(strings.NewReader(responseText))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&output); err != nil {
		return Output{}, latency, errors.New("AI 结果不符合 JSON 输出契约")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Output{}, latency, errors.New("AI 结果包含多余内容")
	}
	output.EvidenceUsed = normalizeEvidenceUsed(output.EvidenceUsed)
	if err := ValidateOutput(output); err != nil {
		return Output{}, latency, err
	}
	return output, latency, nil
}

func normalizeEvidenceUsed(values []string) []string {
	allowed := map[string]bool{
		"domain": true, "tld": true, "lexical": true, "system_facts": true, "user_metadata": true,
		"status": true, "confidence": true, "expiry_date": true, "registrar": true,
		"epp_statuses": true, "provider_consensus": true, "review_required": true, "review_reasons": true,
		"tags": true, "priority": true,
	}
	result := make([]string, 0, min(len(values), 3))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if !allowed[value] {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
		if len(result) == 3 {
			break
		}
	}
	return result
}

func compatibleProviderError(body []byte) string {
	var envelope struct {
		Error struct {
			Type    string `json:"type"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return ""
	}
	value := strings.TrimSpace(envelope.Error.Type)
	if value == "" {
		value = strings.TrimSpace(envelope.Error.Code)
	}
	if value == "" {
		value = strings.TrimSpace(envelope.Error.Message)
	}
	return safeProviderMessage(value)
}

func safeProviderMessage(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	runes := []rune(value)
	if len(runes) > 80 {
		value = string(runes[:80])
	}
	return value
}

func isDeepSeekV4(model string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(model)), "deepseek-v4")
}

func deepSeekReasoningEffort(effort ReasoningEffort) string {
	if effort == ReasoningMax {
		return string(ReasoningMax)
	}
	return string(ReasoningHigh)
}

// firstCompatibleText extracts text from the common OpenAI-compatible content
// shapes. The first non-empty field wins: final content is preferred, with
// reasoning_content and legacy completions text used only as fallbacks.
func firstCompatibleText(values ...json.RawMessage) string {
	for _, raw := range values {
		if text := compatibleText(raw); strings.TrimSpace(text) != "" {
			return stripJSONCodeFence(text)
		}
	}
	return ""
}

func compatibleText(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}

	var text string
	if raw[0] == '"' && json.Unmarshal(raw, &text) == nil {
		return text
	}

	// A few gateways expose content as [{"type":"text","text":"..."}]
	// (or as an array of plain strings) instead of one string.
	if raw[0] == '[' {
		var parts []json.RawMessage
		if json.Unmarshal(raw, &parts) == nil {
			var builder strings.Builder
			for _, part := range parts {
				builder.WriteString(compatibleText(part))
			}
			return builder.String()
		}
	}

	if raw[0] == '{' {
		var part struct {
			Text    json.RawMessage `json:"text"`
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(raw, &part) == nil {
			if text := compatibleText(part.Text); text != "" {
				return text
			}
			return compatibleText(part.Content)
		}
	}

	return ""
}

func stripJSONCodeFence(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "```") {
		return value
	}
	lines := strings.Split(value, "\n")
	if len(lines) >= 2 {
		lines = lines[1:]
		if last := len(lines) - 1; last >= 0 && strings.TrimSpace(lines[last]) == "```" {
			lines = lines[:last]
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func ValidateOutput(output Output) error {
	if output.SchemaVersion != PromptVersion {
		return errors.New("AI 输出 schema_version 不匹配")
	}
	if strings.TrimSpace(output.Summary) == "" || len([]rune(output.Summary)) > 80 {
		return errors.New("AI 输出 summary 无效")
	}
	if output.Score == 0 && output.QualityScore > 0 {
		output.Score = output.QualityScore
	}
	if output.Score < 0 || output.Score > 100 || output.QualityScore < 0 || output.QualityScore > 100 || output.LiquidityScore < 0 || output.LiquidityScore > 100 {
		return errors.New("AI 输出评分超出 0–100 范围")
	}
	if !oneOf(output.RiskLevel, "low", "medium", "high") || !oneOf(output.Confidence, "low", "medium", "high") {
		return errors.New("AI 输出枚举无效")
	}
	if output.PriceEvaluationCNY == nil || output.PriceEvaluationCNY.Currency != "CNY" || output.PriceEvaluationCNY.Low < 0 || output.PriceEvaluationCNY.High < output.PriceEvaluationCNY.Low {
		return errors.New("AI 输出人民币价格区间无效")
	}
	if output.PriceEvaluationCNY.High > 1_000_000_000 {
		return errors.New("AI 输出人民币价格区间过大")
	}
	if strings.TrimSpace(output.CoreAnalysis) == "" || len([]rune(output.CoreAnalysis)) > 1200 {
		return errors.New("AI 输出核心分析无效")
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
