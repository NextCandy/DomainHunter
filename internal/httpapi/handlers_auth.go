package httpapi

import (
	"net/http"
	"strings"

	"DomainHunter/internal/logger"
)

// handleLogin 处理登录。同时接受表单和 JSON 提交，兼容旧前端。
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var (
		username string
		password string
		remember = true
	)

	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Remember *bool  `json:"remember"`
		}
		if !s.decodeJSON(w, r, &req) {
			return
		}
		username, password = req.Username, req.Password
		if req.Remember != nil {
			remember = *req.Remember
		}
	} else {
		if err := r.ParseForm(); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "无法解析表单")
			return
		}
		username = r.FormValue("username")
		password = r.FormValue("password")
		if v := r.FormValue("remember"); v != "" {
			remember = v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "on")
		}
	}

	session, err := s.deps.Auth.Login(r.Context(), username, password)
	if err != nil {
		s.log.Warn(logger.Fields{"ip": clientIP(r)}, "登录失败")
		s.writeJSON(w, r, http.StatusUnauthorized, map[string]string{"error": "用户名或密码错误"})
		return
	}

	s.deps.Auth.Sessions().SetInfo(session.ID, r.UserAgent(), clientIP(r))
	s.setSessionCookies(w, r, session, remember)
	s.log.Info(logger.Fields{"ip": clientIP(r)}, "登录成功")

	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"status":     "success",
		"username":   s.deps.Auth.Username(),
		"csrf_token": session.CSRFToken,
	})
}

// handleLogout 注销。
//
// 除了清 Cookie，还会撤销"记住登录"令牌 —— 否则客户端只要留着那个 cookie，
// 退出登录后依然能免密码进来。代价是其他设备上的免登录状态也会失效。
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		s.deps.Auth.Logout(cookie.Value)
	}
	if err := s.deps.Auth.RevokeRememberTokens(r.Context()); err != nil {
		s.log.Warn(logger.Fields{"error": err.Error()}, "撤销记住登录令牌失败")
	}
	s.clearSessionCookies(w, r)

	if strings.HasPrefix(r.Header.Get("Accept"), "application/json") || r.Method == http.MethodPost {
		s.writeJSON(w, r, http.StatusOK, map[string]string{"status": "success"})
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// handleSession 返回当前登录状态，前端据此决定显示登录页还是主界面
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if !s.deps.Auth.RequireAuth() {
		s.writeJSON(w, r, http.StatusOK, map[string]any{
			"authenticated": true,
			"auth_required": false,
			"username":      s.deps.Auth.Username(),
		})
		return
	}

	session := s.currentSession(r)
	if session == nil {
		if cookie, err := r.Cookie(rememberCookie); err == nil &&
			s.deps.Auth.ValidateRememberToken(cookie.Value) {
			session = s.deps.Auth.CreateSession()
			s.setSessionCookies(w, r, session, false)
		}
	}
	if session == nil {
		s.writeJSON(w, r, http.StatusOK, map[string]any{
			"authenticated": false,
			"auth_required": true,
		})
		return
	}

	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"authenticated":   true,
		"auth_required":   true,
		"username":        s.deps.Auth.Username(),
		"csrf_token":      session.CSRFToken,
		"password_hashed": !s.deps.Auth.UsesLegacyPassword(),
		"version":         s.deps.Version,
	})
}

// handleCSRFToken 单独下发 CSRF 令牌
func (s *Server) handleCSRFToken(w http.ResponseWriter, r *http.Request) {
	session := s.currentSession(r)
	if session == nil {
		s.writeError(w, r, http.StatusUnauthorized, "Unauthorized")
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"csrf_token": session.CSRFToken})
}

// handleChangePassword 修改密码
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
		return
	}

	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if !s.deps.Auth.VerifyPassword(r.Context(), req.CurrentPassword) {
		s.writeError(w, r, http.StatusBadRequest, "当前密码错误")
		return
	}
	if len(req.NewPassword) < 6 {
		s.writeError(w, r, http.StatusBadRequest, "新密码长度至少6位")
		return
	}
	if err := s.deps.Auth.UpdatePassword(r.Context(), req.NewPassword); err != nil {
		s.log.Error(logger.Fields{"error": err.Error()}, "更新密码失败")
		s.writeError(w, r, http.StatusInternalServerError, "更新密码失败")
		return
	}

	s.clearSessionCookies(w, r)
	s.writeJSON(w, r, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "密码修改成功，请重新登录",
	})
}

// handleUpdateUsername 修改用户名
func (s *Server) handleUpdateUsername(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
		return
	}

	var req struct {
		Username string `json:"username"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if len(strings.TrimSpace(req.Username)) < 3 {
		s.writeError(w, r, http.StatusBadRequest, "用户名长度至少3位")
		return
	}
	if err := s.deps.Auth.UpdateUsername(r.Context(), req.Username); err != nil {
		s.log.Error(logger.Fields{"error": err.Error()}, "更新用户名失败")
		s.writeError(w, r, http.StatusInternalServerError, "更新用户名失败")
		return
	}
	s.deps.Settings.SyncCredentials(s.deps.Auth.Username())

	s.writeJSON(w, r, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "用户名更新成功",
	})
}
