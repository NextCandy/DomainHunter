package auth

import (
	"context"
	"sync"
	"testing"

	"DomainHunter/internal/config"
)

type memoryStore struct {
	mu     sync.Mutex
	values map[string]string
}

func newStore() *memoryStore { return &memoryStore{values: map[string]string{}} }

func (m *memoryStore) persist(_ context.Context, values map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range values {
		m.values[k] = v
	}
	return nil
}

func (m *memoryStore) get(key string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.values[key]
}

func TestLegacyPlaintextPasswordMigratesToHashOnFirstLogin(t *testing.T) {
	store := newStore()
	ctx := context.Background()

	a, err := New(ctx, config.ServerConfig{
		Username: "admin",
		Password: "legacy-secret",
	}, store.persist)
	if err != nil {
		t.Fatalf("创建认证器失败: %v", err)
	}
	if !a.UsesLegacyPassword() {
		t.Fatal("初始状态应当是未迁移的明文密码")
	}
	if store.get(config.KeySessionSecret) == "" {
		t.Fatal("应当自动生成并持久化会话密钥")
	}

	if _, err := a.Login(ctx, "admin", "wrong"); err == nil {
		t.Fatal("错误密码不应登录成功")
	}
	if _, err := a.Login(ctx, "admin", "legacy-secret"); err != nil {
		t.Fatalf("旧明文密码应当仍可登录: %v", err)
	}

	hash := store.get(config.KeyPasswordHash)
	if hash == "" {
		t.Fatal("首次登录后应写入密码哈希")
	}
	if a.UsesLegacyPassword() {
		t.Fatal("迁移后不应再依赖明文密码")
	}
	// 为了保证升级当天仍可回滚到旧版本，明文行此时仍然保留
	if store.get(config.KeyServerPassword) == "" && store.values[config.KeyServerPassword] != "" {
		t.Fatal("迁移阶段不应清空明文密码行")
	}

	if _, err := a.Login(ctx, "admin", "legacy-secret"); err != nil {
		t.Fatalf("迁移后用同一密码仍应登录成功: %v", err)
	}
}

func TestUpdatePasswordClearsLegacyPlaintext(t *testing.T) {
	store := newStore()
	ctx := context.Background()
	store.values[config.KeyServerPassword] = "legacy-secret"

	a, err := New(ctx, config.ServerConfig{Username: "admin", Password: "legacy-secret"}, store.persist)
	if err != nil {
		t.Fatalf("创建认证器失败: %v", err)
	}

	if err := a.UpdatePassword(ctx, "short"); err == nil {
		t.Fatal("过短的密码应被拒绝")
	}
	if err := a.UpdatePassword(ctx, "brand-new-secret"); err != nil {
		t.Fatalf("修改密码失败: %v", err)
	}
	if store.get(config.KeyServerPassword) != "" {
		t.Fatal("修改密码后应清空遗留的明文密码")
	}
	if _, err := a.Login(ctx, "admin", "legacy-secret"); err == nil {
		t.Fatal("旧密码在修改后不应再能登录")
	}
	if _, err := a.Login(ctx, "admin", "brand-new-secret"); err != nil {
		t.Fatalf("新密码应能登录: %v", err)
	}
}

func TestRememberTokenUsesSessionSecret(t *testing.T) {
	store := newStore()
	ctx := context.Background()

	a, err := New(ctx, config.ServerConfig{Username: "admin", Password: "secret123"}, store.persist)
	if err != nil {
		t.Fatalf("创建认证器失败: %v", err)
	}

	token := a.GenerateRememberToken()
	if token == "" {
		t.Fatal("应能生成记住登录令牌")
	}
	if !a.ValidateRememberToken(token) {
		t.Fatal("刚生成的令牌应当有效")
	}
	if a.ValidateRememberToken(token + "x") {
		t.Fatal("被篡改的令牌不应通过校验")
	}

	// 改密码不再影响记住登录令牌（签名密钥独立），但改用户名会
	if err := a.UpdateUsername(ctx, "operator"); err != nil {
		t.Fatalf("修改用户名失败: %v", err)
	}
	if a.ValidateRememberToken(token) {
		t.Fatal("用户名变更后旧令牌应失效")
	}
}

func TestSessionLifecycle(t *testing.T) {
	store := newStore()
	ctx := context.Background()
	a, err := New(ctx, config.ServerConfig{Username: "admin", Password: "secret123"}, store.persist)
	if err != nil {
		t.Fatalf("创建认证器失败: %v", err)
	}
	defer a.Stop()

	session, err := a.Login(ctx, "admin", "secret123")
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	if session.CSRFToken == "" {
		t.Fatal("会话应携带 CSRF 令牌")
	}
	if _, err := a.ValidateSession(session.ID); err != nil {
		t.Fatalf("会话校验失败: %v", err)
	}

	a.Logout(session.ID)
	if _, err := a.ValidateSession(session.ID); err == nil {
		t.Fatal("注销后会话应失效")
	}
}
