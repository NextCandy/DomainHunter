package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"DomainHunter/internal/auth"
	"DomainHunter/internal/config"
	"DomainHunter/internal/notification"
	"DomainHunter/internal/query"
	"DomainHunter/internal/scheduler"
	"DomainHunter/internal/service"
	"DomainHunter/internal/storage/sqlite"
)

func TestMain(m *testing.M) {
	os.Setenv("DOMAINHUNTER_RDAP_BOOTSTRAP_URL", "http://127.0.0.1:1/rdap.json")
	os.Exit(m.Run())
}

// newTestServer 组装一套真实依赖（内存 SQLite + 空 Provider 集合），
// 用来验证路由、认证与 CSRF 这些跨切面行为。
func newTestServer(t *testing.T) (*httptest.Server, *sqlite.DB) {
	t.Helper()

	db, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	ctx := context.Background()
	domainRepo := sqlite.NewDomainRepo(db)
	resultRepo := sqlite.NewResultRepo(db)
	observationRepo := sqlite.NewObservationRepo(db)
	settingsRepo := sqlite.NewSettingsRepo(db)
	notificationRepo := sqlite.NewNotificationRepo(db)

	cfg, err := config.Load(ctx, settingsRepo)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	engine := query.NewEngine(query.NewRegistry(), query.NewPolicy(query.Config{}))
	notifier := notification.NewManager(notificationRepo)

	settingsSvc := service.NewSettingsService(settingsRepo, cfg)
	querySvc := service.NewQueryService(engine, domainRepo, resultRepo, observationRepo, notifier, cfg)
	domainSvc := service.NewDomainService(domainRepo, resultRepo, observationRepo)
	sched := scheduler.New(domainRepo, querySvc, scheduler.Options{Workers: 1})
	monitorSvc := service.NewMonitorService(sched, querySvc, domainRepo, observationRepo, cfg)
	overviewSvc := service.NewOverviewService(domainRepo, resultRepo, observationRepo, engine, monitorSvc, domainSvc)

	authenticator, err := auth.New(ctx, cfg.Server, settingsSvc.Persist)
	if err != nil {
		t.Fatalf("创建认证器失败: %v", err)
	}
	t.Cleanup(authenticator.Stop)

	server := NewServer(Deps{
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
		Version:       "test",
	})

	ts := httptest.NewServer(server.routes())
	t.Cleanup(ts.Close)
	return ts, db
}

type client struct {
	t    *testing.T
	base string
	http *http.Client
	csrf string
}

func newClient(t *testing.T, ts *httptest.Server) *client {
	t.Helper()
	jar := &cookieJar{cookies: map[string]*http.Cookie{}}
	return &client{t: t, base: ts.URL, http: &http.Client{Jar: jar}}
}

func (c *client) do(method, path, body string, headers map[string]string) *http.Response {
	c.t.Helper()

	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		c.t.Fatalf("构造请求失败: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.csrf != "" {
		req.Header.Set(csrfHeader, c.csrf)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("请求失败: %v", err)
	}
	return resp
}

func (c *client) login(username, password string) *http.Response {
	c.t.Helper()
	resp := c.do(http.MethodPost, "/api/login",
		`{"username":"`+username+`","password":"`+password+`"}`, nil)
	if resp.StatusCode == http.StatusOK {
		var payload struct {
			CSRFToken string `json:"csrf_token"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&payload)
		c.csrf = payload.CSRFToken
	}
	return resp
}

// cookieJar 是一个不区分域名的极简 Cookie 罐，够本测试使用
type cookieJar struct {
	cookies map[string]*http.Cookie
}

func (j *cookieJar) SetCookies(_ *url.URL, cookies []*http.Cookie) {
	for _, cookie := range cookies {
		if cookie.MaxAge < 0 {
			delete(j.cookies, cookie.Name)
			continue
		}
		j.cookies[cookie.Name] = cookie
	}
}

func (j *cookieJar) Cookies(*url.URL) []*http.Cookie {
	out := make([]*http.Cookie, 0, len(j.cookies))
	for _, cookie := range j.cookies {
		out = append(out, cookie)
	}
	return out
}

func TestSessionLoginLogoutFlow(t *testing.T) {
	ts, _ := newTestServer(t)
	c := newClient(t, ts)

	if resp := c.do(http.MethodGet, "/api/domains", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("未登录访问应返回 401，实际 %d", resp.StatusCode)
	}

	if resp := c.login("domainhunter", "wrong-password"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("错误密码应返回 401，实际 %d", resp.StatusCode)
	}
	if resp := c.login("domainhunter", "domainhunter123"); resp.StatusCode != http.StatusOK {
		t.Fatalf("登录应成功，实际 %d", resp.StatusCode)
	}
	if c.csrf == "" {
		t.Fatal("登录响应应带 CSRF 令牌")
	}

	if resp := c.do(http.MethodGet, "/api/domains", "", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("登录后应能访问，实际 %d", resp.StatusCode)
	}

	// 前端调用的是 /api/logout；这里显式覆盖，防止路由缺失导致会话没有真正失效
	if resp := c.do(http.MethodPost, "/api/logout", "", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/logout 应返回 200，实际 %d", resp.StatusCode)
	}
	if resp := c.do(http.MethodGet, "/api/domains", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("退出后应返回 401，实际 %d", resp.StatusCode)
	}
}

func TestCSRFRejectsCrossOriginWrite(t *testing.T) {
	ts, _ := newTestServer(t)
	c := newClient(t, ts)
	if resp := c.login("domainhunter", "domainhunter123"); resp.StatusCode != http.StatusOK {
		t.Fatalf("登录失败: %d", resp.StatusCode)
	}

	token := c.csrf
	c.csrf = ""
	resp := c.do(http.MethodPost, "/api/domain/add", `{"domain":"evil.com"}`,
		map[string]string{"Origin": "http://evil.example"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("跨站写操作应被拒绝，实际 %d", resp.StatusCode)
	}

	c.csrf = token
	if resp := c.do(http.MethodPost, "/api/domain/add", `{"domain":"example.com"}`, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("带正确令牌的写操作应成功，实际 %d", resp.StatusCode)
	}
}

func TestFrontendRoutesAreRegistered(t *testing.T) {
	ts, _ := newTestServer(t)
	c := newClient(t, ts)
	if resp := c.login("domainhunter", "domainhunter123"); resp.StatusCode != http.StatusOK {
		t.Fatalf("登录失败: %d", resp.StatusCode)
	}

	// 管理端实际会调用的 GET 接口，任何一个返回 404 都说明路由缺失
	paths := []string{
		"/api/session",
		"/api/csrf",
		"/api/settings",
		"/api/stats",
		"/api/domains",
		"/api/v2/overview",
		"/api/v2/meta",
		"/api/v2/domains",
		"/api/v2/observations",
		"/api/v2/providers",
		"/api/v2/notifications",
		"/api/v2/settings",
		"/api/v2/backups",
		"/api/domains/example.com/history",
		"/api/domains/example.com/attempts",
		"/api/health/providers",
		"/health",
	}
	for _, path := range paths {
		resp := c.do(http.MethodGet, path, "", nil)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s 应返回 200，实际 %d", path, resp.StatusCode)
		}
	}
}

func TestUnknownAPIPathReturns404(t *testing.T) {
	ts, _ := newTestServer(t)
	c := newClient(t, ts)
	if resp := c.do(http.MethodGet, "/api/definitely-not-a-route", "", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("未知 API 路径应返回 404，实际 %d", resp.StatusCode)
	}
}

func TestSPAFallbackServesIndex(t *testing.T) {
	ts, _ := newTestServer(t)
	c := newClient(t, ts)

	for _, path := range []string{"/", "/login", "/domains", "/settings"} {
		resp := c.do(http.MethodGet, path, "", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s 应返回前端页面，实际 %d", path, resp.StatusCode)
		}
	}
}
