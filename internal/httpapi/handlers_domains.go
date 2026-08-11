package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/registry"
	"DomainHunter/internal/repository"
	"DomainHunter/internal/service"
)

// handleDomains 旧版域名列表（分页 + 搜索 + 状态筛选）
func (s *Server) handleDomains(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
		return
	}

	if r.URL.Query().Get("stats_only") == "true" {
		_, total, err := s.deps.Domains.StatusCounts(r.Context())
		if err != nil {
			s.writeError(w, r, http.StatusInternalServerError, "获取域名统计失败: "+err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"total": total})
		return
	}

	result, err := s.deps.Domains.List(r.Context(), parseListFilter(r))
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, result)
}

// handleDomainsV2 新版域名列表，支持更多筛选与排序
func (s *Server) handleDomainsV2(w http.ResponseWriter, r *http.Request) {
	result, err := s.deps.Domains.List(r.Context(), parseListFilter(r))
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, result)
}

func parseListFilter(r *http.Request) service.ListFilter {
	q := r.URL.Query()
	filter := service.ListFilter{
		Search:    strings.TrimSpace(q.Get("search")),
		Status:    strings.TrimSpace(q.Get("status")),
		TLD:       strings.TrimSpace(q.Get("tld")),
		Registrar: strings.TrimSpace(q.Get("registrar")),
		Provider:  strings.TrimSpace(q.Get("provider")),
		Tag:       strings.TrimSpace(q.Get("tag")),
		Favorite:  q.Get("favorite") == "true" || q.Get("favorite") == "1",
		Sort:      strings.TrimSpace(q.Get("sort")),
		Order:     strings.TrimSpace(q.Get("order")),
		Page:      1,
		Limit:     10,
	}
	if v, err := strconv.Atoi(q.Get("page")); err == nil && v > 0 {
		filter.Page = v
	}
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 && v <= 500 {
		filter.Limit = v
	}
	return filter
}

// handleDomainDetail 旧版域名详情：GET /api/domain/{name}
func (s *Server) handleDomainDetail(w http.ResponseWriter, r *http.Request) {
	name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/domain/"), "/")
	if name == "" {
		s.writeError(w, r, http.StatusBadRequest, "域名不能为空")
		return
	}
	info, err := s.deps.Domains.Get(r.Context(), name)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, info)
}

// handleDomainDetailV2 新版详情：附带证据、观测历史与查询尝试
func (s *Server) handleDomainDetailV2(w http.ResponseWriter, r *http.Request) {
	detail, err := s.deps.Domains.GetDetail(r.Context(), r.PathValue("domain"), 50, 30)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, detail)
}

// handleDomainHistory 状态变化历史
func (s *Server) handleDomainHistory(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("domain")
	limit := 100
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = v
	}
	history, err := s.deps.Domains.History(r.Context(), name, limit)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"domain": name, "history": history})
}

// handleDomainAttempts 查询源历史
func (s *Server) handleDomainAttempts(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("domain")
	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = v
	}
	attempts, err := s.deps.Domains.Attempts(r.Context(), name, limit)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"domain": name, "attempts": attempts})
}

// handleDomainCheck 旧版立即检查：POST /api/domain/check/{name}
func (s *Server) handleDomainCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
		return
	}
	name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/domain/check/"), "/")
	s.checkDomain(w, r, name)
}

// handleDomainCheckV2 新版立即检查
func (s *Server) handleDomainCheckV2(w http.ResponseWriter, r *http.Request) {
	s.checkDomain(w, r, r.PathValue("domain"))
}

func (s *Server) checkDomain(w http.ResponseWriter, r *http.Request, name string) {
	if strings.TrimSpace(name) == "" {
		s.writeError(w, r, http.StatusBadRequest, "域名不能为空")
		return
	}
	info, err := s.deps.Monitor.CheckNow(r.Context(), name)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrDomainNotMonitored) {
			status = http.StatusNotFound
		}
		s.writeError(w, r, status, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, info)
}

// handleDomainAdd 添加单个域名
func (s *Server) handleDomainAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
		return
	}

	var req struct {
		Domain string `json:"domain"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}

	name, err := s.deps.Domains.Add(r.Context(), req.Domain)
	if err != nil {
		// 旧前端读取 status 字段判断成功与否，这里保持 200 + status=error 的形式。
		s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "error", "message": err.Error()})
		return
	}

	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"status":  "success",
		"message": "Domain added successfully",
		"domain":  name,
		"info": &domain.Info{
			Name:        name,
			Status:      domain.StatusUnknown,
			QueryMethod: "checking",
		},
	})
}

// handleDomainBatchAdd 批量添加域名
func (s *Server) handleDomainBatchAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
		return
	}

	var req struct {
		Domains []string `json:"domains"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if len(req.Domains) == 0 {
		s.writeError(w, r, http.StatusBadRequest, "No domains provided")
		return
	}
	if len(req.Domains) > 1000 {
		s.writeError(w, r, http.StatusBadRequest, "一次最多添加1000个域名")
		return
	}

	result, err := s.deps.Domains.BatchAdd(r.Context(), req.Domains)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"status":              "success",
		"added_count":         result.Added,
		"invalid_count":       result.InvalidCount,
		"invalid_domains":     result.Invalid,
		"unsupported_count":   result.UnsupportedCount,
		"unsupported_domains": result.Unsupported,
		"duplicate_count":     result.DuplicateCount,
		"duplicate_domains":   result.Duplicate,
	})
}

// handleDomainRemove 旧版删除：DELETE /api/domain/remove/{name}
func (s *Server) handleDomainRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
		return
	}
	name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/domain/remove/"), "/")
	if name == "" {
		s.writeError(w, r, http.StatusBadRequest, "域名不能为空")
		return
	}
	if err := s.deps.Domains.Remove(r.Context(), name); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "删除域名失败: "+err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "Domain removed successfully",
		"domain":  name,
	})
}

// handleDomainDeleteV2 新版删除
func (s *Server) handleDomainDeleteV2(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("domain")
	if err := s.deps.Domains.Remove(r.Context(), name); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "删除域名失败: "+err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"status": "success", "domain": name})
}

// handleDomainBatchDelete 批量删除
func (s *Server) handleDomainBatchDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domains []string `json:"domains"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if len(req.Domains) == 0 {
		s.writeError(w, r, http.StatusBadRequest, "没有需要删除的域名")
		return
	}
	deleted, err := s.deps.Domains.RemoveMany(r.Context(), req.Domains)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "批量删除失败: "+err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "success", "deleted": deleted})
}

// handleDomainBatchCheck 批量立即检查（异步入队，按手动优先级执行）
func (s *Server) handleDomainBatchCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domains []string `json:"domains"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if len(req.Domains) == 0 {
		s.writeError(w, r, http.StatusBadRequest, "没有需要检查的域名")
		return
	}
	if len(req.Domains) > 500 {
		s.writeError(w, r, http.StatusBadRequest, "一次最多检查500个域名")
		return
	}

	queued := 0
	for _, name := range req.Domains {
		if strings.TrimSpace(name) == "" {
			continue
		}
		s.deps.Monitor.Enqueue(name, domain.PriorityManual)
		queued++
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"status":  "success",
		"queued":  queued,
		"message": "已加入高优先级队列，稍后自动刷新",
	})
}

// handleDomainPatch 更新域名属性（收藏 / 备注 / 标签 / 通知开关）
func (s *Server) handleDomainPatch(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("domain")

	var req struct {
		Favorite *bool     `json:"favorite"`
		Notify   *bool     `json:"notify"`
		Enabled  *bool     `json:"enabled"`
		Note     *string   `json:"note"`
		Tags     *[]string `json:"tags"`
		Priority *int      `json:"priority"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}

	patch := repository.DomainPatch{
		Favorite: req.Favorite,
		Notify:   req.Notify,
		Enabled:  req.Enabled,
		Note:     req.Note,
		Tags:     req.Tags,
		Priority: req.Priority,
	}
	if err := s.deps.Domains.Update(r.Context(), name, patch); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	info, err := s.deps.Domains.Get(r.Context(), name)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, info)
}

// handleDomainWhoisRaw 返回保存的原始 WHOIS/RDAP 报文
func (s *Server) handleDomainWhoisRaw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
		return
	}
	name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/domain/whois-raw/"), "/")
	if name == "" {
		s.writeError(w, r, http.StatusBadRequest, "域名不能为空")
		return
	}

	info, err := s.deps.Domains.Get(r.Context(), name)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "获取域名信息失败: "+err.Error())
		return
	}
	if info == nil || info.WhoisRaw == "" {
		s.writeError(w, r, http.StatusNotFound, "WHOIS data not found, please wait for the query to complete")
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"domain":    name,
		"whois_raw": info.WhoisRaw,
		"timestamp": info.LastChecked.Format("2006-01-02 15:04:05"),
	})
}

// handleFacets 返回完整的筛选项清单（后缀、注册商、查询源、状态、标签）
func (s *Server) handleFacets(w http.ResponseWriter, r *http.Request) {
	facets, err := s.deps.Domains.Facets(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, facets)
}

// handleRecentObservations 返回全局最近的状态变化，用于查询历史页
func (s *Server) handleRecentObservations(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = v
	}
	changes, err := s.deps.Domains.RecentChanges(r.Context(), limit)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"observations": changes})
}

// handleMeta 返回前端需要的静态元数据：状态列表与已知 TLD
func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	statuses := make([]domain.StatusInfo, 0, len(domain.AllStatuses()))
	for _, status := range domain.AllStatuses() {
		statuses = append(statuses, domain.GetStatusInfo(status))
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"statuses":  statuses,
		"providers": s.deps.Engine.Providers().Names(),
		"tld_count": len(registry.SupportedTLDs()),
		"version":   s.deps.Version,
	})
}
