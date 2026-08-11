// Package httpx 提供全局复用的 HTTP 客户端与拨号器。
//
// 重构前每次查询都会新建 http.Client 与 Transport，连接池实际从未生效。
// 这里按 timeout 缓存客户端，所有 Provider 共享同一组连接池，同时保留对
// ALL_PROXY / HTTP_PROXY / NO_PROXY 环境变量的支持。
package httpx

import (
	"context"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

var (
	mu        sync.RWMutex
	clients   = map[time.Duration]*http.Client{}
	transport *http.Transport
	once      sync.Once
)

func baseTransport() *http.Transport {
	once.Do(func() {
		dialer := proxy.FromEnvironment()
		t := &http.Transport{
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   8,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
		// ALL_PROXY（SOCKS5）必须走自定义拨号器；否则用标准的
		// ProxyFromEnvironment，它已经覆盖 HTTP_PROXY/HTTPS_PROXY/NO_PROXY。
		if os.Getenv("ALL_PROXY") != "" {
			t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				if cd, ok := dialer.(proxy.ContextDialer); ok {
					return cd.DialContext(ctx, network, addr)
				}
				return dialer.Dial(network, addr)
			}
		} else {
			t.Proxy = http.ProxyFromEnvironment
			t.DialContext = (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext
		}
		transport = t
	})
	return transport
}

// Client 返回指定超时时间的共享 HTTP 客户端
func Client(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	mu.RLock()
	c := clients[timeout]
	mu.RUnlock()
	if c != nil {
		return c
	}

	mu.Lock()
	defer mu.Unlock()
	if c = clients[timeout]; c != nil {
		return c
	}
	c = &http.Client{Transport: baseTransport(), Timeout: timeout}
	clients[timeout] = c
	return c
}

// Dialer 返回遵循代理环境变量的 TCP 拨号器（WHOIS 43 端口使用）
func Dialer() proxy.Dialer { return proxy.FromEnvironment() }

// DialContext 使用代理拨号器建立带 context 的 TCP 连接
func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d := proxy.FromEnvironment()
	if cd, ok := d.(proxy.ContextDialer); ok {
		return cd.DialContext(ctx, network, address)
	}

	type result struct {
		conn net.Conn
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		c, err := d.Dial(network, address)
		ch <- result{c, err}
	}()
	select {
	case r := <-ch:
		return r.conn, r.err
	case <-ctx.Done():
		go func() {
			// 避免拨号 goroutine 泄漏：连接建立后立刻关闭
			if r := <-ch; r.conn != nil {
				_ = r.conn.Close()
			}
		}()
		return nil, ctx.Err()
	}
}
