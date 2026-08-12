package ai

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	ErrSecretKeyRequired = errors.New("未配置 DOMAINHUNTER_SECRET_KEY，不能持久化 API Key")
	ErrUnsafeBaseURL     = errors.New("AI Base URL 不符合出站安全策略")
)

// Encryptor stores a key only as AES-GCM ciphertext. It is intentionally optional:
// deployments without DOMAINHUNTER_SECRET_KEY may still use DOMAINHUNTER_AI_API_KEY.
type Encryptor struct{ aead cipher.AEAD }

func NewEncryptorFromEnv() (*Encryptor, error) {
	value := strings.TrimSpace(os.Getenv("DOMAINHUNTER_SECRET_KEY"))
	if value == "" {
		return nil, ErrSecretKeyRequired
	}
	key := sha256.Sum256([]byte(value))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("创建 AI 密钥加密器失败: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("创建 AI 密钥加密器失败: %w", err)
	}
	return &Encryptor{aead: aead}, nil
}

func (e *Encryptor) Seal(plain string) (string, error) {
	if e == nil || e.aead == nil {
		return "", ErrSecretKeyRequired
	}
	nonce := make([]byte, e.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("生成密钥随机数失败: %w", err)
	}
	ciphertext := e.aead.Seal(nil, nonce, []byte(plain), nil)
	return base64.RawURLEncoding.EncodeToString(append(nonce, ciphertext...)), nil
}

func (e *Encryptor) Open(value string) (string, error) {
	if e == nil || e.aead == nil {
		return "", ErrSecretKeyRequired
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(data) < e.aead.NonceSize() {
		return "", errors.New("AI 密钥密文格式无效")
	}
	plain, err := e.aead.Open(nil, data[:e.aead.NonceSize()], data[e.aead.NonceSize():], nil)
	if err != nil {
		return "", errors.New("无法解密 AI 密钥")
	}
	return string(plain), nil
}

type BaseURLPolicy struct {
	AllowedHosts       map[string]struct{}
	AllowInsecureLocal bool
	Resolver           *net.Resolver
}

// BaseURLPolicyFromEnv only allows explicitly configured custom hosts.
// DOMAINHUNTER_AI_ALLOWED_HOSTS is a comma-separated host list; the supported
// first-party/default hosts are always allowed.
func BaseURLPolicyFromEnv() BaseURLPolicy {
	allowed := map[string]struct{}{"api.deepseek.com": {}}
	for _, raw := range strings.Split(os.Getenv("DOMAINHUNTER_AI_ALLOWED_HOSTS"), ",") {
		if host := strings.ToLower(strings.TrimSpace(raw)); host != "" {
			allowed[host] = struct{}{}
		}
	}
	allowLocal, _ := strconv.ParseBool(os.Getenv("DOMAINHUNTER_ALLOW_INSECURE_AI_BASE_URL"))
	return BaseURLPolicy{AllowedHosts: allowed, AllowInsecureLocal: allowLocal, Resolver: net.DefaultResolver}
}

// NormalizeBaseURL validates a base URL and appends /chat/completions exactly once.
// The persisted BaseURL remains the root, while the returned URL is the request endpoint.
func NormalizeBaseURL(ctx context.Context, raw string, policy BaseURLPolicy) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, ErrUnsafeBaseURL
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return nil, ErrUnsafeBaseURL
	}
	if u.Scheme != "https" {
		if !(policy.AllowInsecureLocal && u.Scheme == "http" && isLocalDevelopmentHost(host)) {
			return nil, ErrUnsafeBaseURL
		}
	}
	localAllowed := policy.AllowInsecureLocal && isLocalDevelopmentHost(host)
	if !localAllowed {
		if _, ok := policy.AllowedHosts[host]; !ok {
			return nil, fmt.Errorf("%w：主机未在 AI allowlist 中", ErrUnsafeBaseURL)
		}
	}
	if !localAllowed && u.Scheme != "https" {
		return nil, fmt.Errorf("%w：主机未在 AI allowlist 中", ErrUnsafeBaseURL)
	}
	path := strings.TrimRight(u.Path, "/")
	if strings.HasSuffix(path, "/chat/completions") {
		return nil, fmt.Errorf("%w：请填写 Base URL，不要填写 chat completions 端点", ErrUnsafeBaseURL)
	}
	if !localAllowed {
		if err := validateHostIPs(ctx, host, policy.Resolver); err != nil {
			return nil, err
		}
	}
	u.Path = path + "/chat/completions"
	return u, nil
}

func validateURLTarget(ctx context.Context, u *url.URL, policy BaseURLPolicy) error {
	if u == nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return ErrUnsafeBaseURL
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return ErrUnsafeBaseURL
	}
	if u.Scheme != "https" && !(policy.AllowInsecureLocal && u.Scheme == "http" && isLocalDevelopmentHost(host)) {
		return ErrUnsafeBaseURL
	}
	localAllowed := policy.AllowInsecureLocal && isLocalDevelopmentHost(host)
	if !localAllowed {
		if _, ok := policy.AllowedHosts[host]; !ok {
			return fmt.Errorf("%w：主机未在 AI allowlist 中", ErrUnsafeBaseURL)
		}
	}
	if policy.AllowInsecureLocal && isLocalDevelopmentHost(host) {
		return nil
	}
	return validateHostIPs(ctx, host, policy.Resolver)
}

func isLocalDevelopmentHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func validateHostIPs(ctx context.Context, host string, resolver *net.Resolver) error {
	if ip := net.ParseIP(host); ip != nil {
		if forbiddenIP(ip) {
			return fmt.Errorf("%w：IP 地址不允许", ErrUnsafeBaseURL)
		}
		return nil
	}
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	ips, err := resolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("%w：无法解析主机", ErrUnsafeBaseURL)
	}
	for _, item := range ips {
		if forbiddenIP(item.IP) {
			return fmt.Errorf("%w：主机解析到内网或保留地址", ErrUnsafeBaseURL)
		}
	}
	return nil
}

func forbiddenIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		// 100.64.0.0/10 carrier-grade NAT and metadata / reserved ranges.
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return true
		}
		if v4[0] == 169 && v4[1] == 254 {
			return true
		}
		if v4[0] == 0 || v4[0] >= 224 {
			return true
		}
	}
	return false
}

// NewSafeHTTPClient revalidates each dial and rejects redirects to a different host.
func NewSafeHTTPClient(policy BaseURLPolicy, timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	resolver := policy.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	dialer := &net.Dialer{Timeout: timeout / 2, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		// 不使用环境代理，避免代理改变实际出站目标而绕过逐跳 DNS/IP 校验。
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   timeout / 2,
		ResponseHeaderTimeout: timeout,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			host = strings.ToLower(host)
			localAllowed := policy.AllowInsecureLocal && isLocalDevelopmentHost(host)
			if !localAllowed {
				if _, ok := policy.AllowedHosts[host]; !ok {
					return nil, ErrUnsafeBaseURL
				}
			}
			if localAllowed {
				return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
			}
			if _, ok := policy.AllowedHosts[host]; !ok {
				return nil, ErrUnsafeBaseURL
			}
			ips, err := resolver.LookupIPAddr(ctx, host)
			if err != nil || len(ips) == 0 {
				return nil, ErrUnsafeBaseURL
			}
			for _, item := range ips {
				if forbiddenIP(item.IP) {
					return nil, ErrUnsafeBaseURL
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 1 && !strings.EqualFold(req.URL.Hostname(), via[0].URL.Hostname()) {
				return ErrUnsafeBaseURL
			}
			return validateURLTarget(req.Context(), req.URL, policy)
		},
	}
}
