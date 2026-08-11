package query

import (
	"context"
	"fmt"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/registry"
)

// Outcome 一次完整查询的产物：最终结论 + 全部证据
type Outcome struct {
	Domain   string
	TLD      string
	Info     *domain.Info
	Winner   Result
	Results  []Result
	Evidence []domain.Evidence
	Plan     []Step
}

// Engine 查询引擎：按 Policy 依次调用 Provider，并合成最终结论。
//
// 安全模型（不可放宽）：
//   - 只有明确的"未注册"信号才允许得到 available
//   - 超时、连接失败、HTTP 404 但语义不明、保留域名策略文本一律不是 available
//   - 全部查询源都不可用时返回 skipped/error，绝不返回 available
type Engine struct {
	providers *Registry
	policy    *Policy
	health    *HealthTracker
	limiter   *Limiter
}

// NewEngine 创建查询引擎
func NewEngine(providers *Registry, policy *Policy) *Engine {
	engine := &Engine{
		providers: providers,
		policy:    policy,
		health:    NewHealthTracker(),
		limiter:   NewLimiter(),
	}
	engine.ApplyPolicy(policy.Config())
	return engine
}

// ApplyPolicy 在策略变更后同步限速规则
func (e *Engine) ApplyPolicy(cfg Config) {
	e.policy.Update(cfg)
	e.limiter.Apply(cfg.RateLimits, DefaultRateLimits)
}

// Providers 返回底层 Provider 集合
func (e *Engine) Providers() *Registry { return e.providers }

// Policy 返回当前策略
func (e *Engine) Policy() *Policy { return e.policy }

// Health 返回健康统计器
func (e *Engine) Health() *HealthTracker { return e.health }

// Limiter 返回限速器
func (e *Engine) Limiter() *Limiter { return e.limiter }

// Query 查询单个域名
func (e *Engine) Query(ctx context.Context, name string) Outcome {
	name = domain.Normalize(name)
	tld := registry.FindBestTLD(name)
	req := Request{Domain: name, TLD: tld}

	out := Outcome{Domain: name, TLD: tld}
	if tld == "" {
		out.Winner = Result{
			Domain:     name,
			Status:     domain.StatusSkipped,
			Provider:   "none",
			Confidence: domain.ConfidenceLow,
			Note:       "没有匹配的查询配置，已跳过",
			StartedAt:  time.Now(),
			FinishedAt: time.Now(),
		}
		out.Info = out.Winner.ToInfo()
		out.Info.ErrorMessage = out.Winner.Note
		return out
	}

	out.Plan = e.policy.Plan(ctx, req, e.providers)
	if len(out.Plan) == 0 {
		out.Winner = Result{
			Domain:     name,
			Status:     domain.StatusSkipped,
			Provider:   "none",
			Confidence: domain.ConfidenceLow,
			Note:       "没有可用的 RDAP/WHOIS 查询源，已跳过",
			StartedAt:  time.Now(),
			FinishedAt: time.Now(),
		}
		out.Info = out.Winner.ToInfo()
		out.Info.ErrorMessage = out.Winner.Note
		return out
	}

	var (
		optInErr        *Result
		otherErr        *Result
		lastUnknown     *Result
		pendingAvail    *Result
		availableVotes  int
		skippedCount    int
		producedResults int
	)

	for _, step := range out.Plan {
		if err := ctx.Err(); err != nil {
			res := errorResult(step.Provider, name, time.Now(), err)
			out.Results = append(out.Results, res)
			out.Evidence = append(out.Evidence, res.Evidence())
			break
		}

		provider, ok := e.providers.Get(step.Provider)
		if !ok {
			continue
		}
		if err := e.limiter.Wait(ctx, step.Provider, tld); err != nil {
			res := errorResult(step.Provider, name, time.Now(), err)
			out.Results = append(out.Results, res)
			out.Evidence = append(out.Evidence, res.Evidence())
			break
		}

		res := provider.Query(ctx, req)
		if res.Provider == "" {
			res.Provider = step.Provider
		}
		if res.Domain == "" {
			res.Domain = name
		}
		e.health.Record(res)
		out.Results = append(out.Results, res)
		out.Evidence = append(out.Evidence, res.Evidence())
		producedResults++

		if res.Err != nil {
			// 显式启用的专用源出错必须冒泡成错误进入重试，
			// 不能被通用源的 unknown 吞掉后被长期缓存。
			if IsOptIn(step.Provider) {
				if optInErr == nil {
					copyRes := res
					optInErr = &copyRes
				}
			} else if otherErr == nil {
				copyRes := res
				otherErr = &copyRes
			}
			continue
		}

		if res.Status == domain.StatusSkipped {
			skippedCount++
			continue
		}

		if res.Status == domain.StatusAvailable {
			availableVotes++
		}

		if !domain.IsDefinitive(res.Status) {
			copyRes := res
			lastUnknown = &copyRes
			continue
		}

		if res.Status != domain.StatusAvailable {
			// 明确的"已被占用"类结论优先级最高，立即结束查询链路。
			return e.finalize(out, res)
		}

		switch step.Available {
		case AvailableTrust:
			return e.finalize(out, res)
		case AvailableConfirm:
			if availableVotes >= 2 {
				return e.finalize(out, res)
			}
			if pendingAvail == nil {
				copyRes := res
				pendingAvail = &copyRes
			}
		default: // AvailableDistrust
			if pendingAvail == nil {
				copyRes := res
				pendingAvail = &copyRes
			}
		}
	}

	// 走到这里说明没有拿到可直接采用的结论。
	switch {
	case optInErr != nil:
		return e.finalize(out, *optInErr)

	case pendingAvail != nil:
		res := *pendingAvail
		res.Status = domain.StatusUnknown
		res.Confidence = domain.ConfidenceLow
		res.Note = fmt.Sprintf("%s 报告可注册，但未获得二次确认，按未知处理", res.Provider)
		return e.finalize(out, res)

	case lastUnknown != nil:
		return e.finalize(out, *lastUnknown)

	case producedResults > 0 && skippedCount == producedResults:
		res := Result{
			Domain:     name,
			Status:     domain.StatusSkipped,
			Provider:   "none",
			Confidence: domain.ConfidenceLow,
			Note:       "没有可用的 RDAP/WHOIS 查询源，已跳过",
			StartedAt:  time.Now(),
			FinishedAt: time.Now(),
		}
		return e.finalize(out, res)

	case otherErr != nil:
		return e.finalize(out, *otherErr)

	default:
		res := Result{
			Domain:     name,
			Status:     domain.StatusError,
			Provider:   "none",
			Confidence: domain.ConfidenceLow,
			Err:        NewError(KindUnavailable, "none", "RDAP 和 WHOIS 查询失败"),
			StartedAt:  time.Now(),
			FinishedAt: time.Now(),
		}
		return e.finalize(out, res)
	}
}

// finalize 组装最终结论，并根据证据一致性调整可信度
func (e *Engine) finalize(out Outcome, winner Result) Outcome {
	if domain.IsDefinitive(winner.Status) {
		agree := 0
		for _, res := range out.Results {
			if res.Err == nil && res.Status == winner.Status {
				agree++
			}
		}
		if agree >= 2 {
			winner.Confidence = domain.ConfidenceHigh
		} else if winner.Confidence == "" {
			winner.Confidence = domain.ConfidenceMedium
		}
	} else if winner.Confidence == "" {
		winner.Confidence = domain.ConfidenceLow
	}

	out.Winner = winner
	out.Info = winner.ToInfo()
	out.Info.Evidence = out.Evidence

	// 详情页需要看到所有查询源拿到的原始报文，优先展示胜出源的原文；
	// 胜出源没有原文时（例如超时）退回到任何一个拿到过原文的源。
	if out.Info.WhoisRaw == "" {
		for i := len(out.Results) - 1; i >= 0; i-- {
			if raw := out.Results[i].Raw; raw != "" {
				out.Info.WhoisRaw = raw
				break
			}
		}
	}
	return out
}
