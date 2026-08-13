package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"DomainHunter/internal/domain"
	"DomainHunter/internal/p1"
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
	if r.URL.Query().Get("sort") == "ai_score" && r.URL.Query().Get("filter") == "" && r.URL.Query().Get("view_id") == "" {
		filter := parseListFilter(r)
		filter.Page, filter.Limit, filter.Sort = 1, 2000, ""
		result, err := s.deps.Domains.List(r.Context(), filter)
		if err != nil {
			s.writeError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		page := s.newDomainListPage(result.Domains, result.Total, result.TotalFiltered, 1, 2000, 1, false, false, result.DataStatus)
		desc := strings.EqualFold(r.URL.Query().Get("order"), "desc")
		sort.SliceStable(page.Domains, func(i, j int) bool {
			left, right := -1, -1
			if page.Domains[i].AIQualityScore != nil {
				left = *page.Domains[i].AIQualityScore
			}
			if page.Domains[j].AIQualityScore != nil {
				right = *page.Domains[j].AIQualityScore
			}
			if desc {
				return left > right
			}
			return left < right
		})
		requestedPage, requestedLimit := 1, 20
		if value, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && value > 0 {
			requestedPage = value
		}
		if value, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && value > 0 && value <= 2000 {
			requestedLimit = value
		}
		start := (requestedPage - 1) * requestedLimit
		end := start + requestedLimit
		if start > len(page.Domains) {
			start = len(page.Domains)
		}
		if end > len(page.Domains) {
			end = len(page.Domains)
		}
		page.Domains, page.Page, page.Limit = page.Domains[start:end], requestedPage, requestedLimit
		page.TotalPages = (page.TotalFiltered + requestedLimit - 1) / requestedLimit
		page.HasPrev, page.HasNext = requestedPage > 1, end < page.TotalFiltered
		s.writeJSON(w, r, http.StatusOK, page)
		return
	}
	if s.deps.P1 != nil && (r.URL.Query().Get("filter") != "" || r.URL.Query().Get("view_id") != "") {
		node, err := s.advancedFilterFromRequest(r)
		if err != nil {
			s.writeError(w, r, http.StatusBadRequest, err.Error())
			return
		}
		page, limit := 1, 20
		if value, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && value > 0 {
			page = value
		}
		if value, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && value > 0 && value <= 2000 {
			limit = value
		}
		result, err := s.deps.P1.ListDomains(r.Context(), node, page, limit)
		if err != nil {
			s.writeError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusOK, s.newDomainListPage(result.Domains, result.Total, result.TotalFiltered, result.Page, result.Limit, result.TotalPages, result.HasNext, result.HasPrev, result.DataStatus))
		return
	}
	result, err := s.deps.Domains.List(r.Context(), parseListFilter(r))
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, s.newDomainListPage(result.Domains, result.Total, result.TotalFiltered, result.Page, result.Limit, result.TotalPages, result.HasNext, result.HasPrev, result.DataStatus))
}

// domainListItem 是 v2 列表的最小 DTO：列表只服务于筛选和扫读，不扩散
// WHOIS/RDAP 原文、联系人、备注或完整名称服务器。详情和专门 raw endpoint
// 继续使用完整的 domain.Info。
type domainListItem struct {
	Name           string              `json:"name"`
	Status         domain.Status       `json:"status"`
	Registrar      string              `json:"registrar"`
	CreatedDate    *time.Time          `json:"created_date"`
	ExpiryDate     *time.Time          `json:"expiry_date"`
	UpdatedDate    *time.Time          `json:"updated_date"`
	LastChecked    time.Time           `json:"last_checked"`
	QueryMethod    string              `json:"query_method"`
	ErrorMessage   string              `json:"error_message"`
	AddedAt        *time.Time          `json:"added_at"`
	Confidence     domain.Confidence   `json:"confidence,omitempty"`
	EPPStatuses    []string            `json:"epp_statuses,omitempty"`
	NextCheckAt    *time.Time          `json:"next_check_at,omitempty"`
	Notify         bool                `json:"notify"`
	Favorite       bool                `json:"favorite,omitempty"`
	Tags           []string            `json:"tags,omitempty"`
	Priority       int                 `json:"priority,omitempty"`
	FolderID       *int64              `json:"folder_id,omitempty"`
	FolderName     string              `json:"folder,omitempty"`
	Cached         bool                `json:"cached,omitempty"`
	Review         *domain.ReviewState `json:"review,omitempty"`
	AIQualityScore *int                `json:"ai_quality_score,omitempty"`
}

type domainListPage struct {
	Domains       []*domainListItem `json:"domains"`
	Total         int               `json:"total"`
	TotalFiltered int               `json:"total_filtered"`
	Page          int               `json:"page"`
	Limit         int               `json:"limit"`
	TotalPages    int               `json:"total_pages"`
	HasNext       bool              `json:"has_next"`
	HasPrev       bool              `json:"has_prev"`
	DataStatus    string            `json:"data_status"`
}

func (s *Server) newDomainListPage(domains []*domain.Info, total, totalFiltered, page, limit, totalPages int, hasNext, hasPrev bool, dataStatus string) domainListPage {
	items := make([]*domainListItem, 0, len(domains))
	qualityScores := map[string]int{}
	if s.deps.DB != nil {
		rows, err := s.deps.DB.QueryContext(context.Background(), `SELECT lower(domain), quality_score FROM ai_domain_valuations_v2 ORDER BY created_at ASC`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var name string
				var score int
				if rows.Scan(&name, &score) == nil {
					qualityScores[name] = score
				}
			}
		}
	}
	for _, info := range domains {
		if info == nil {
			continue
		}
		copyInfo := *info
		copyInfo.Review = domain.BuildReviewState(&copyInfo, time.Now())
		var quality *int
		if value, ok := qualityScores[strings.ToLower(copyInfo.Name)]; ok {
			score := value
			quality = &score
		}
		items = append(items, &domainListItem{
			Name: copyInfo.Name, Status: copyInfo.Status, Registrar: copyInfo.Registrar,
			CreatedDate: copyInfo.CreatedDate, ExpiryDate: copyInfo.ExpiryDate, UpdatedDate: copyInfo.UpdatedDate,
			LastChecked: copyInfo.LastChecked, QueryMethod: copyInfo.QueryMethod, ErrorMessage: copyInfo.ErrorMessage,
			AddedAt: copyInfo.AddedAt, Confidence: copyInfo.Confidence, EPPStatuses: copyInfo.EPPStatuses,
			NextCheckAt: copyInfo.NextCheckAt, Favorite: copyInfo.Favorite, Tags: copyInfo.Tags,
			Notify:   copyInfo.Notify,
			Priority: copyInfo.Priority, FolderID: copyInfo.FolderID, FolderName: copyInfo.FolderName,
			Cached: copyInfo.Cached, Review: copyInfo.Review, AIQualityScore: quality,
		})
	}
	return domainListPage{Domains: items, Total: total, TotalFiltered: totalFiltered, Page: page, Limit: limit, TotalPages: totalPages, HasNext: hasNext, HasPrev: hasPrev, DataStatus: dataStatus}
}

func (s *Server) advancedFilterFromRequest(r *http.Request) (p1.FilterNode, error) {
	var node p1.FilterNode
	if raw := r.URL.Query().Get("filter"); raw != "" {
		parsed, err := p1.ParseFilter(raw)
		if err != nil {
			return p1.FilterNode{}, err
		}
		node = parsed
	} else if raw := r.URL.Query().Get("view_id"); raw != "" && s.deps.P1 != nil {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return p1.FilterNode{}, fmt.Errorf("view_id 无效")
		}
		view, err := s.deps.P1.GetSavedView(r.Context(), id)
		if err != nil {
			return p1.FilterNode{}, err
		}
		node = view.Filter
	} else {
		node = p1.FilterNode{Version: 1, Logic: "and"}
	}
	if node.Field != "" {
		node = p1.FilterNode{Version: 1, Logic: "and", Conditions: []p1.FilterNode{node}}
	}
	if node.Version == 0 {
		node.Version = 1
	}
	if node.Logic == "" {
		node.Logic = "and"
	}
	advancedNode := node
	conditions := make([]p1.FilterNode, 0, 8)
	q := r.URL.Query()
	if value := strings.TrimSpace(q.Get("search")); value != "" {
		conditions = append(conditions, p1.FilterNode{Field: "name", Op: "contains", Value: value})
	}
	if values := splitCSV(q.Get("statuses")); len(values) > 0 {
		conditions = append(conditions, p1.FilterNode{Field: "status", Op: "in", Value: values})
	} else if value := strings.TrimSpace(q.Get("status")); value != "" {
		conditions = append(conditions, p1.FilterNode{Field: "status", Op: "eq", Value: value})
	}
	if value := strings.Trim(strings.TrimSpace(q.Get("tld")), "."); value != "" {
		conditions = append(conditions, p1.FilterNode{Field: "tld", Op: "eq", Value: value})
	}
	if value := strings.TrimSpace(q.Get("registrar")); value != "" {
		conditions = append(conditions, p1.FilterNode{Field: "registrar", Op: "contains", Value: value})
	}
	if value := strings.TrimSpace(q.Get("provider")); value != "" {
		conditions = append(conditions, p1.FilterNode{Field: "provider", Op: "eq", Value: value})
	}
	if value := strings.TrimSpace(q.Get("tag")); value != "" {
		conditions = append(conditions, p1.FilterNode{Field: "tag", Op: "contains", Value: value})
	}
	if q.Get("favorite") == "true" || q.Get("favorite") == "1" {
		conditions = append(conditions, p1.FilterNode{Field: "favorite", Op: "eq", Value: true})
	}
	if len(conditions) > 0 {
		node = p1.FilterNode{Version: 1, Logic: "and", Conditions: append([]p1.FilterNode{advancedNode}, conditions...)}
	} else {
		node = advancedNode
	}
	return node, nil
}

func parseListFilter(r *http.Request) service.ListFilter {
	q := r.URL.Query()
	filter := service.ListFilter{
		Search:    strings.TrimSpace(q.Get("search")),
		Status:    strings.TrimSpace(q.Get("status")),
		Statuses:  splitCSV(q.Get("statuses")),
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
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 && v <= 2000 {
		filter.Limit = v
	}
	return filter
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
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
		Favorite    *bool     `json:"favorite"`
		Notify      *bool     `json:"notify"`
		Enabled     *bool     `json:"enabled"`
		Note        *string   `json:"note"`
		Tags        *[]string `json:"tags"`
		Priority    *int      `json:"priority"`
		FolderID    *int64    `json:"folder_id"`
		ClearFolder bool      `json:"clear_folder"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}

	patch := repository.DomainPatch{
		Favorite:    req.Favorite,
		Notify:      req.Notify,
		Enabled:     req.Enabled,
		Note:        req.Note,
		Tags:        req.Tags,
		Priority:    req.Priority,
		FolderID:    req.FolderID,
		ClearFolder: req.ClearFolder,
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
