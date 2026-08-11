package query

import (
	"context"
	"time"
)

// Provider 一个域名查询源。
//
// 引擎只认识这个接口：RDAP、注册局 WHOIS、WHOIS.LS 与本地备用服务都是它的实现。
// 新增查询源只需要实现 Provider 并注册进 Registry，不需要改动任何调度或业务代码。
type Provider interface {
	// Name 返回稳定的短标识，会写入数据库与 API（如 rdap / whois / whois_ls / fallback）
	Name() string

	// Supports 判断该 Provider 当前是否能处理这个请求
	// （例如 TLD 没有 RDAP 端点，或备用服务未对该后缀启用）
	Supports(ctx context.Context, req Request) bool

	// Query 执行一次查询。实现必须尊重 ctx，并且永远返回 Result（错误放在 Result.Err）
	Query(ctx context.Context, req Request) Result
}

// TimeoutAware 允许在设置界面热更新超时时间的 Provider
type TimeoutAware interface {
	UpdateTimeout(timeout time.Duration)
}

// Registry Provider 集合
type Registry struct {
	order     []string
	providers map[string]Provider
}

// NewRegistry 创建 Provider 集合，注册顺序即默认回退顺序
func NewRegistry(providers ...Provider) *Registry {
	r := &Registry{providers: make(map[string]Provider, len(providers))}
	for _, p := range providers {
		if p == nil {
			continue
		}
		if _, exists := r.providers[p.Name()]; !exists {
			r.order = append(r.order, p.Name())
		}
		r.providers[p.Name()] = p
	}
	return r
}

// Get 按名称取 Provider
func (r *Registry) Get(name string) (Provider, bool) {
	if r == nil {
		return nil, false
	}
	p, ok := r.providers[name]
	return p, ok
}

// Names 返回注册顺序下的全部 Provider 名称
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.order...)
}

// All 返回注册顺序下的全部 Provider
func (r *Registry) All() []Provider {
	if r == nil {
		return nil
	}
	out := make([]Provider, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.providers[name])
	}
	return out
}

// UpdateTimeout 把新的超时时间广播给支持热更新的 Provider
func (r *Registry) UpdateTimeout(timeout time.Duration) {
	if r == nil || timeout <= 0 {
		return
	}
	for _, p := range r.All() {
		if ta, ok := p.(TimeoutAware); ok {
			ta.UpdateTimeout(timeout)
		}
	}
}
