package httpapi

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"time"

	"DomainHunter/internal/auth"
	"DomainHunter/internal/config"
	"DomainHunter/internal/repository"
)

const (
	sessionCookie  = "session_id"
	rememberCookie = "remember_token"
	csrfCookie     = "csrf_token"
	csrfHeader     = "X-CSRF-Token"
)

type apiTokenContextKey struct{}

// withAuth 校验会话或 Bearer token。
func (s *Server) withAuth(handler http.HandlerFunc) http.HandlerFunc {
	return s.withAuthScope("", handler)
}

// withAuthScope 在 API token 请求上额外校验 scope；会话请求不受 scope 限制。
func (s *Server) withAuthScope(required string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if raw, ok := bearerFromRequest(r); ok {
			if s.deps.Tokens == nil {
				s.writeError(w, r, http.StatusUnauthorized, "Bearer token 未启用")
				return
			}
			hash := sha256.Sum256([]byte(raw))
			token, err := s.deps.Tokens.Validate(r.Context(), fmt.Sprintf("%x", hash[:]))
			if err != nil || token == nil {
				s.writeError(w, r, http.StatusUnauthorized, "无效的 Bearer token")
				return
			}
			if required != "" && !scopeAllowed(token.Scopes, required) {
				s.writeError(w, r, http.StatusForbidden, "Bearer token 缺少所需 scope")
				return
			}
			ctx := context.WithValue(r.Context(), apiTokenContextKey{}, token)
			handler(w, r.WithContext(ctx))
			return
		}
		if !s.deps.Auth.RequireAuth() {
			handler(w, r)
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
			s.writeError(w, r, http.StatusUnauthorized, "Unauthorized")
			return
		}
		if !s.checkCSRF(r, session) {
			s.writeError(w, r, http.StatusForbidden, "CSRF 校验失败，请刷新页面后重试")
			return
		}
		handler(w, r)
	}
}

func bearerFromRequest(r *http.Request) (string, bool) {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

func scopeAllowed(scopes []string, required string) bool {
	for _, scope := range scopes {
		scope = strings.ToLower(strings.TrimSpace(scope))
		if scope == "*" || scope == strings.ToLower(required) {
			return true
		}
		if required == "read" && (scope == "read:*" || strings.HasSuffix(scope, ":read")) {
			return true
		}
		if required == "write" && (scope == "write:*" || strings.HasSuffix(scope, ":write")) {
			return true
		}
	}
	return false
}

// apiTokenFromContext 返回当前 Bearer token，仅供审计/测试使用。
func apiTokenFromContext(ctx context.Context) *repository.APIToken {
	token, _ := ctx.Value(apiTokenContextKey{}).(*repository.APIToken)
	return token
}

// withRateLimit 通用限流
func (s *Server) withRateLimit(limiter *RateLimiter, message string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow(clientIP(r)) {
			s.writeError(w, r, http.StatusTooManyRequests, message)
			return
		}
		handler(w, r)
	}
}

func (s *Server) currentSession(r *http.Request) *auth.Session {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	session, err := s.deps.Auth.ValidateSession(cookie.Value)
	if err != nil {
		return nil
	}
	return session
}

// checkCSRF 对写操作做跨站请求防护。
//
// 判定顺序：
//  1. Origin 存在 → 必须与 Host 同源（或在允许的 CORS 白名单内）
//  2. Sec-Fetch-Site 存在 → 必须是 same-origin / none
//  3. 都不存在（curl 等非浏览器客户端）→ 需要 X-CSRF-Token 与 Cookie 匹配；
//     若连 Referer 也没有，则视为非浏览器上下文放行
func (s *Server) checkCSRF(r *http.Request, session *auth.Session) bool {
	cfg := s.config()
	if !cfg.Security.CSRFEnabled {
		return true
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}

	if token := r.Header.Get(csrfHeader); token != "" && session != nil && token == session.CSRFToken {
		return true
	}

	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		return s.originAllowed(origin, r)
	}
	if site := strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")); site != "" {
		return site == "same-origin" || site == "none"
	}
	// 现代浏览器一定会带 Origin 或 Sec-Fetch-Site；两者都没有说明不是浏览器
	// 发起的跨站请求。此时只要没有 Referer 就放行，保证 curl / 脚本可用。
	return strings.TrimSpace(r.Header.Get("Referer")) == ""
}

func (s *Server) originAllowed(origin string, r *http.Request) bool {
	if sameOrigin(origin, r) {
		return true
	}
	for _, allowed := range s.config().Security.CORSOrigins {
		if strings.EqualFold(strings.TrimRight(allowed, "/"), strings.TrimRight(origin, "/")) {
			return true
		}
	}
	return false
}

func sameOrigin(origin string, r *http.Request) bool {
	origin = strings.TrimRight(origin, "/")
	host := r.Host
	for _, scheme := range []string{"https://", "http://"} {
		if strings.EqualFold(origin, scheme+host) {
			return true
		}
	}
	return false
}

// applyCORS 只在显式配置了允许来源时输出 CORS 头。
// 默认不再对所有 API 输出 Access-Control-Allow-Origin: *。
func (s *Server) applyCORS(w http.ResponseWriter, r *http.Request) {
	origins := s.config().Security.CORSOrigins
	if len(origins) == 0 {
		return
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return
	}
	for _, allowed := range origins {
		if allowed == "*" || strings.EqualFold(strings.TrimRight(allowed, "/"), strings.TrimRight(origin, "/")) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, "+csrfHeader)
			return
		}
	}
}

// cookieSecure 判断是否给 Cookie 打 Secure 标记。
// auto 模式下同时考虑反向代理透传的 X-Forwarded-Proto，
// 避免"应用内部是 HTTP、外部其实是 HTTPS"时误判。
func cookieSecure(cfg *config.Config, r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(cfg.Security.CookieSecure)) {
	case "true":
		return true
	case "false":
		return false
	}
	if r.TLS != nil {
		return true
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return strings.EqualFold(strings.TrimSpace(strings.Split(proto, ",")[0]), "https")
	}
	if r.Header.Get("X-Forwarded-Ssl") == "on" || r.Header.Get("Front-End-Https") == "on" {
		return true
	}
	return false
}

func sameSiteMode(cfg *config.Config) http.SameSite {
	switch strings.ToLower(strings.TrimSpace(cfg.Security.CookieSameSite)) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}

// setSessionCookies 下发会话、CSRF 与（可选的）记住登录 Cookie
func (s *Server) setSessionCookies(w http.ResponseWriter, r *http.Request, session *auth.Session, remember bool) {
	cfg := s.config()
	secure := cookieSecure(cfg, r)
	sameSite := sameSiteMode(cfg)
	if sameSite == http.SameSiteNoneMode && !secure {
		// SameSite=None 必须配合 Secure，否则浏览器会直接丢弃 Cookie
		sameSite = http.SameSiteLaxMode
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    session.ID,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
		Expires:  session.ExpiresAt,
	})
	// CSRF 令牌需要被前端 JS 读取，因此不能 HttpOnly。
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookie,
		Value:    session.CSRFToken,
		Path:     "/",
		HttpOnly: false,
		Secure:   secure,
		SameSite: sameSite,
		Expires:  session.ExpiresAt,
	})

	if remember {
		if token := s.deps.Auth.GenerateRememberToken(); token != "" {
			http.SetCookie(w, &http.Cookie{
				Name:     rememberCookie,
				Value:    token,
				Path:     "/",
				HttpOnly: true,
				Secure:   secure,
				SameSite: sameSite,
				Expires:  time.Now().Add(s.deps.Auth.RememberDuration()),
			})
		}
	}
}

func (s *Server) clearSessionCookies(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	secure := cookieSecure(cfg, r)
	sameSite := sameSiteMode(cfg)
	for _, name := range []string{sessionCookie, rememberCookie, csrfCookie} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			HttpOnly: name != csrfCookie,
			Secure:   secure,
			SameSite: sameSite,
			MaxAge:   -1,
			Expires:  time.Unix(0, 0),
		})
	}
}

// securityHeaders 输出基础安全响应头
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}
