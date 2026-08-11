package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) handleTokens(w http.ResponseWriter, r *http.Request) {
	if s.deps.Tokens == nil {
		s.writeError(w, r, http.StatusNotImplemented, "API token 未启用")
		return
	}
	tokens, err := s.deps.Tokens.List(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"tokens": tokens})
}

func (s *Server) handleTokenCreate(w http.ResponseWriter, r *http.Request) {
	if s.deps.Tokens == nil {
		s.writeError(w, r, http.StatusNotImplemented, "API token 未启用")
		return
	}
	var req struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, r, http.StatusBadRequest, "token 名称不能为空")
		return
	}
	scopes := normalizeScopes(req.Scopes)
	if len(scopes) == 0 {
		scopes = []string{"read", "write"}
	}
	raw, err := randomAPIToken()
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "生成 token 失败")
		return
	}
	hash := sha256.Sum256([]byte(raw))
	token, err := s.deps.Tokens.Create(r.Context(), req.Name, scopes, hex.EncodeToString(hash[:]))
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	token.RawToken = raw
	s.writeJSON(w, r, http.StatusCreated, token)
}

func (s *Server) handleTokenRevoke(w http.ResponseWriter, r *http.Request) {
	if s.deps.Tokens == nil {
		s.writeError(w, r, http.StatusNotImplemented, "API token 未启用")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.writeError(w, r, http.StatusBadRequest, "token ID 无效")
		return
	}
	if err := s.deps.Tokens.Revoke(r.Context(), id); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "success", "id": id})
}

func randomAPIToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "dh_" + hex.EncodeToString(buf), nil
}

func normalizeScopes(scopes []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.ToLower(strings.TrimSpace(scope))
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	return out
}
