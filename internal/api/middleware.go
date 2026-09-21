// Package api 提供 Dashboard 所需的全部 HTTP 接口与 SSE 事件流。
package api

import (
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"owdash/internal/config"
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

// with_auth 校验 Bearer Token，health 与静态资源不参与校验
func (s *Server) with_auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Auth.Disabled || !strings.HasPrefix(r.URL.Path, "/api/") ||
			r.URL.Path == "/api/v1/health" {
			next.ServeHTTP(w, r)
			return
		}
		token := bearer_token(r)
		// SSE 无法自定义请求头时允许通过查询参数传 Token
		if token == "" && r.URL.Path == "/api/v1/events" {
			token = r.URL.Query().Get("token")
		}
		expected := s.secrets.Token()
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
			write_error(w, http.StatusUnauthorized, "unauthorized", "缺少或错误的 Bearer Token")
			return
		}
		next.ServeHTTP(w, r)
	})
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
