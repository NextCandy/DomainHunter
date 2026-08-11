package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"DomainHunter/internal/p1"
)

func (s *Server) p1OrError(w http.ResponseWriter, r *http.Request) *p1.Service {
	if s.deps.P1 == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "P1 服务未初始化")
		return nil
	}
	return s.deps.P1
}

func (s *Server) handleSavedViews(w http.ResponseWriter, r *http.Request) {
	p1Service := s.p1OrError(w, r)
	if p1Service == nil {
		return
	}
	switch r.Method {
	case http.MethodGet:
		views, err := p1Service.ListSavedViews(r.Context())
		if err != nil {
			s.writeError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"views": views})
	case http.MethodPost:
		var req struct {
			Name   string        `json:"name"`
			Filter p1.FilterNode `json:"filter"`
			Shared bool          `json:"shared"`
		}
		if !s.decodeJSON(w, r, &req) {
			return
		}
		view, err := p1Service.CreateSavedView(r.Context(), req.Name, req.Filter, req.Shared, s.deps.Auth.Username())
		if err != nil {
			s.writeError(w, r, http.StatusBadRequest, err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusCreated, view)
	default:
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
	}
}

func (s *Server) handleSavedView(w http.ResponseWriter, r *http.Request) {
	p1Service := s.p1OrError(w, r)
	if p1Service == nil {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.writeError(w, r, http.StatusBadRequest, "视图 ID 无效")
		return
	}
	switch r.Method {
	case http.MethodPut, http.MethodPatch:
		var req struct {
			Name   string        `json:"name"`
			Filter p1.FilterNode `json:"filter"`
			Shared bool          `json:"shared"`
		}
		if !s.decodeJSON(w, r, &req) {
			return
		}
		if err := p1Service.UpdateSavedView(r.Context(), id, req.Name, req.Filter, req.Shared); err != nil {
			s.writeError(w, r, http.StatusBadRequest, err.Error())
			return
		}
		view, err := p1Service.GetSavedView(r.Context(), id)
		if err != nil {
			s.writeError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusOK, view)
	case http.MethodDelete:
		if err := p1Service.DeleteSavedView(r.Context(), id); err != nil {
			s.writeError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "success", "id": id})
	default:
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
	}
}

func (s *Server) handleBulkPreview(w http.ResponseWriter, r *http.Request) {
	p1Service := s.p1OrError(w, r)
	if p1Service == nil {
		return
	}
	var action p1.BulkAction
	if !s.decodeJSON(w, r, &action) {
		return
	}
	preview, err := p1Service.PreviewBulk(r.Context(), action)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, preview)
}

func (s *Server) handleBulkExecute(w http.ResponseWriter, r *http.Request) {
	p1Service := s.p1OrError(w, r)
	if p1Service == nil {
		return
	}
	var action p1.BulkAction
	if !s.decodeJSON(w, r, &action) {
		return
	}
	result, err := p1Service.ExecuteBulk(r.Context(), action)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, result)
}

func (s *Server) handleAISettings(w http.ResponseWriter, r *http.Request) {
	p1Service := s.p1OrError(w, r)
	if p1Service == nil {
		return
	}
	switch r.Method {
	case http.MethodGet:
		settings, err := p1Service.AISettings(r.Context())
		if err != nil {
			s.writeError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusOK, settings)
	case http.MethodPut, http.MethodPatch:
		var input p1.AISettingsInput
		if !s.decodeJSON(w, r, &input) {
			return
		}
		settings, err := p1Service.SaveAISettings(r.Context(), input)
		if err != nil {
			s.writeError(w, r, http.StatusBadRequest, err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusOK, settings)
	default:
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
	}
}

func (s *Server) handleAIModels(w http.ResponseWriter, r *http.Request) {
	p1Service := s.p1OrError(w, r)
	if p1Service == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	models, err := p1Service.AIModels(ctx)
	if err != nil {
		s.writeError(w, r, http.StatusBadGateway, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"models": models})
}

func (s *Server) handleAIUsage(w http.ResponseWriter, r *http.Request) {
	p1Service := s.p1OrError(w, r)
	if p1Service == nil {
		return
	}
	usage, err := p1Service.AIUsage(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, usage)
}

func (s *Server) handleAIJobs(w http.ResponseWriter, r *http.Request) {
	p1Service := s.p1OrError(w, r)
	if p1Service == nil {
		return
	}
	switch r.Method {
	case http.MethodGet:
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		jobs, err := p1Service.AIJobs(r.Context(), limit)
		if err != nil {
			s.writeError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"jobs": jobs})
	case http.MethodPost:
		var req struct {
			Domains []string `json:"domains"`
		}
		if !s.decodeJSON(w, r, &req) {
			return
		}
		queued, err := p1Service.EnqueueAI(r.Context(), req.Domains)
		if err != nil {
			s.writeError(w, r, http.StatusBadRequest, err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusAccepted, map[string]any{"status": "accepted", "queued": queued})
	default:
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
	}
}

func (s *Server) handleAICancelJob(w http.ResponseWriter, r *http.Request) {
	p1Service := s.p1OrError(w, r)
	if p1Service == nil {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "任务 ID 无效")
		return
	}
	if err := p1Service.CancelAIJob(r.Context(), id); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "cancelled", "id": id})
}

func (s *Server) handleAIValuation(w http.ResponseWriter, r *http.Request) {
	p1Service := s.p1OrError(w, r)
	if p1Service == nil {
		return
	}
	value, err := p1Service.AIValuation(r.Context(), r.PathValue("domain"))
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"valuation": value})
}

func (s *Server) handleAutomationRules(w http.ResponseWriter, r *http.Request) {
	p1Service := s.p1OrError(w, r)
	if p1Service == nil {
		return
	}
	switch r.Method {
	case http.MethodGet:
		rules, err := p1Service.ListAutomationRules(r.Context())
		if err != nil {
			s.writeError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"rules": rules})
	case http.MethodPost:
		var rule p1.AutomationRule
		if !s.decodeJSON(w, r, &rule) {
			return
		}
		created, err := p1Service.CreateAutomationRule(r.Context(), rule)
		if err != nil {
			s.writeError(w, r, http.StatusBadRequest, err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusCreated, created)
	default:
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
	}
}

func (s *Server) handleAutomationRule(w http.ResponseWriter, r *http.Request) {
	p1Service := s.p1OrError(w, r)
	if p1Service == nil {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "规则 ID 无效")
		return
	}
	switch r.Method {
	case http.MethodPut, http.MethodPatch:
		var rule p1.AutomationRule
		if !s.decodeJSON(w, r, &rule) {
			return
		}
		rule.ID = id
		if err := p1Service.UpdateAutomationRule(r.Context(), rule); err != nil {
			s.writeError(w, r, http.StatusBadRequest, err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "success", "id": id})
	case http.MethodDelete:
		if err := p1Service.DeleteAutomationRule(r.Context(), id); err != nil {
			s.writeError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "success", "id": id})
	default:
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
	}
}

func (s *Server) handleAutomationDryRun(w http.ResponseWriter, r *http.Request) {
	p1Service := s.p1OrError(w, r)
	if p1Service == nil {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "规则 ID 无效")
		return
	}
	var event p1.AutomationEvent
	if !s.decodeJSON(w, r, &event) {
		return
	}
	if strings.TrimSpace(event.ID) == "" {
		event.ID = "dry-run-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	runs, err := p1Service.DryRunAutomation(r.Context(), id, event)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"runs": runs, "side_effects": false})
}

func (s *Server) handleAutomationEvaluate(w http.ResponseWriter, r *http.Request) {
	p1Service := s.p1OrError(w, r)
	if p1Service == nil {
		return
	}
	var req struct {
		Event   p1.AutomationEvent `json:"event"`
		Execute bool               `json:"execute"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	runs, err := p1Service.EvaluateAutomation(r.Context(), req.Event, req.Execute)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"runs": runs, "executed": req.Execute})
}

func (s *Server) handleAutomationRuns(w http.ResponseWriter, r *http.Request) {
	p1Service := s.p1OrError(w, r)
	if p1Service == nil {
		return
	}
	ruleID := int64(0)
	if raw := r.URL.Query().Get("rule_id"); raw != "" {
		ruleID, _ = strconv.ParseInt(raw, 10, 64)
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := p1Service.ListAutomationRuns(r.Context(), ruleID, limit)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"runs": runs})
}
