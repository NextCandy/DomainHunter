package httpapi

import (
	"net/http"
)

func (s *Server) handleDomainBatchRetryFailed(w http.ResponseWriter, r *http.Request) {
	queued, err := s.deps.Domains.RetryFailed(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"status": "success", "queued": queued,
		"window_seconds": 30,
		"message":        "失败和未知域名已均摊到30秒窗口",
	})
}
