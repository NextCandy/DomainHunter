package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"DomainHunter/internal/ai"
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
	strictPolicy := ai.BaseURLPolicyFromEnv()
	strictAI := ai.NewService(sqlite.NewAIRepo(db), domainRepo, resultRepo, ai.DeepSeekCompatibleClient{Policy: strictPolicy}, nil, strictPolicy)

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
		AI:            strictAI,
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

// 退出登录必须让"记住登录"令牌一并失效：客户端就算留着那个 cookie，
// 也不能再免密码进来。
func TestLogoutRevokesRememberToken(t *testing.T) {
	ts, _ := newTestServer(t)
	c := newClient(t, ts)

	resp := c.login("domainhunter", "domainhunter123")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("登录失败: %d", resp.StatusCode)
	}
	var remember string
	for _, cookie := range resp.Cookies() {
		if cookie.Name == rememberCookie {
			remember = cookie.Value
		}
	}
	if remember == "" {
		t.Fatal("登录应下发记住登录令牌")
	}

	if resp := c.do(http.MethodPost, "/api/logout", "", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("退出失败: %d", resp.StatusCode)
	}

	// 手工回放旧的 remember_token
	replay := c.do(http.MethodGet, "/api/domains", "",
		map[string]string{"Cookie": rememberCookie + "=" + remember})
	if replay.StatusCode != http.StatusUnauthorized {
		t.Fatalf("退出后回放旧的记住登录令牌应返回 401，实际 %d", replay.StatusCode)
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
		"/api/v2/overview/trend?days=7",
		"/api/v2/meta",
		"/api/v2/facets",
		"/api/v2/domains",
		"/api/v2/ai/valuation-policy",
		"/api/v2/ai/profiles",
		"/api/v2/observations",
		"/api/v2/providers",
		"/api/v2/notifications",
		"/api/v2/notifications/preferences",
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

func TestV2DomainListOmitsSensitiveFields(t *testing.T) {
	ts, db := newTestServer(t)
	c := newClient(t, ts)
	if resp := c.login("domainhunter", "domainhunter123"); resp.StatusCode != http.StatusOK {
		t.Fatalf("登录失败: %d", resp.StatusCode)
	}
	checked := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO domains(name, enabled, notify, note, tags) VALUES(?, 1, 1, ?, ?)`, "secret.example", "不要发送给 AI", "重点"); err != nil {
		t.Fatalf("插入测试域名失败: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO domain_results(domain, status, registrar, last_checked, query_method, name_servers, whois_raw, error_message, epp_statuses, confidence) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "secret.example", "registered", "Example Registrar", checked, "rdap", "ns1.example,ns2.example", "Registrant Email: private@example", "", "clientTransferProhibited", "high"); err != nil {
		t.Fatalf("插入测试结果失败: %v", err)
	}

	assertListSafe := func(path string) {
		resp := c.do(http.MethodGet, path, "", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s 应返回 200，实际 %d", path, resp.StatusCode)
		}
		var payload struct {
			Domains []map[string]json.RawMessage `json:"domains"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("列表响应不是合法 JSON: %v", err)
		}
		if len(payload.Domains) != 1 {
			t.Fatalf("应返回 1 条列表数据，实际 %d", len(payload.Domains))
		}
		for _, forbidden := range []string{"whois_raw", "name_servers", "note"} {
			if _, ok := payload.Domains[0][forbidden]; ok {
				t.Errorf("列表 JSON 不应包含 %s", forbidden)
			}
		}
	}

	assertListSafe("/api/v2/domains")
	assertListSafe("/api/v2/domains?filter=" + url.QueryEscape(`{"version":1,"logic":"and","conditions":[]}`))
}

func TestOverviewTrendEndpointReturnsStableContract(t *testing.T) {
	ts, db := newTestServer(t)
	c := newClient(t, ts)
	if resp := c.login("domainhunter", "domainhunter123"); resp.StatusCode != http.StatusOK {
		t.Fatalf("登录失败: %d", resp.StatusCode)
	}
	if _, err := db.Exec(`INSERT INTO domain_observations(domain_id, domain, status, provider, confidence, changed, observed_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`, 1, "trend.com", "available", "rdap", "high", 1, time.Now().In(time.Local)); err != nil {
		t.Fatalf("插入趋势观测失败: %v", err)
	}

	resp := c.do(http.MethodGet, "/api/v2/overview/trend?days=2", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("趋势接口应返回 200，实际 %d", resp.StatusCode)
	}
	var payload struct {
		Days   int `json:"days"`
		Points []struct {
			Day          string         `json:"day"`
			Total        int            `json:"total"`
			Available    int            `json:"available"`
			HighScore    int            `json:"high_score"`
			Changes      int            `json:"changes"`
			StatusCounts map[string]int `json:"status_counts"`
		} `json:"points"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("趋势响应不是合法 JSON: %v", err)
	}
	if payload.Days != 2 || len(payload.Points) != 2 {
		t.Fatalf("趋势天数/点数契约错误: %+v", payload)
	}
	last := payload.Points[len(payload.Points)-1]
	if last.Total != 1 || last.Available != 1 || last.HighScore != 1 || last.Changes != 1 || last.StatusCounts["available"] != 1 {
		t.Fatalf("趋势聚合字段错误: %+v", last)
	}

	resp = c.do(http.MethodGet, "/api/v2/overview/trend?days=0", "", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("非法 days 应返回 400，实际 %d", resp.StatusCode)
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

func TestV2FoldersExportsMetricsAndTokenScopes(t *testing.T) {
	ts, db := newTestServer(t)
	c := newClient(t, ts)
	if resp := c.login("domainhunter", "domainhunter123"); resp.StatusCode != http.StatusOK {
		t.Fatalf("登录失败: %d", resp.StatusCode)
	}

	resp := c.do(http.MethodPost, "/api/v2/folders", `{"name":"重点域名"}`, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("创建文件夹应返回 201，实际 %d", resp.StatusCode)
	}
	var folder struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&folder); err != nil || folder.ID <= 0 {
		t.Fatalf("文件夹响应无效: %+v %v", folder, err)
	}
	if _, err := db.Exec(`INSERT INTO domains(name, enabled, notify, folder_id) VALUES(?, 1, 1, NULL)`, "example.com"); err != nil {
		t.Fatalf("插入测试域名失败: %v", err)
	}

	resp = c.do(http.MethodPost, "/api/v2/domains/batch-move-folder",
		`{"domains":["example.com"],"folder_id":`+strconv.FormatInt(folder.ID, 10)+`}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("批量移动应返回 200，实际 %d", resp.StatusCode)
	}
	var assigned int64
	if err := db.QueryRow(`SELECT folder_id FROM domains WHERE name = 'example.com'`).Scan(&assigned); err != nil || assigned != folder.ID {
		t.Fatalf("域名未移动到文件夹: %d %v", assigned, err)
	}

	resp = c.do(http.MethodGet, "/api/v2/domains/export?format=csv", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CSV 导出应返回 200，实际 %d", resp.StatusCode)
	}
	data, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(data), "example.com") {
		t.Fatalf("CSV 导出缺少测试域名: %s", data)
	}
	resp = c.do(http.MethodPost, "/api/v2/domains/import?format=json&mode=overwrite",
		`{"domains":[{"domain":"example.com","note":"updated"}]}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("JSON 导入应返回 200，实际 %d", resp.StatusCode)
	}
	resp = c.do(http.MethodGet, "/api/v2/domains/example.com/history/export?format=json", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("历史 JSON 导出应返回 200，实际 %d", resp.StatusCode)
	}
	resp = c.do(http.MethodPost, "/api/v2/notifications/rules",
		`{"name":"available-only","enabled":true,"statuses":["available"]}`, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("创建通知规则应返回 201，实际 %d", resp.StatusCode)
	}
	resp = c.do(http.MethodPost, "/api/v2/notifications/templates",
		`{"name":"simple","event_type":"status_change","subject":"{{domain}}","body":"{{status}}","enabled":true}`, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("创建通知模板应返回 201，实际 %d", resp.StatusCode)
	}
	resp = c.do(http.MethodGet, "/api/v2/notifications/digest", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("读取通知摘要应返回 200，实际 %d", resp.StatusCode)
	}
	resp = c.do(http.MethodPost, "/api/v2/domains/batch-retry-failed", `{}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("失败重查 API 应返回 200，实际 %d", resp.StatusCode)
	}

	metricsClient := newClient(t, ts)
	resp = metricsClient.do(http.MethodGet, "/metrics", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/metrics 应无需认证并返回 200，实际 %d", resp.StatusCode)
	}
	metrics, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(metrics), "domainhunter_domains_total") {
		t.Fatalf("metrics 缺少域名指标: %s", metrics)
	}

	resp = c.do(http.MethodPost, "/api/v2/tokens", `{"name":"read-only","scopes":["read"]}`, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("创建 token 应返回 201，实际 %d", resp.StatusCode)
	}
	var token struct {
		Raw string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil || token.Raw == "" {
		t.Fatalf("token 只显示一次的响应无效: %+v %v", token, err)
	}
	tokenClient := newClient(t, ts)
	resp = tokenClient.do(http.MethodGet, "/api/v2/folders", "", map[string]string{"Authorization": "Bearer " + token.Raw})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read scope 应能读取文件夹，实际 %d", resp.StatusCode)
	}
	resp = tokenClient.do(http.MethodPost, "/api/v2/folders", `{"name":"blocked"}`,
		map[string]string{"Authorization": "Bearer " + token.Raw})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("read scope 不应写入文件夹，实际 %d", resp.StatusCode)
	}
}
