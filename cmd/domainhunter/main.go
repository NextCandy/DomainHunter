// Command domainhunter 是 DomainHunter 的单体入口。
//
// 组装顺序：SQLite → 迁移 → 仓储 → 配置 → 查询引擎 → 服务 → 调度器 → HTTP。
// 依赖只从外向内传递，没有全局单例，关闭时按相反顺序优雅退出。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	strictai "DomainHunter/internal/ai"
	"DomainHunter/internal/auth"
	"DomainHunter/internal/config"
	"DomainHunter/internal/httpapi"
	"DomainHunter/internal/logger"
	"DomainHunter/internal/notification"
	"DomainHunter/internal/p1"
	"DomainHunter/internal/query"
	"DomainHunter/internal/query/providers/ai"
	"DomainHunter/internal/query/providers/fallback"
	"DomainHunter/internal/query/providers/rdap"
	"DomainHunter/internal/query/providers/rdaporg"
	"DomainHunter/internal/query/providers/spaceship"
	"DomainHunter/internal/query/providers/whodat"
	"DomainHunter/internal/query/providers/whois"
	"DomainHunter/internal/query/providers/whoisls"
	"DomainHunter/internal/registry"
	"DomainHunter/internal/scheduler"
	"DomainHunter/internal/service"
	"DomainHunter/internal/storage/sqlite"
)

var (
	// AppName 应用名称
	AppName = "DomainHunter"
	// AppVersion 版本号，由构建时 -ldflags 注入
	AppVersion = "v2.0.0"
)

func main() {
	showVersion := flag.Bool("version", false, "显示版本信息")
	dataDir := flag.String("data-dir", envOrDefault("DOMAINHUNTER_DATA_DIR", sqlite.DefaultDir), "数据目录")
	flag.BoolVar(showVersion, "v", false, "显示版本信息")
	flag.Parse()

	if *showVersion {
		fmt.Printf("%s %s\n", AppName, AppVersion)
		return
	}

	if err := run(*dataDir); err != nil {
		logger.Fatal("启动失败: %v", err)
	}
}

func run(dataDir string) error {
	fmt.Printf("%s %s\n正在启动...\n", AppName, AppVersion)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ---- 存储 ----
	db, err := sqlite.Open(dataDir)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		return err
	}
	p1Service := p1.New(db)
	p1Service.Start(ctx)
	defer p1Service.Stop()
	strictAIRepo := sqlite.NewAIRepo(db)

	domainRepo := sqlite.NewDomainRepo(db)
	resultRepo := sqlite.NewResultRepo(db)
	observationRepo := sqlite.NewObservationRepo(db)
	settingsRepo := sqlite.NewSettingsRepo(db)
	notificationRepo := sqlite.NewNotificationRepo(db)

	// ---- 配置与日志 ----
	cfg, err := config.Load(ctx, settingsRepo)
	if err != nil {
		return err
	}
	if err := logger.Init(cfg.Log.Level, cfg.Log.File); err != nil {
		logger.Warn("初始化日志失败: %v", err)
	}
	defer logger.Close()

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("配置验证失败: %w", err)
	}
	logger.Info("日志系统已初始化，级别: %s", cfg.Log.Level)

	if results, notifications, err := domainRepo.CleanOrphaned(ctx); err != nil {
		logger.Warn("清理孤立数据失败: %v", err)
	} else if results > 0 || notifications > 0 {
		logger.Info("清理孤立数据完成: %d 条结果, %d 条通知历史", results, notifications)
	}

	// ---- TLD / RDAP 映射 ----
	if err := registry.Load(); err != nil {
		return fmt.Errorf("加载查询源配置失败: %w", err)
	}
	if bootstrap := registry.Bootstrap(); bootstrap.OK {
		logger.Info("已合并 %d 条 IANA RDAP 映射", bootstrap.Merged)
	} else {
		logger.Warn("IANA RDAP bootstrap 不可用（%s），使用内置静态映射", bootstrap.Error)
	}

	// ---- 查询引擎 ----
	policyCfg, err := loadQueryPolicy(cfg)
	if err != nil {
		logger.Warn("查询策略无效，回落到默认策略: %v", err)
		policyCfg = query.Config{}
	}
	policy := query.NewPolicy(policyCfg)
	providers := query.NewRegistry(
		whodat.NewPi(cfg.Monitor.Timeout),
		fallback.NewNamed(query.ProviderWhoisDomainLookup, cfg.Monitor.Timeout),
		whodat.NewVercel(cfg.Monitor.Timeout),
		rdap.New(cfg.Monitor.Timeout),
		rdaporg.New(cfg.Monitor.Timeout),
		ai.NewWithResolver(cfg.Monitor.Timeout, p1Service.AIQueryConfig),
		whoisls.New(cfg.Monitor.Timeout),
		fallback.New(cfg.Monitor.Timeout),
		whois.New(cfg.Monitor.Timeout),
		spaceship.New(cfg.Monitor.Timeout),
	)
	engine := query.NewEngine(providers, policy)

	// ---- 通知 ----
	notifier := notification.NewManager(notificationRepo)
	notifier.RegisterAll(cfg)
	notifier.Start()
	logger.Info("已启用的通知渠道: %v", notifier.EnabledNames())

	// ---- 服务 ----
	settingsSvc := service.NewSettingsService(settingsRepo, cfg)
	querySvc := service.NewQueryService(engine, domainRepo, resultRepo, observationRepo, notifier, cfg)
	domainSvc := service.NewDomainService(domainRepo, resultRepo, observationRepo)

	sched := scheduler.New(domainRepo, querySvc, scheduler.Options{
		Workers:         cfg.Monitor.ConcurrentLimit,
		DefaultInterval: cfg.Monitor.CheckInterval,
	})
	monitorSvc := service.NewMonitorService(sched, querySvc, domainRepo, observationRepo, cfg)
	overviewSvc := service.NewOverviewService(domainRepo, resultRepo, observationRepo, engine, monitorSvc, domainSvc)
	domainSvc.SetEnqueuer(monitorSvc.Enqueue)

	settingsSvc.OnChange(func(next *config.Config) {
		monitorSvc.UpdateConfig(next)
		notifier.ApplyConfig(next)
		if parsed, err := loadQueryPolicy(next); err == nil {
			engine.ApplyPolicy(parsed)
		} else {
			logger.Warn("查询策略更新失败: %v", err)
		}
	})

	// ---- 认证 ----
	authenticator, err := auth.New(ctx, cfg.Server, settingsSvc.Persist)
	if err != nil {
		return err
	}
	defer authenticator.Stop()

	// ---- 调度 ----
	if err := monitorSvc.Start(ctx); err != nil {
		logger.Warn("启动监控器失败: %v", err)
	} else {
		logger.Info("域名监控器已启动，并发: %d，检查间隔: %v",
			cfg.Monitor.ConcurrentLimit, cfg.Monitor.CheckInterval)
	}

	// ---- HTTP ----
	strictEncryptor, secretErr := strictai.NewEncryptorFromEnv()
	if secretErr != nil {
		logger.Warn("严格 AI 估价未启用数据库 Key 保存: %v", secretErr)
	}
	strictPolicy := strictai.BaseURLPolicyFromEnv()
	strictAIService := strictai.NewService(
		strictAIRepo,
		domainRepo,
		resultRepo,
		strictai.DeepSeekCompatibleClient{Policy: strictPolicy},
		strictEncryptor,
		strictPolicy,
	)
	if err := strictAIService.EnsureDefaultProfile(ctx); err != nil {
		logger.Warn("初始化严格 DeepSeek AI 档案失败: %v", err)
	}
	strictAIWorker := strictai.NewWorker(strictAIService, 5)
	strictAIWorker.Start(ctx)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := strictAIWorker.Stop(shutdownCtx); err != nil {
			logger.Warn("停止严格 AI 估价 worker 失败: %v", err)
		}
	}()
	server := httpapi.NewServer(httpapi.Deps{
		DB:            db,
		Auth:          authenticator,
		Settings:      settingsSvc,
		Domains:       domainSvc,
		Query:         querySvc,
		Monitor:       monitorSvc,
		Overview:      overviewSvc,
		Notification:  notifier,
		Engine:        engine,
		Notifications: notificationRepo,
		P1:            p1Service,
		AI:            strictAIService,
		Version:       AppVersion,
	})

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("访问地址: http://localhost:%s", cfg.Server.Port)
		serverErr <- server.Start()
	}()

	select {
	case <-ctx.Done():
		logger.Info("收到退出信号，正在关闭应用程序...")
	case err := <-serverErr:
		if err != nil {
			logger.Error("Web服务器异常退出: %v", err)
		}
	}

	// ---- 优雅关闭 ----
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Stop(shutdownCtx); err != nil {
		logger.Warn("停止Web服务器时出错: %v", err)
	} else {
		logger.Info("Web服务器已停止")
	}

	monitorSvc.Stop()
	logger.Info("域名监控器已停止")

	notifier.Stop()
	logger.Info("通知管理器已停止")

	logger.Info("应用程序已优雅关闭")
	return nil
}

// loadQueryPolicy 读取查询策略：优先使用 JSON 文件，其次数据库设置
func loadQueryPolicy(cfg *config.Config) (query.Config, error) {
	if fileCfg, present, err := query.LoadConfigFromEnvFile(); present {
		return fileCfg, err
	}
	return query.LoadConfig(cfg.QueryPolicy)
}

func envOrDefault(key, fallbackValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallbackValue
}
