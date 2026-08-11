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
	// /logout 供旧前端的链接跳转使用，/api/logout 供新前端的 fetch 使用
	mux.HandleFunc("/logout", s.handleLogout)
	mux.HandleFunc("/api/logout", s.handleLogout)
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
	mux.HandleFunc("POST /api/settings/bark", s.withAuth(s.handleBarkSettings))
	mux.HandleFunc("PUT /api/settings/bark", s.withAuth(s.handleBarkSettings))
	mux.HandleFunc("POST /api/settings/feishu", s.withAuth(s.handleFeishuSettings))
	mux.HandleFunc("PUT /api/settings/feishu", s.withAuth(s.handleFeishuSettings))
	mux.HandleFunc("POST /api/settings/webhook", s.withAuth(s.handleWebhookSettings))
	mux.HandleFunc("PUT /api/settings/webhook", s.withAuth(s.handleWebhookSettings))
	mux.HandleFunc("POST /api/v2/notifications/test/{channel}", s.withAuthScope("write", s.handleChannelTest))

	// ---- 维护 ----
	mux.HandleFunc("/api/database/clean-orphaned", s.withAuth(s.handleCleanOrphaned))
	mux.HandleFunc("/api/check-update", s.handleCheckUpdate)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("GET /api/health/providers", s.handleProviderHealth)

	// ---- 历史（规格要求的路径）----
	mux.HandleFunc("GET /api/domains/{domain}/history", s.withAuth(s.handleDomainHistory))
	mux.HandleFunc("GET /api/domains/{domain}/attempts", s.withAuth(s.handleDomainAttempts))
	mux.HandleFunc("GET /metrics", s.handleMetrics)

	// ---- v2 ----
	mux.HandleFunc("GET /api/v2/overview", s.withAuthScope("read", s.handleOverview))
	mux.HandleFunc("GET /api/v2/overview/trend", s.withAuthScope("read", s.handleOverviewTrend))
	mux.HandleFunc("GET /api/v2/meta", s.withAuthScope("read", s.handleMeta))
	mux.HandleFunc("GET /api/v2/facets", s.withAuthScope("read", s.handleFacets))
	mux.HandleFunc("GET /api/v2/domains", s.withAuthScope("read", s.handleDomainsV2))
	mux.HandleFunc("POST /api/v2/domains", s.withAuthScope("write", s.handleDomainAdd))
	mux.HandleFunc("POST /api/v2/domains/batch-add", s.withAuthScope("write",
		s.withRateLimit(s.batchLimiter, "批量添加过于频繁，请1分钟后再试", s.handleDomainBatchAdd)))
	mux.HandleFunc("POST /api/v2/domains/batch-delete", s.withAuthScope("write", s.handleDomainBatchDelete))
	mux.HandleFunc("POST /api/v2/domains/batch-check", s.withAuthScope("write",
		s.withRateLimit(s.batchLimiter, "批量检查过于频繁，请1分钟后再试", s.handleDomainBatchCheck)))
	mux.HandleFunc("POST /api/v2/domains/batch-move-folder", s.withAuthScope("write", s.handleDomainBatchMoveFolder))
	mux.HandleFunc("POST /api/v2/domains/batch-retry-failed", s.withAuthScope("write", s.handleDomainBatchRetryFailed))
	mux.HandleFunc("GET /api/v2/domains/export", s.withAuthScope("read", s.handleDomainExport))
	mux.HandleFunc("POST /api/v2/domains/import", s.withAuthScope("write", s.handleDomainImport))
	mux.HandleFunc("GET /api/v2/domains/{domain}", s.withAuthScope("read", s.handleDomainDetailV2))
	mux.HandleFunc("PATCH /api/v2/domains/{domain}", s.withAuthScope("write", s.handleDomainPatch))
	mux.HandleFunc("DELETE /api/v2/domains/{domain}", s.withAuthScope("write", s.handleDomainDeleteV2))
	mux.HandleFunc("POST /api/v2/domains/{domain}/check", s.withAuthScope("write", s.handleDomainCheckV2))
	mux.HandleFunc("GET /api/v2/domains/{domain}/history", s.withAuthScope("read", s.handleDomainHistory))
	mux.HandleFunc("GET /api/v2/domains/{domain}/history/export", s.withAuthScope("read", s.handleDomainHistoryExport))
	mux.HandleFunc("GET /api/v2/domains/{domain}/attempts", s.withAuthScope("read", s.handleDomainAttempts))

	// ---- folders ----
	mux.HandleFunc("GET /api/v2/folders", s.withAuthScope("read", s.handleFolders))
	mux.HandleFunc("POST /api/v2/folders", s.withAuthScope("write", s.handleFolderCreate))
	mux.HandleFunc("GET /api/v2/folders/{id}", s.withAuthScope("read", s.handleFolderGet))
	mux.HandleFunc("PUT /api/v2/folders/{id}", s.withAuthScope("write", s.handleFolderUpdate))
	mux.HandleFunc("PATCH /api/v2/folders/{id}", s.withAuthScope("write", s.handleFolderUpdate))
	mux.HandleFunc("DELETE /api/v2/folders/{id}", s.withAuthScope("write", s.handleFolderDelete))
	mux.HandleFunc("GET /api/v2/observations", s.withAuthScope("read", s.handleRecentObservations))
	mux.HandleFunc("GET /api/v2/providers", s.withAuthScope("read", s.handleProviders))
	mux.HandleFunc("GET /api/v2/notifications", s.withAuthScope("read", s.handleNotificationHistory))
	mux.HandleFunc("GET /api/v2/settings", s.withAuthScope("read", s.handleSettingsV2))
	mux.HandleFunc("PUT /api/v2/settings/query-policy", s.withAuthScope("write", s.handleQueryPolicy))
	mux.HandleFunc("PUT /api/v2/settings/history", s.withAuthScope("write", s.handleHistorySettings))
	mux.HandleFunc("PUT /api/v2/settings/log-level", s.withAuthScope("write", s.handleLogLevel))
	mux.HandleFunc("GET /api/v2/backups", s.withAuthScope("read", s.handleBackups))
	mux.HandleFunc("POST /api/v2/backups", s.withAuthScope("write", s.handleCreateBackup))

	// ---- P1：高级筛选、批量预览、AI 研究估价与自动化 ----
	mux.HandleFunc("GET /api/v2/saved-views", s.withAuthScope("read", s.handleSavedViews))
	mux.HandleFunc("POST /api/v2/saved-views", s.withAuthScope("write", s.handleSavedViews))
	mux.HandleFunc("PUT /api/v2/saved-views/{id}", s.withAuthScope("write", s.handleSavedView))
	mux.HandleFunc("PATCH /api/v2/saved-views/{id}", s.withAuthScope("write", s.handleSavedView))
	mux.HandleFunc("DELETE /api/v2/saved-views/{id}", s.withAuthScope("write", s.handleSavedView))
	mux.HandleFunc("POST /api/v2/bulk-actions/preview", s.withAuthScope("write", s.handleBulkPreview))
	mux.HandleFunc("POST /api/v2/bulk-actions", s.withAuthScope("write", s.handleBulkExecute))
	mux.HandleFunc("GET /api/v2/bulk-actions/audits", s.withAuthScope("read", s.handleBulkAudits))
	mux.HandleFunc("GET /api/v2/ai/settings", s.withAuthScope("read", s.handleAISettings))
	mux.HandleFunc("PUT /api/v2/ai/settings", s.withAuthScope("write", s.handleAISettings))
	mux.HandleFunc("PATCH /api/v2/ai/settings", s.withAuthScope("write", s.handleAISettings))
	mux.HandleFunc("GET /api/v2/ai/models", s.withAuthScope("read", s.handleAIModels))
	mux.HandleFunc("GET /api/v2/ai/usage", s.withAuthScope("read", s.handleAIUsage))
	mux.HandleFunc("GET /api/v2/ai/jobs", s.withAuthScope("read", s.handleAIJobs))
	mux.HandleFunc("POST /api/v2/ai/jobs", s.withAuthScope("write", s.handleAIJobs))
	mux.HandleFunc("POST /api/v2/ai/jobs/{id}/cancel", s.withAuthScope("write", s.handleAICancelJob))
	mux.HandleFunc("GET /api/v2/ai/valuations/{domain}", s.withAuthScope("read", s.handleAIValuation))
	mux.HandleFunc("GET /api/v2/automation/rules", s.withAuthScope("read", s.handleAutomationRules))
	mux.HandleFunc("POST /api/v2/automation/rules", s.withAuthScope("write", s.handleAutomationRules))
	mux.HandleFunc("PUT /api/v2/automation/rules/{id}", s.withAuthScope("write", s.handleAutomationRule))
	mux.HandleFunc("PATCH /api/v2/automation/rules/{id}", s.withAuthScope("write", s.handleAutomationRule))
	mux.HandleFunc("DELETE /api/v2/automation/rules/{id}", s.withAuthScope("write", s.handleAutomationRule))
	mux.HandleFunc("POST /api/v2/automation/rules/{id}/dry-run", s.withAuthScope("write", s.handleAutomationDryRun))
	mux.HandleFunc("GET /api/v2/automation/runs", s.withAuthScope("read", s.handleAutomationRuns))
	mux.HandleFunc("POST /api/v2/automation/evaluate", s.withAuthScope("write", s.handleAutomationEvaluate))

	// ---- API tokens ----
	mux.HandleFunc("GET /api/v2/tokens", s.withAuthScope("read", s.handleTokens))
	mux.HandleFunc("POST /api/v2/tokens", s.withAuthScope("write", s.handleTokenCreate))
	mux.HandleFunc("DELETE /api/v2/tokens/{id}", s.withAuthScope("write", s.handleTokenRevoke))

	// ---- notification rules/templates/digest ----
	for _, prefix := range []string{"/api/v2/notifications/rules", "/api/v2/notification-rules", "/api/v2/settings/notification-rules"} {
		mux.HandleFunc("GET "+prefix, s.withAuthScope("read", s.handleNotificationRules))
		mux.HandleFunc("POST "+prefix, s.withAuthScope("write", s.handleNotificationRuleCreate))
		mux.HandleFunc("PUT "+prefix+"/{id}", s.withAuthScope("write", s.handleNotificationRuleUpdate))
		mux.HandleFunc("DELETE "+prefix+"/{id}", s.withAuthScope("write", s.handleNotificationRuleDelete))
	}
	for _, prefix := range []string{"/api/v2/notifications/templates", "/api/v2/notification-templates", "/api/v2/settings/notification-templates"} {
		mux.HandleFunc("GET "+prefix, s.withAuthScope("read", s.handleNotificationTemplates))
		mux.HandleFunc("POST "+prefix, s.withAuthScope("write", s.handleNotificationTemplateCreate))
		mux.HandleFunc("PUT "+prefix+"/{id}", s.withAuthScope("write", s.handleNotificationTemplateUpdate))
		mux.HandleFunc("DELETE "+prefix+"/{id}", s.withAuthScope("write", s.handleNotificationTemplateDelete))
	}
	mux.HandleFunc("GET /api/v2/notifications/digest", s.withAuthScope("read", s.handleNotificationDigest))
	mux.HandleFunc("PUT /api/v2/notifications/digest", s.withAuthScope("write", s.handleNotificationDigestUpdate))
	mux.HandleFunc("GET /api/v2/settings/notification-digest", s.withAuthScope("read", s.handleNotificationDigest))
	mux.HandleFunc("PUT /api/v2/settings/notification-digest", s.withAuthScope("write", s.handleNotificationDigestUpdate))

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
