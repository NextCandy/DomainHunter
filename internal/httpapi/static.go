package httpapi

import (
	"context"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"DomainHunter/web"
)

// staticHandler 提供构建后的前端静态资源
func (s *Server) staticHandler() http.Handler {
	assets, err := fs.Sub(web.Dist(), "assets")
	if err != nil {
		return http.NotFoundHandler()
	}
	return http.StripPrefix("/assets/", cacheForever(http.FileServer(http.FS(assets))))
}

// cacheForever 构建产物的文件名带内容哈希，可以长期缓存
func cacheForever(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		next.ServeHTTP(w, r)
	})
}

// handleIndex 单页应用入口：所有前端路由都返回 index.html
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	// API 路径没有匹配到任何 handler 时应返回 404，而不是前端页面
	if strings.HasPrefix(r.URL.Path, "/api/") {
		s.writeError(w, r, http.StatusNotFound, "接口不存在")
		return
	}
	s.serveIndex(w, r)
}

// handleLoginPage 登录页同样由前端应用渲染
func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		s.writeError(w, r, http.StatusMethodNotAllowed, "不允许的请求方法")
		return
	}
	s.serveIndex(w, r)
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	// 直接命中的静态文件（favicon 等）优先返回
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name != "" && !strings.Contains(name, "..") {
		if data, err := fs.ReadFile(web.Dist(), name); err == nil {
			http.ServeContent(w, r, name, time.Time{}, strings.NewReader(string(data)))
			return
		}
	}

	index, err := fs.ReadFile(web.Dist(), "index.html")
	if err != nil {
		http.Error(w, "前端资源未构建，请先执行 npm run build", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(index)
}

func timeoutContext(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}
