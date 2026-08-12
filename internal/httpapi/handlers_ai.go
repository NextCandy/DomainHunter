package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	ai "DomainHunter/internal/ai"
)

// handleAICancelJobCompat keeps the historical numeric P1 endpoint and the
// strict string-ID valuation endpoint on one ServeMux pattern.
func (s *Server) handleAICancelJobCompat(w http.ResponseWriter, r *http.Request) {
	if id, err := strconv.ParseInt(r.PathValue("id"), 10, 64); err == nil && id > 0 {
		s.handleAICancelJob(w, r)
		return
	}
	s.handleAIJobCancel(w, r)
}

func (s *Server) aiService(w http.ResponseWriter, r *http.Request) *ai.Service {
	if s.deps.AI == nil {
		s.writeError(w, r, http.StatusNotImplemented, "AI 估价功能尚未启用")
		return nil
	}
	return s.deps.AI
}

func (s *Server) handleAIValuationPolicy(w http.ResponseWriter, r *http.Request) {
	service := s.aiService(w, r)
	if service == nil {
		return
	}
	s.writeJSON(w, r, http.StatusOK, service.Policy())
}

func (s *Server) handleAIProfiles(w http.ResponseWriter, r *http.Request) {
	service := s.aiService(w, r)
	if service == nil {
		return
	}
	profiles, err := service.ListProfiles(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "读取 AI 档案失败")
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"profiles": profiles})
}

func (s *Server) handleAIProfileCreate(w http.ResponseWriter, r *http.Request) {
	service := s.aiService(w, r)
	if service == nil {
		return
	}
	var input ai.ProfileInput
	if !s.decodeJSON(w, r, &input) {
		return
	}
	profile, err := service.SaveProfile(r.Context(), "", input, "authenticated")
	if err != nil {
		s.writeAIError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, profile)
}

func (s *Server) handleAIProfileUpdate(w http.ResponseWriter, r *http.Request) {
	service := s.aiService(w, r)
	if service == nil {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		s.writeError(w, r, http.StatusBadRequest, "AI 档案 ID 无效")
		return
	}
	var input ai.ProfileInput
	if !s.decodeJSON(w, r, &input) {
		return
	}
	profile, err := service.SaveProfile(r.Context(), id, input, "authenticated")
	if err != nil {
		s.writeAIError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, profile)
}

func (s *Server) handleAIProfileDelete(w http.ResponseWriter, r *http.Request) {
	service := s.aiService(w, r)
	if service == nil {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		s.writeError(w, r, http.StatusBadRequest, "AI 档案 ID 无效")
		return
	}
	if err := service.DeleteProfile(r.Context(), id, "authenticated"); err != nil {
		s.writeAIError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleAIProfileTestConnection(w http.ResponseWriter, r *http.Request) {
	service := s.aiService(w, r)
	if service == nil {
		return
	}
	var input ai.ProfileInput
	if !s.decodeJSON(w, r, &input) {
		return
	}
	result, err := service.TestConnection(r.Context(), input)
	if err != nil {
		s.writeAIError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, result)
}

func (s *Server) handleDomainValuationGet(w http.ResponseWriter, r *http.Request) {
	service := s.aiService(w, r)
	if service == nil {
		return
	}
	job, err := service.GetCurrent(r.Context(), r.PathValue("domain"))
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "读取估价任务失败")
		return
	}
	if job == nil {
		s.writeError(w, r, http.StatusNotFound, "尚无估价任务")
		return
	}
	s.writeJSON(w, r, http.StatusOK, job)
}

func (s *Server) handleDomainValuationEnqueue(w http.ResponseWriter, r *http.Request) {
	service := s.aiService(w, r)
	if service == nil {
		return
	}
	var input ai.EnqueueInput
	if !s.decodeJSON(w, r, &input) {
		return
	}
	job, err := service.Enqueue(r.Context(), r.PathValue("domain"), input, "authenticated")
	if err != nil {
		s.writeAIError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusAccepted, job)
}

func (s *Server) handleAIJobGet(w http.ResponseWriter, r *http.Request) {
	service := s.aiService(w, r)
	if service == nil {
		return
	}
	job, err := service.GetJob(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "读取 AI 任务失败")
		return
	}
	if job == nil {
		s.writeError(w, r, http.StatusNotFound, "AI 任务不存在")
		return
	}
	s.writeJSON(w, r, http.StatusOK, job)
}

func (s *Server) handleAIJobCancel(w http.ResponseWriter, r *http.Request) {
	service := s.aiService(w, r)
	if service == nil {
		return
	}
	job, err := service.Cancel(r.Context(), r.PathValue("id"), "authenticated")
	if err != nil {
		s.writeAIError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, job)
}

func (s *Server) writeAIError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ai.ErrProfileNotFound):
		s.writeError(w, r, http.StatusNotFound, "AI 档案不存在")
	case errors.Is(err, ai.ErrProfileDisabled):
		s.writeError(w, r, http.StatusConflict, "AI 档案未启用")
	case errors.Is(err, ai.ErrActiveJobDuplicate):
		s.writeError(w, r, http.StatusConflict, "相同输入的 AI 估价任务已在队列中，请等待当前任务完成")
	case errors.Is(err, ai.ErrJobNotCancellable):
		s.writeError(w, r, http.StatusConflict, "任务已开始或不存在，不能取消")
	case errors.Is(err, ai.ErrIneligible):
		s.writeError(w, r, http.StatusUnprocessableEntity, "当前域名状态或证据不足，暂不能加入 AI 估价")
	case errors.Is(err, ai.ErrQuotaExceeded):
		s.writeError(w, r, http.StatusTooManyRequests, "今日估价额度已用完，请稍后重试或调整额度")
	case errors.Is(err, ai.ErrProviderAuth):
		s.writeError(w, r, http.StatusUnprocessableEntity, "默认 AI 的 API Key 无效或已过期，请在 AI 与自动化中更新 Key")
	case errors.Is(err, ai.ErrProviderConfig):
		s.writeError(w, r, http.StatusUnprocessableEntity, "默认 AI 模型或接口地址无效，请在 AI 与自动化中检查配置")
	case errors.Is(err, ai.ErrSecretKeyRequired):
		s.writeError(w, r, http.StatusUnprocessableEntity, "未配置可用的 AI API Key 或应用级加密主密钥")
	case errors.Is(err, ai.ErrUnsafeBaseURL):
		s.writeError(w, r, http.StatusBadRequest, "AI Base URL 不符合出站安全策略")
	default:
		s.writeError(w, r, http.StatusBadRequest, safeAIHandlerError(err))
	}
}

func safeAIHandlerError(err error) string {
	if err == nil {
		return "AI 请求失败"
	}
	message := strings.TrimSpace(err.Error())
	if len([]rune(message)) > 180 {
		message = string([]rune(message)[:180])
	}
	return message
}
