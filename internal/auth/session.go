package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Session 用户会话
type Session struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	LastAccess time.Time `json:"last_access"`
	ExpiresAt  time.Time `json:"expires_at"`
	UserAgent  string    `json:"user_agent,omitempty"`
	IPAddress  string    `json:"ip_address,omitempty"`
	CSRFToken  string    `json:"-"`
}

// IsExpired 判断会话是否过期
func (s *Session) IsExpired() bool { return time.Now().After(s.ExpiresAt) }

// SessionStore 内存会话存储
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	maxAge   time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewSessionStore 创建会话存储并启动定期清理
func NewSessionStore() *SessionStore {
	store := &SessionStore{
		sessions: make(map[string]*Session),
		maxAge:   30 * 24 * time.Hour,
		stopCh:   make(chan struct{}),
	}
	go store.cleanupLoop()
	return store
}

// Create 创建新会话
func (s *SessionStore) Create() *Session {
	now := time.Now()
	session := &Session{
		ID:         randomToken(32),
		CSRFToken:  randomToken(32),
		CreatedAt:  now,
		LastAccess: now,
		ExpiresAt:  now.Add(s.MaxAge()),
	}
	s.mu.Lock()
	s.sessions[session.ID] = session
	s.mu.Unlock()
	return session
}

// Get 获取未过期的会话
func (s *SessionStore) Get(id string) *Session {
	s.mu.RLock()
	session, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok || session.IsExpired() {
		return nil
	}
	return session
}

// Touch 校验并续期会话，返回会话副本
func (s *SessionStore) Touch(id string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[id]
	if !ok {
		return nil
	}
	if session.IsExpired() {
		delete(s.sessions, id)
		return nil
	}
	session.LastAccess = time.Now()
	session.ExpiresAt = time.Now().Add(s.maxAge)
	copied := *session
	return &copied
}

// Delete 删除会话
func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

// Clear 清空全部会话（改密码/改用户名后强制重新登录）
func (s *SessionStore) Clear() {
	s.mu.Lock()
	s.sessions = make(map[string]*Session)
	s.mu.Unlock()
}

// Count 返回有效会话数量
func (s *SessionStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, session := range s.sessions {
		if !session.IsExpired() {
			count++
		}
	}
	return count
}

// CleanupExpired 清理过期会话，返回清理数量
func (s *SessionStore) CleanupExpired() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	cleaned := 0
	for id, session := range s.sessions {
		if session.IsExpired() {
			delete(s.sessions, id)
			cleaned++
		}
	}
	return cleaned
}

// MaxAge 返回会话有效期
func (s *SessionStore) MaxAge() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.maxAge
}

// SetMaxAge 设置会话有效期
func (s *SessionStore) SetMaxAge(maxAge time.Duration) {
	if maxAge <= 0 {
		return
	}
	s.mu.Lock()
	s.maxAge = maxAge
	s.mu.Unlock()
}

// SetInfo 记录会话的客户端信息
func (s *SessionStore) SetInfo(id, userAgent, ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session, ok := s.sessions[id]; ok {
		session.UserAgent = userAgent
		session.IPAddress = ip
	}
}

// Stop 停止后台清理协程
func (s *SessionStore) Stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
}

// Stats 返回会话统计
func (s *SessionStore) Stats() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	active, expired := 0, 0
	for _, session := range s.sessions {
		if session.IsExpired() {
			expired++
		} else {
			active++
		}
	}
	return map[string]any{
		"total_sessions":   len(s.sessions),
		"active_sessions":  active,
		"expired_sessions": expired,
		"max_age":          s.maxAge.String(),
	}
}

func (s *SessionStore) cleanupLoop() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.CleanupExpired()
		case <-s.stopCh:
			return
		}
	}
}

func randomToken(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand 失败在实践中意味着系统熵源不可用；
		// 这里退化为时间戳只是为了不 panic，调用方仍应视为异常环境。
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(buf)
}
