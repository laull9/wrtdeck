// Package api 提供 Dashboard 所需的全部 HTTP 接口与 SSE 事件流。
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"wrtdeck/internal/config"
)

// error_body 是统一的错误响应结构
type error_body struct {
	Error error_detail `json:"error"`
}

// error_detail 描述一次失败的细节
type error_detail struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

// write_json 输出一个 JSON 响应
func write_json(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("写出响应失败: %v", err)
	}
}

// write_error 输出统一格式的错误响应
func write_error(w http.ResponseWriter, status int, code, message string) {
	write_json(w, status, error_body{Error: error_detail{Message: message, Code: code}})
}

// status_recorder 记录响应状态码，供访问日志使用
type status_recorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader 记录状态码后透传
func (r *status_recorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Write 在未显式写状态码时按 200 记录
func (r *status_recorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(data)
}

// Flush 透传 Flush，保证 SSE 能即时下发
func (r *status_recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// middleware 把若干中间件按顺序套在处理器外层
func middleware(handler http.Handler, layers ...func(http.Handler) http.Handler) http.Handler {
	for i := len(layers) - 1; i >= 0; i-- {
		handler = layers[i](handler)
	}
	return handler
}

// with_logging 打印访问日志并捕获 panic
func with_logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &status_recorder{ResponseWriter: w}
		defer func() {
			if err := recover(); err != nil {
				log.Printf("panic 恢复: %v", err)
				if rec.status == 0 {
					write_error(rec, http.StatusInternalServerError, "internal", "服务内部错误")
				}
			}
			log.Printf("%s %s %d %dms", r.Method, r.URL.Path, rec.status, time.Since(start).Milliseconds())
		}()
		next.ServeHTTP(rec, r)
	})
}

// with_cors 仅在开发模式下放行跨域，方便直接访问 Vite 端口
func with_cors(enabled bool, next http.Handler) http.Handler {
	if !enabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, POST, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// with_limit 给请求体加上大小上限
func with_limit(max_bytes int, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, int64(max_bytes))
		}
		next.ServeHTTP(w, r)
	})
}

// 凭据角色。两种凭据都能访问全部接口，区别只在生命周期与来源：
// API Token 长期有效，供脚本与设备本机使用；会话凭据会过期，供浏览器登录使用。
const (
	role_api     = "api"
	role_session = "session"
)

// auth_ctx 描述本次请求用的是哪种凭据
type auth_ctx struct {
	role  string
	token string
}

// auth_key 是请求上下文里存放 auth_ctx 的键
type auth_key_type struct{}

// auth_from 取出本次请求的凭据信息，未鉴权时返回空值
func auth_from(r *http.Request) auth_ctx {
	ctx, _ := r.Context().Value(auth_key_type{}).(auth_ctx)
	return ctx
}

// with_auth 校验 Bearer 凭据，health 与静态资源不参与校验。
//
// 凭据可以是 API Token，也可以是登录后拿到的会话凭据。两者都校验通过才能继续。
// 另外还有一条状态约束：仍然使用初始口令时，会话凭据只能用于改口令与登出，
// 否则「首启默认口令」就等同于一个没有任何限制的后门。
func (s *Server) with_auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Auth.Disabled || !strings.HasPrefix(r.URL.Path, "/api/") || open_path(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		token := bearer_token(r)
		// SSE 无法自定义请求头时允许通过查询参数传 Token。
		// 访问日志只记路径不记查询串，因此凭据不会因此落进日志。
		if token == "" && r.URL.Path == "/api/v1/events" {
			token = r.URL.Query().Get("token")
		}
		if token == "" {
			write_error(w, http.StatusUnauthorized, "unauthorized", "未登录，请先登录面板")
			return
		}

		role := ""
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.secrets.Token())) == 1 {
			role = role_api
		} else if _, ok := s.sessions.lookup(token); ok {
			role = role_session
		}
		if role == "" {
			write_error(w, http.StatusUnauthorized, "unauthorized", "凭据无效或已过期，请重新登录")
			return
		}
		if role == role_session && s.secrets.MustChange() && !password_change_path(r.URL.Path) {
			write_error(w, http.StatusForbidden, "password_change_required",
				"面板仍在使用初始口令，请先修改口令")
			return
		}

		ctx := context.WithValue(r.Context(), auth_key_type{}, auth_ctx{role: role, token: token})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// open_path 列出自身就是凭据、因此不走 Bearer 校验的接口。
// health 只回元信息；login 认口令；redeem 认一次性登录码，且两者都在内部校验来源。
func open_path(path string) bool {
	switch path {
	case "/api/v1/health", "/api/v1/session/login", "/api/v1/session/redeem":
		return true
	}
	return false
}

// password_change_path 列出初始口令状态下仍然放行的接口。
// 会话必须能问到自己的状态、能改口令、能登出，否则用户会被锁在门外无路可走。
func password_change_path(path string) bool {
	switch path {
	case "/api/v1/session/me", "/api/v1/session/password", "/api/v1/session/logout":
		return true
	}
	return false
}

// with_host_validation 校验请求中的 Host 头是否合法
func (s *Server) with_host_validation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.dev {
			next.ServeHTTP(w, r)
			return
		}
		if !host_allowed(r.Host, s.cfg.Auth.AllowedHosts) {
			write_error(w, http.StatusBadRequest, "invalid_host", "请求的 Host 头不受信任或格式非法")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// with_csrf_protection 检查修改类接口的来源，拦截跨站伪造请求
func (s *Server) with_csrf_protection(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.dev || !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodDelete:
			if ok, present := same_origin(r); present && !ok {
				write_error(w, http.StatusForbidden, "cross_origin_forbidden", "跨站来源请求被拒绝")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// with_security_headers 给所有响应加上安全头，按「面板可能直接暴露在公网」来配。
func (s *Server) with_security_headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		// 点击劫持用 CSP 挡：面板要被 LuCI 嵌进另一端口的页面，同源策略拦不住它，
		// 因此放行同主机的任意端口，其余来源一律不许套框架。
		// 这里不用 X-Frame-Options：它只支持同源或全放行，会连带把 LuCI 的嵌入也挡掉。
		host := r.Host
		if parsed_host, _, err := net.SplitHostPort(r.Host); err == nil {
			host = parsed_host
		}
		host = strings.Trim(host, "[]")
		csp := "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; " +
			"script-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none';"
		if host != "" && !strings.ContainsAny(host, "/\\ \t\r\n;\"'") {
			csp += " frame-ancestors 'self' http://" + host + ":* https://" + host + ":*"
		} else {
			csp += " frame-ancestors 'self'"
		}
		header.Set("Content-Security-Policy", csp)

		// 只有确实走进 TLS 才发 HSTS：明文监听上发 HSTS 会把用户自己锁在门外
		if request_is_secure(r) {
			header.Set("Strict-Transport-Security", "max-age=15552000")
		}
		// 接口响应里有状态与凭据，禁止任何中间层缓存
		if strings.HasPrefix(r.URL.Path, "/api/") {
			header.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// request_is_secure 判断这次请求是否经 TLS 到达，兼容反向代理转发的场景
func request_is_secure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// bearer_token 从 Authorization 头中取出 Token
func bearer_token(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if len(header) < 7 || !strings.EqualFold(header[:7], "bearer ") {
		return ""
	}
	return strings.TrimSpace(header[7:])
}

// ensure 让 config 引用在编译期保持有效
var _ = config.Default
