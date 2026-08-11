package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"DomainHunter/internal/logger"
	"DomainHunter/internal/registry"
)

// handleStats 旧版统计接口
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"monitor":      s.deps.Monitor.Stats(r.Context(), s.deps.Domains),
		"auth":         s.deps.Auth.Stats(),
		"notification": s.deps.Notification.Stats(),
		"timestamp":    time.Now(),
	})
}

// handleOverview 概览页数据
func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	overview, err := s.deps.Overview.Build(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, overview)
}

// handleOverviewTrend 返回按自然日聚合的历史观测趋势。
func (s *Server) handleOverviewTrend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
		return
	}

	days := 7
	if raw := r.URL.Query().Get("days"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 366 {
			s.writeError(w, r, http.StatusBadRequest, "days 必须是 1 到 366 之间的整数")
			return
		}
		days = parsed
	}

	trend, err := s.deps.Overview.Trend(r.Context(), days)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, trend)
}

// handleMonitorStart 启动监控
func (s *Server) handleMonitorStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
		return
	}
	if err := s.deps.Monitor.Restart(r.Context()); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"status": "started"})
}

// handleMonitorStop 停止监控
func (s *Server) handleMonitorStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
		return
	}
	s.deps.Monitor.Stop()
	s.writeJSON(w, r, http.StatusOK, map[string]string{"status": "stopped"})
}

// handleMonitorReload 重新加载监控。
//
// 调度器直接以数据库为准，不再需要"重新加载域名列表"这一步；
// 这里保留接口语义：刷新 TLD/RDAP 映射并确保调度器在运行。
func (s *Server) handleMonitorReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
		return
	}
	if err := registry.Reload(); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "刷新查询源配置失败: "+err.Error())
		return
	}
	if err := s.deps.Monitor.Restart(r.Context()); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"status": "reloaded"})
}

// handleCleanOrphaned 清理孤立数据
func (s *Server) handleCleanOrphaned(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
		return
	}
	results, notifications, err := s.deps.Domains.CleanOrphaned(r.Context())
	if err != nil {
		s.log.Error(logger.Fields{"error": err.Error()}, "清理孤立数据失败")
		s.writeError(w, r, http.StatusInternalServerError, "清理失败: "+err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"status":               "success",
		"message":              "孤立数据清理完成",
		"results_deleted":      results,
		"notifications_delete": notifications,
	})
}

// handleProviders 查询源健康状态
func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"providers":   s.deps.Engine.Health().Snapshot(s.deps.Engine.Providers().Names()),
		"policy":      s.deps.Engine.Policy().Config(),
		"rate_limits": s.deps.Engine.Limiter().Rules(),
		"bootstrap":   registry.Bootstrap(),
	})
}

// handleProviderHealth /api/health/providers
func (s *Server) handleProviderHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"providers": s.deps.Engine.Health().Snapshot(s.deps.Engine.Providers().Names()),
	})
}

// handleBackups 列出数据库备份
func (s *Server) handleBackups(w http.ResponseWriter, r *http.Request) {
	backups, err := s.deps.DB.ListBackups()
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"backups": backups})
}

// handleCreateBackup 手动生成一份数据库备份
func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	path, err := s.deps.DB.Backup("manual")
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "备份失败: "+err.Error())
		return
	}
	if err := s.deps.DB.PruneBackups(5); err != nil {
		s.log.Warn(logger.Fields{"error": err.Error()}, "清理旧备份失败")
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"status": "success", "backup": path})
}

// handleHealth 健康检查。
//
// 兼容性：顶层 status / timestamp / monitor / domains / checks 字段保持不变，
// 新增 database、scheduler、providers 等子项。
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	checks := map[string]any{}
	status := "ok"

	dbStatus, dbError := "ok", ""
	if err := s.deps.DB.PingContext(r.Context()); err != nil {
		dbStatus, dbError, status = "error", err.Error(), "degraded"
	}
	checks["database"] = map[string]any{"status": dbStatus, "error": dbError}

	configStatus, configError := "ok", ""
	if err := registry.Load(); err != nil {
		configStatus, configError, status = "error", err.Error(), "degraded"
	}
	checks["config"] = map[string]any{"status": configStatus, "error": configError}

	notificationStatus, notificationError := "ok", ""
	if s.deps.Notification == nil {
		notificationStatus, notificationError = "error", "notification manager not initialized"
	}
	checks["notification"] = map[string]any{"status": notificationStatus, "error": notificationError}

	schedulerStatus := "stopped"
	if s.deps.Monitor.IsRunning() {
		schedulerStatus = "ok"
	}
	checks["scheduler"] = map[string]any{"status": schedulerStatus}

	_, total, err := s.deps.Domains.StatusCounts(r.Context())
	if err != nil {
		total = 0
	}

	providers := map[string]any{}
	for _, health := range s.deps.Engine.Health().Snapshot(s.deps.Engine.Providers().Names()) {
		providers[health.Provider] = map[string]any{
			"state":          health.State,
			"requests":       health.Requests,
			"errors":         health.Errors,
			"avg_latency_ms": health.AvgLatency,
		}
	}

	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"status":    status,
		"timestamp": time.Now(),
		"version":   s.deps.Version,
		"monitor":   s.deps.Monitor.IsRunning(),
		"domains":   total,
		"database":  dbStatus,
		"scheduler": schedulerStatus,
		"checks":    checks,
		"providers": providers,
	})
}

// handleCheckUpdate 检查 GitHub 上是否有新版本
func (s *Server) handleCheckUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
		return
	}
	current := s.deps.Version

	ctx, cancel := timeoutContext(r, 8*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/NextCandy/DomainHunter/releases/latest", nil)
	if err != nil {
		s.writeJSON(w, r, http.StatusOK, map[string]any{"error": "无法检查更新", "currentVersion": current})
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "DomainHunter/"+current)

	resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
	if err != nil {
		s.log.Warn(logger.Fields{"error": err.Error()}, "检查更新失败")
		s.writeJSON(w, r, http.StatusOK, map[string]any{"error": "无法检查更新", "currentVersion": current})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.writeJSON(w, r, http.StatusOK, map[string]any{
			"error": "当前没有可用的 Release", "currentVersion": current,
		})
		return
	}

	var release struct {
		TagName     string `json:"tag_name"`
		PublishedAt string `json:"published_at"`
		Body        string `json:"body"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		s.writeJSON(w, r, http.StatusOK, map[string]any{"error": "解析更新信息失败", "currentVersion": current})
		return
	}

	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"currentVersion":  current,
		"latestVersion":   release.TagName,
		"publishedAt":     release.PublishedAt,
		"updateAvailable": release.TagName != current,
		"announcement":    release.Body,
	})
}
