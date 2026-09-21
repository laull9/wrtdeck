package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"wrtdeck/internal/registry"
)

// http_transport 负责执行 HTTP 调用
type http_transport struct {
	client *http.Client
}

// init 注册 HTTP 传输实现并配置重定向防御
func init() {
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("重定向次数过多")
			}
			if is_blocked_target(req.URL.String()) {
				return errors.New("重定向目标受限，禁止访问云元数据端点")
			}
			return nil
		},
	}
	Register(&http_transport{client: client})
}

// is_blocked_target 拦截针对云厂商元数据服务（169.254.169.254）的请求
func is_blocked_target(target_url string) bool {
	parsed, err := url.Parse(target_url)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	return host == "169.254.169.254" || strings.EqualFold(host, "instance-data")
}

// Type 返回传输名称
func (h *http_transport) Type() string { return registry.TransportHTTP }

// Available 表示 HTTP 传输始终可用
func (h *http_transport) Available() bool { return true }

// Do 执行一次 HTTP 请求并返回响应体
func (h *http_transport) Do(ctx context.Context, spec registry.TransportSpec, opts Options) (*Result, error) {
	if spec.HTTP == nil {
		return nil, fmt.Errorf("缺少 transport.http 配置")
	}
	if is_blocked_target(spec.HTTP.URL) {
		return nil, fmt.Errorf("目标地址受限，禁止访问云元数据端点")
	}
	cfg := spec.HTTP
	method := strings.ToUpper(strings.TrimSpace(cfg.Method))
	if method == "" {
		method = http.MethodGet
	}

	var body io.Reader
	if cfg.Body != "" {
		body = bytes.NewBufferString(cfg.Body)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout_of(cfg.TimeoutMS, opts))
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, cfg.URL, body)
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}
	if cfg.Body != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "wrtdeck/1.0")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	limit := int64(opts.MaxBodyBytes)
	if limit <= 0 {
		limit = 256 * 1024
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("响应体超过上限 %d 字节", limit)
	}

	res := &Result{
		StatusCode: resp.StatusCode,
		Body:       truncate(data, opts.MaxBodyBytes),
		BodyText:   preview(data, 200),
	}
	if resp.StatusCode >= 400 {
		return res, fmt.Errorf("HTTP 状态码 %d", resp.StatusCode)
	}
	return res, nil
}
