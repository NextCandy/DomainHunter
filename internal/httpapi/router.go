package httpapi

import "net/http"

// routes 注册全部路由。
//
// 兼容性约定：/api/domains、/api/domain/*、/api/stats、/api/settings/*、
// /api/monitor/*、/health 这些旧路径的语义与响应结构保持不变；
// 重构新增的能力放在 /api/v2/* 下，旧客户端不受影响。
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// ---- 前端 ----
	mux.Handle("/assets/", s.staticHandler())
	mux.HandleFunc("/login", s.handleLoginPage)
	mux.HandleFunc("/", s.handleIndex)

	// ---- 认证 ----
	mux.HandleFunc("POST /api/login", s.withRateLimit(s.loginLimiter, "登录尝试过于频繁，请5分钟后再试", s.handleLogin))
	mux.HandleFunc("POST /login", s.withRateLimit(s.loginLimiter, "登录尝试过于频繁，请5分钟后再试", s.handleLogin))
	mux.HandleFunc("/logout", s.handleLogout)
	mux.HandleFunc("GET /api/session", s.handleSession)
	mux.HandleFunc("GET /api/csrf", s.handleCSRFToken)

	// ---- 旧版域名 API ----
	mux.HandleFunc("/api/domains", s.withAuth(s.handleDomains))
	mux.HandleFunc("/api/domain/add", s.withAuth(s.handleDomainAdd))
	mux.HandleFunc("/api/domain/batch-add", s.withAuth(
		s.withRateLimit(s.batchLimiter, "批量添加过于频繁，请1分钟后再试", s.handleDomainBatchAdd)))
	mux.HandleFunc("/api/domain/check/", s.withAuth(s.handleDomainCheck))
	mux.HandleFunc("/api/domain/remove/", s.withAuth(s.handleDomainRemove))
	mux.HandleFunc("/api/domain/whois-raw/", s.withAuth(s.handleDomainWhoisRaw))
	mux.HandleFunc("/api/domain/", s.withAuth(s.handleDomainDetail))

	// ---- 状态与监控 ----
	mux.HandleFunc("/api/stats", s.withAuth(s.handleStats))
	mux.HandleFunc("/api/monitor/start", s.withAuth(s.handleMonitorStart))
	mux.HandleFunc("/api/monitor/stop", s.withAuth(s.handleMonitorStop))
	mux.HandleFunc("/api/monitor/reload", s.withAuth(s.handleMonitorReload))

	// ---- 设置 ----
	mux.HandleFunc("/api/settings", s.withAuth(s.handleGetSettings))
	mux.HandleFunc("/api/settings/smtp", s.withAuth(s.handleSMTPSettings))
	mux.HandleFunc("/api/settings/telegram", s.withAuth(s.handleTelegramSettings))
	mux.HandleFunc("/api/settings/monitor", s.withAuth(s.handleMonitorSettings))
	mux.HandleFunc("/api/change-password", s.withAuth(s.handleChangePassword))
	mux.HandleFunc("/api/update-username", s.withAuth(s.handleUpdateUsername))

	// ---- 通知 ----
	mux.HandleFunc("/api/notification/test", s.withAuth(s.handleNotificationTest))
	mux.HandleFunc("/api/test/email", s.withAuth(s.handleTestEmail))
	mux.HandleFunc("/api/test/telegram", s.withAuth(s.handleTestTelegram))

	// ---- 维护 ----
	mux.HandleFunc("/api/database/clean-orphaned", s.withAuth(s.handleCleanOrphaned))
	mux.HandleFunc("/api/check-update", s.handleCheckUpdate)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("GET /api/health/providers", s.handleProviderHealth)

	// ---- 历史（规格要求的路径）----
	mux.HandleFunc("GET /api/domains/{domain}/history", s.withAuth(s.handleDomainHistory))
	mux.HandleFunc("GET /api/domains/{domain}/attempts", s.withAuth(s.handleDomainAttempts))

	// ---- v2 ----
	mux.HandleFunc("GET /api/v2/overview", s.withAuth(s.handleOverview))
	mux.HandleFunc("GET /api/v2/meta", s.withAuth(s.handleMeta))
	mux.HandleFunc("GET /api/v2/domains", s.withAuth(s.handleDomainsV2))
	mux.HandleFunc("POST /api/v2/domains", s.withAuth(s.handleDomainAdd))
	mux.HandleFunc("POST /api/v2/domains/batch-add", s.withAuth(
		s.withRateLimit(s.batchLimiter, "批量添加过于频繁，请1分钟后再试", s.handleDomainBatchAdd)))
	mux.HandleFunc("POST /api/v2/domains/batch-delete", s.withAuth(s.handleDomainBatchDelete))
	mux.HandleFunc("POST /api/v2/domains/batch-check", s.withAuth(
		s.withRateLimit(s.batchLimiter, "批量检查过于频繁，请1分钟后再试", s.handleDomainBatchCheck)))
	mux.HandleFunc("GET /api/v2/domains/{domain}", s.withAuth(s.handleDomainDetailV2))
	mux.HandleFunc("PATCH /api/v2/domains/{domain}", s.withAuth(s.handleDomainPatch))
	mux.HandleFunc("DELETE /api/v2/domains/{domain}", s.withAuth(s.handleDomainDeleteV2))
	mux.HandleFunc("POST /api/v2/domains/{domain}/check", s.withAuth(s.handleDomainCheckV2))
	mux.HandleFunc("GET /api/v2/domains/{domain}/history", s.withAuth(s.handleDomainHistory))
	mux.HandleFunc("GET /api/v2/domains/{domain}/attempts", s.withAuth(s.handleDomainAttempts))
	mux.HandleFunc("GET /api/v2/observations", s.withAuth(s.handleRecentObservations))
	mux.HandleFunc("GET /api/v2/providers", s.withAuth(s.handleProviders))
	mux.HandleFunc("GET /api/v2/notifications", s.withAuth(s.handleNotificationHistory))
	mux.HandleFunc("GET /api/v2/settings", s.withAuth(s.handleSettingsV2))
	mux.HandleFunc("PUT /api/v2/settings/query-policy", s.withAuth(s.handleQueryPolicy))
	mux.HandleFunc("PUT /api/v2/settings/history", s.withAuth(s.handleHistorySettings))
	mux.HandleFunc("PUT /api/v2/settings/log-level", s.withAuth(s.handleLogLevel))
	mux.HandleFunc("GET /api/v2/backups", s.withAuth(s.handleBackups))
	mux.HandleFunc("POST /api/v2/backups", s.withAuth(s.handleCreateBackup))

	return securityHeaders(s.withGlobalRateLimit(mux))
}

// withGlobalRateLimit 给所有 API 请求加一层宽松的整体限流
func (s *Server) withGlobalRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			s.applyCORS(w, r)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
			if !s.generalLimiter.Allow(clientIP(r)) {
				s.writeError(w, r, http.StatusTooManyRequests, "请求过于频繁，请稍后再试")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
