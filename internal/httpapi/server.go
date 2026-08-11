// Package httpapi 提供 HTTP 服务：路由、中间件与 handler。
//
// Handler 只负责解析请求、校验参数、调用 service、返回响应；
// 不再直接操作 SQLite、查询引擎或调度器。
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"DomainHunter/internal/auth"
	"DomainHunter/internal/config"
	"DomainHunter/internal/logger"
	"DomainHunter/internal/notification"
	"DomainHunter/internal/query"
	"DomainHunter/internal/repository"
	"DomainHunter/internal/service"
	"DomainHunter/internal/storage/sqlite"
)

// Deps 组装 HTTP 层需要的全部依赖
type Deps struct {
	DB                 *sqlite.DB
	Auth               *auth.Authenticator
	Settings           *service.SettingsService
	Domains            *service.DomainService
	Query              *service.QueryService
	Monitor            *service.MonitorService
	Overview           *service.OverviewService
	Notification       *notification.Manager
	Engine             *query.Engine
	Notifications      repository.NotificationRepository
	Tokens             repository.APITokenRepository
	NotificationConfig repository.NotificationConfigRepository
	Version            string
}

// Server HTTP 服务器
type Server struct {
	deps Deps
	log  *logger.Logger

	mu  sync.RWMutex
	cfg *config.Config

	httpServer *http.Server

	loginLimiter   *RateLimiter
	batchLimiter   *RateLimiter
	generalLimiter *RateLimiter
}

// NewServer 创建 HTTP 服务器
func NewServer(deps Deps) *Server {
	// 旧的 cmd 组装代码不需要感知新增的 Repository；只要提供 DB，HTTP 层
	// 就能把 P2 存储能力接入现有单体进程。
	if deps.DB != nil {
		if deps.Tokens == nil {
			deps.Tokens = sqlite.NewAPITokenRepo(deps.DB)
		}
		if deps.NotificationConfig == nil {
			deps.NotificationConfig = sqlite.NewNotificationConfigRepo(deps.DB)
		}
		if deps.Domains != nil {
			deps.Domains.SetFolderRepository(sqlite.NewFolderRepo(deps.DB))
		}
		if deps.Notification != nil {
			if rules, err := deps.NotificationConfig.ListRules(context.Background()); err == nil {
				deps.Notification.SetRules(rules)
			}
			if templates, err := deps.NotificationConfig.ListTemplates(context.Background()); err == nil {
				deps.Notification.SetTemplates(templates)
			}
		}
	}
	s := &Server{
		deps:           deps,
		cfg:            deps.Settings.Config(),
		log:            logger.Component("http"),
		loginLimiter:   NewRateLimiter(5, 5*time.Minute),
		batchLimiter:   NewRateLimiter(10, time.Minute),
		generalLimiter: NewRateLimiter(600, time.Minute),
	}
	deps.Settings.OnChange(func(cfg *config.Config) {
		s.mu.Lock()
		s.cfg = cfg
		s.mu.Unlock()
	})
	return s
}

func (s *Server) config() *config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// Start 启动 HTTP 服务
func (s *Server) Start() error {
	s.httpServer = &http.Server{
		Addr:              ":" + s.config().Server.Port,
		Handler:           s.routes(),
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	s.log.Info(logger.Fields{"port": s.config().Server.Port}, "Web 服务器启动")
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("启动Web服务器失败: %w", err)
	}
	return nil
}

// Stop 优雅关闭 HTTP 服务
func (s *Server) Stop(ctx context.Context) error {
	s.loginLimiter.Stop()
	s.batchLimiter.Stop()
	s.generalLimiter.Stop()

	if s.httpServer == nil {
		return nil
	}
	if err := s.httpServer.Shutdown(ctx); err != nil {
		s.log.Warn(logger.Fields{"error": err.Error()}, "优雅关闭失败，执行强制关闭")
		return s.httpServer.Close()
	}
	return nil
}

func (s *Server) writeJSON(w http.ResponseWriter, r *http.Request, status int, payload any) {
	s.applyCORS(w, r)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		s.log.Error(logger.Fields{"error": err.Error()}, "写入响应失败")
	}
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, status int, message string) {
	s.applyCORS(w, r)
	// 旧前端用 response.text() 读取错误，这里保持纯文本 + 状态码的形式。
	http.Error(w, message, status)
}

func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	if err := decoder.Decode(target); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "请求体不是合法的 JSON")
		return false
	}
	return true
}
