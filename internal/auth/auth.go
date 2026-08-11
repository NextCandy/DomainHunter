// Package auth 负责账号校验与会话管理。
//
// 密码不再以明文参与业务校验：登录时优先用 bcrypt 哈希比对；只有当数据库里
// 还没有哈希（老部署）时，才用旧的明文值比对，并在第一次登录成功后自动写入哈希。
// 用户下次修改密码时，明文行会被清空。
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"DomainHunter/internal/config"
	"DomainHunter/internal/logger"
)

// ErrInvalidCredentials 用户名或密码错误
var ErrInvalidCredentials = errors.New("用户名或密码错误")

const rememberDuration = 30 * 24 * time.Hour

// Persister 把凭据变更写回持久化存储
type Persister func(ctx context.Context, values map[string]string) error

// Authenticator 认证器
type Authenticator struct {
	mu             sync.RWMutex
	username       string
	passwordHash   string
	legacyPassword string
	sessionSecret  string

	sessions *SessionStore
	persist  Persister
	log      *logger.Logger
}

// New 创建认证器；必要时生成并持久化会话密钥
func New(ctx context.Context, server config.ServerConfig, persist Persister) (*Authenticator, error) {
	a := &Authenticator{
		username:       server.Username,
		passwordHash:   strings.TrimSpace(server.PasswordHash),
		legacyPassword: server.Password,
		sessionSecret:  strings.TrimSpace(server.SessionSecret),
		sessions:       NewSessionStore(),
		persist:        persist,
		log:            logger.Component("auth"),
	}

	if a.sessionSecret == "" {
		a.sessionSecret = randomToken(32)
		if persist != nil {
			if err := persist(ctx, map[string]string{config.KeySessionSecret: a.sessionSecret}); err != nil {
				return nil, fmt.Errorf("保存会话密钥失败: %w", err)
			}
		}
		a.log.Info(nil, "已生成新的会话签名密钥")
	}
	return a, nil
}

// Sessions 返回会话存储
func (a *Authenticator) Sessions() *SessionStore { return a.sessions }

// Stop 停止后台协程
func (a *Authenticator) Stop() { a.sessions.Stop() }

// Login 校验凭据并创建会话
func (a *Authenticator) Login(ctx context.Context, username, password string) (*Session, error) {
	if !a.Verify(ctx, username, password) {
		return nil, ErrInvalidCredentials
	}
	return a.sessions.Create(), nil
}

// Verify 校验用户名与密码，必要时把旧明文密码迁移为哈希
func (a *Authenticator) Verify(ctx context.Context, username, password string) bool {
	a.mu.RLock()
	expectedUser := a.username
	hash := a.passwordHash
	a.mu.RUnlock()

	if expectedUser == "" || subtle.ConstantTimeCompare([]byte(expectedUser), []byte(username)) != 1 {
		// 即便用户名不匹配也做一次哈希比对，避免通过响应时间区分用户名是否存在。
		if hash != "" {
			_ = bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
		}
		return false
	}
	return a.verifyPassword(ctx, password)
}

// VerifyPassword 只校验密码（修改密码时确认当前密码用）
func (a *Authenticator) VerifyPassword(ctx context.Context, password string) bool {
	return a.verifyPassword(ctx, password)
}

func (a *Authenticator) verifyPassword(ctx context.Context, password string) bool {
	a.mu.RLock()
	hash := a.passwordHash
	legacy := a.legacyPassword
	a.mu.RUnlock()

	if hash != "" {
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	}
	if legacy == "" {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(legacy), []byte(password)) != 1 {
		return false
	}

	// 旧部署第一次用明文密码登录成功：立刻升级为哈希。
	// 这里刻意保留 server_password 行，使得升级当天仍可无损回滚到旧版本；
	// 用户下次修改密码时该行会被清空。
	if err := a.storeHash(ctx, password, false); err != nil {
		a.log.Warn(logger.Fields{"error": err.Error()}, "旧密码迁移为哈希失败，本次登录仍然放行")
	} else {
		a.log.Info(nil, "已把旧的明文密码迁移为 bcrypt 哈希")
	}
	return true
}

func (a *Authenticator) storeHash(ctx context.Context, password string, clearLegacy bool) error {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	values := map[string]string{config.KeyPasswordHash: string(hashed)}
	if clearLegacy {
		values[config.KeyServerPassword] = ""
	}
	if a.persist != nil {
		if err := a.persist(ctx, values); err != nil {
			return err
		}
	}

	a.mu.Lock()
	a.passwordHash = string(hashed)
	if clearLegacy {
		a.legacyPassword = ""
	}
	a.mu.Unlock()
	return nil
}

// UpdatePassword 设置新密码，并清空遗留的明文密码
func (a *Authenticator) UpdatePassword(ctx context.Context, newPassword string) error {
	if len(newPassword) < 6 {
		return fmt.Errorf("新密码长度不能少于6位")
	}
	if err := a.storeHash(ctx, newPassword, true); err != nil {
		return err
	}
	a.sessions.Clear()
	return nil
}

// UpdateUsername 修改用户名
func (a *Authenticator) UpdateUsername(ctx context.Context, newUsername string) error {
	newUsername = strings.TrimSpace(newUsername)
	if len(newUsername) < 3 {
		return fmt.Errorf("用户名长度不能少于3位")
	}
	if a.persist != nil {
		if err := a.persist(ctx, map[string]string{config.KeyServerUsername: newUsername}); err != nil {
			return err
		}
	}
	a.mu.Lock()
	a.username = newUsername
	a.mu.Unlock()
	return nil
}

// Username 返回当前用户名
func (a *Authenticator) Username() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.username
}

// RequireAuth 是否需要登录（凭据齐全时为 true）
func (a *Authenticator) RequireAuth() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.username != "" && (a.passwordHash != "" || a.legacyPassword != "")
}

// UsesLegacyPassword 是否仍在使用未迁移的明文密码
func (a *Authenticator) UsesLegacyPassword() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.passwordHash == "" && a.legacyPassword != ""
}

// ValidateSession 校验并续期会话
func (a *Authenticator) ValidateSession(id string) (*Session, error) {
	if id == "" {
		return nil, fmt.Errorf("未提供会话ID")
	}
	session := a.sessions.Touch(id)
	if session == nil {
		return nil, fmt.Errorf("会话不存在或已过期")
	}
	return session, nil
}

// Logout 注销会话
func (a *Authenticator) Logout(id string) { a.sessions.Delete(id) }

// SessionMaxAge 返回会话有效期
func (a *Authenticator) SessionMaxAge() time.Duration { return a.sessions.MaxAge() }

// RememberDuration 返回"记住登录"有效期
func (a *Authenticator) RememberDuration() time.Duration { return rememberDuration }

// CreateSession 直接创建会话（记住登录恢复时使用）
func (a *Authenticator) CreateSession() *Session { return a.sessions.Create() }

// GenerateRememberToken 生成"记住登录"令牌。
// 使用独立的 session_secret 签名，不再拿密码当密钥。
func (a *Authenticator) GenerateRememberToken() string {
	a.mu.RLock()
	username, secret := a.username, a.sessionSecret
	a.mu.RUnlock()
	if username == "" || secret == "" {
		return ""
	}
	expiry := time.Now().Add(rememberDuration).Unix()
	payload := fmt.Sprintf("%s|%d", username, expiry)
	return fmt.Sprintf("%s|%s", payload, sign(secret, payload))
}

// ValidateRememberToken 校验"记住登录"令牌
func (a *Authenticator) ValidateRememberToken(token string) bool {
	a.mu.RLock()
	username, secret := a.username, a.sessionSecret
	a.mu.RUnlock()
	if username == "" || secret == "" {
		return false
	}

	parts := strings.Split(token, "|")
	if len(parts) != 3 {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(parts[0]), []byte(username)) != 1 {
		return false
	}
	expiry, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().After(time.Unix(expiry, 0)) {
		return false
	}
	expected := sign(secret, fmt.Sprintf("%s|%d", parts[0], expiry))
	return subtle.ConstantTimeCompare([]byte(expected), []byte(parts[2])) == 1
}

// Stats 返回认证统计
func (a *Authenticator) Stats() map[string]any {
	stats := a.sessions.Stats()
	stats["has_credentials"] = a.RequireAuth()
	stats["password_hashed"] = !a.UsesLegacyPassword()
	return stats
}

func sign(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}
