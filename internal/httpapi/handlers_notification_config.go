package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"DomainHunter/internal/repository"
)

func (s *Server) handleNotificationRules(w http.ResponseWriter, r *http.Request) {
	if s.deps.NotificationConfig == nil {
		s.writeError(w, r, http.StatusNotImplemented, "通知规则未启用")
		return
	}
	rules, err := s.deps.NotificationConfig.ListRules(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"rules": rules})
}

func (s *Server) handleNotificationRuleCreate(w http.ResponseWriter, r *http.Request) {
	if s.deps.NotificationConfig == nil {
		s.writeError(w, r, http.StatusNotImplemented, "通知规则未启用")
		return
	}
	var rule repository.NotificationRule
	if !s.decodeJSON(w, r, &rule) {
		return
	}
	rule.Name = strings.TrimSpace(rule.Name)
	if rule.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "规则名称不能为空")
		return
	}
	created, err := s.deps.NotificationConfig.CreateRule(r.Context(), rule)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.reloadNotificationConfig(r); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusCreated, created)
}

func (s *Server) handleNotificationRuleUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := notificationConfigID(r)
	if !ok || s.deps.NotificationConfig == nil {
		s.writeError(w, r, http.StatusBadRequest, "规则 ID 无效")
		return
	}
	var rule repository.NotificationRule
	if !s.decodeJSON(w, r, &rule) {
		return
	}
	rule.ID = id
	if err := s.deps.NotificationConfig.UpdateRule(r.Context(), rule); err != nil {
		s.writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.reloadNotificationConfig(r); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "success", "id": id})
}

func (s *Server) handleNotificationRuleDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := notificationConfigID(r)
	if !ok || s.deps.NotificationConfig == nil {
		s.writeError(w, r, http.StatusBadRequest, "规则 ID 无效")
		return
	}
	if err := s.deps.NotificationConfig.DeleteRule(r.Context(), id); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.reloadNotificationConfig(r); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "success", "id": id})
}

func (s *Server) handleNotificationTemplates(w http.ResponseWriter, r *http.Request) {
	if s.deps.NotificationConfig == nil {
		s.writeError(w, r, http.StatusNotImplemented, "通知模板未启用")
		return
	}
	templates, err := s.deps.NotificationConfig.ListTemplates(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"templates": templates})
}

func (s *Server) handleNotificationTemplateCreate(w http.ResponseWriter, r *http.Request) {
	if s.deps.NotificationConfig == nil {
		s.writeError(w, r, http.StatusNotImplemented, "通知模板未启用")
		return
	}
	var template repository.NotificationTemplate
	if !s.decodeJSON(w, r, &template) {
		return
	}
	created, err := s.deps.NotificationConfig.CreateTemplate(r.Context(), template)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.reloadNotificationConfig(r); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusCreated, created)
}

func (s *Server) handleNotificationTemplateUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := notificationConfigID(r)
	if !ok || s.deps.NotificationConfig == nil {
		s.writeError(w, r, http.StatusBadRequest, "模板 ID 无效")
		return
	}
	var template repository.NotificationTemplate
	if !s.decodeJSON(w, r, &template) {
		return
	}
	template.ID = id
	if err := s.deps.NotificationConfig.UpdateTemplate(r.Context(), template); err != nil {
		s.writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.reloadNotificationConfig(r); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "success", "id": id})
}

func (s *Server) handleNotificationTemplateDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := notificationConfigID(r)
	if !ok || s.deps.NotificationConfig == nil {
		s.writeError(w, r, http.StatusBadRequest, "模板 ID 无效")
		return
	}
	if err := s.deps.NotificationConfig.DeleteTemplate(r.Context(), id); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.reloadNotificationConfig(r); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "success", "id": id})
}

func (s *Server) handleNotificationDigest(w http.ResponseWriter, r *http.Request) {
	if s.deps.NotificationConfig == nil {
		s.writeError(w, r, http.StatusNotImplemented, "通知摘要未启用")
		return
	}
	digest, err := s.deps.NotificationConfig.GetDigest(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, digest)
}

func (s *Server) handleNotificationDigestUpdate(w http.ResponseWriter, r *http.Request) {
	if s.deps.NotificationConfig == nil {
		s.writeError(w, r, http.StatusNotImplemented, "通知摘要未启用")
		return
	}
	var digest repository.NotificationDigest
	if !s.decodeJSON(w, r, &digest) {
		return
	}
	if err := s.deps.NotificationConfig.UpdateDigest(r.Context(), digest); err != nil {
		s.writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "success", "digest": digest})
}

func notificationConfigID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func (s *Server) reloadNotificationConfig(r *http.Request) error {
	if s.deps.NotificationConfig == nil || s.deps.Notification == nil {
		return nil
	}
	rules, err := s.deps.NotificationConfig.ListRules(r.Context())
	if err != nil {
		return err
	}
	templates, err := s.deps.NotificationConfig.ListTemplates(r.Context())
	if err != nil {
		return err
	}
	s.deps.Notification.SetRules(rules)
	s.deps.Notification.SetTemplates(templates)
	return nil
}
