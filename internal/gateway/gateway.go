// Package gateway 把面板的 API 交给设备既有 Web 服务器上的 CGI 转交给面板本体。
//
// 背景：OpenWrt 的 uhttpd 能服务静态文件与 CGI，但不能反向代理。
// 面板于是把前端资源导出到 Web 根目录、把 API 交给一个 CGI 网关转发，
// 浏览器始终只与 LuCI 同一个源打交道：同一个域名、同一个端口、同一套加密方式。
// 这样就不会再出现「页面跑在 HTTPS 域名上，却要浏览器直连 IP:8080」的错配。
//
// 网关还负责认出已经登录 LuCI 的浏览器：校验通过后替它注入面板凭据，
// 用户从 LuCI 点进来就不必再输一次面板口令。
package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// api_prefix 是网关唯一转发的路径前缀。
// 只放行面板的 API：网关一旦能被当作通用代理，就等于给设备开了一个没有鉴权的跳板。
const api_prefix = "/api/v1"

// blocked_paths 是网关不转发、直接拒绝的接口。
//
// /api/v1/session/handoff 只允许设备本机申请登录码，而网关进程恰好就跑在设备本机、
// 从回环发起请求——放它过去等于让任何一个够得着设备 Web 端口的人都能凭空拿到
// 一张登录码，再换成正儿八经的会话凭据。这条捷径必须堵死：
// 需要登录码的是 LuCI 的 rpcd 后端，它自己就能直接访问回环上的面板。
var blocked_paths = map[string]string{
	"/api/v1/session/handoff": "登录码只能由设备本机申请，网关不代传",
}

// Options 描述一次网关运行所需的全部参数
type Options struct {
	// Upstream 是面板本体的地址，通常是 http://127.0.0.1:8080
	Upstream string
	// Token 是校验通过后注入的 API Token
	Token string
	// VerifyLuci 为真时校验浏览器带来的 LuCI 会话
	VerifyLuci bool
	// Inject 为真时在 LuCI 会话有效的前提下注入 Token
	Inject bool
	// Timeout 是等待面板本体回应的最长时间（自发起请求到响应头到达）。
	// 响应体本身不设上限：实时事件流是长连接，一刀切断反而比不切更糟。
	Timeout time.Duration
	// UbusBin 是 ubus 命令行路径，留空时按 ubus 处理
	UbusBin string
	// Stdin / Stdout 便于测试注入，真机上是 CGI 的标准输入输出
	Stdin  io.Reader
	Stdout io.Writer
	// Logger 用于记录异常，默认丢弃
	Logger *log.Logger
}

// Run 处理一次 CGI 调用：还原请求、转发、写回响应
func Run(opts Options) error {
	opts.fill_defaults()
	req, err := read_request(opts)
	if err != nil {
		var denied denied_error
		if errors.As(err, &denied) {
			opts.log("按策略拒绝：%s", denied.Error())
			return write_error(opts.Stdout, http.StatusForbidden, "gateway_denied", denied.Error())
		}
		return write_error(opts.Stdout, http.StatusBadRequest, "bad_gateway_request", err.Error())
	}

	if err = authorize(req, env("HTTP_COOKIE"), opts); err != nil {
		opts.log("鉴权处理失败: %v", err)
	}

	resp, err := forward(new_client(opts.Timeout), req, opts)
	if err != nil {
		opts.log("转发到面板本体失败: %v", err)
		return write_error(opts.Stdout, http.StatusBadGateway, "upstream_unreachable",
			"面板本体当前不可达，请确认 wrtdeck 服务正在运行")
	}
	defer resp.Body.Close()
	return write_response(opts.Stdout, resp)
}

// fill_defaults 补齐可省略的字段，避免调用方为每个默认值都写一遍
func (o *Options) fill_defaults() {
	if o.Timeout <= 0 {
		o.Timeout = 15 * time.Second
	}
	if o.UbusBin == "" {
		o.UbusBin = "ubus"
	}
	if o.Stdout == nil {
		o.Stdout = io.Discard
	}
	if o.Stdin == nil {
		o.Stdin = strings.NewReader("")
	}
}

// log 在配置了日志器时输出一行诊断信息
func (o Options) log(format string, args ...any) {
	if o.Logger != nil {
		o.Logger.Printf(format, args...)
	}
}

// read_request 从 CGI 环境变量与标准输入还原出一次 HTTP 请求
func read_request(opts Options) (*http.Request, error) {
	method := strings.TrimSpace(env("REQUEST_METHOD"))
	if method == "" {
		method = http.MethodGet
	}
	target, err := request_target()
	if err != nil {
		return nil, err
	}
	raw_url := opts.Upstream + target
	if query := env("QUERY_STRING"); query != "" {
		raw_url += "?" + query
	}

	body, err := request_body(opts.Stdin)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(method, raw_url, body)
	if err != nil {
		return nil, fmt.Errorf("构造上游请求失败: %w", err)
	}
	copy_env_headers(req)
	// Host 保持浏览器原本请求的那个：面板要靠它判断同源，
	// 也要靠它把 CSP 的 frame-ancestors 放行到真正的前端页面。
	if host := env("HTTP_HOST"); host != "" {
		req.Host = host
	}
	mark_proxied(req)
	return req, nil
}

// mark_proxied 留下「这次请求经过了代理」的痕迹。
//
// 面板对「来源是不是设备本机」有几处判断（登录码只许本机申请、初始口令期间
// 只许内网登录），而经网关转发后来源一律显示为回环，那些判断会全部失真。
// 面板据此把头存在与否当作「不能再相信 RemoteAddr」的信号，因此这里必须写。
func mark_proxied(req *http.Request) {
	if client := env("REMOTE_ADDR"); client != "" {
		req.Header.Set("X-Forwarded-For", client)
	}
	if host := env("HTTP_HOST"); host != "" {
		req.Header.Set("X-Forwarded-Host", host)
	}
	req.Header.Set("X-Forwarded-Proto", request_scheme())
}

// request_scheme 判断浏览器与设备 Web 服务器之间用的是什么协议。
// 认这个头是因为反向代理后面的设备 Web 服务器看到的永远是明文，
// 只有代理自己知道前端那一段是不是加密的。
func request_scheme() string {
	if forwarded := env("HTTP_X_FORWARDED_PROTO"); forwarded != "" {
		return forwarded
	}
	for _, name := range []string{"HTTPS", "REQUEST_SCHEME"} {
		switch strings.ToLower(env(name)) {
		case "on", "1", "https":
			return "https"
		}
	}
	return "http"
}

// request_target 取出要转发到面板本体的路径，并确认它落在面板 API 之下
func request_target() (string, error) {
	target := strings.TrimSpace(env("PATH_INFO"))
	if target == "" {
		target = strip_prefix_uri(env("REQUEST_URI"))
	}
	if target == "" {
		return "", fmt.Errorf("请求里没有可转发的路径")
	}
	if !strings.HasPrefix(target, "/") {
		target = "/" + target
	}
	if target != api_prefix && !strings.HasPrefix(target, api_prefix+"/") {
		return "", fmt.Errorf("网关只转发面板 API（%s），收到的是 %q", api_prefix, target)
	}
	if reason, blocked := blocked_paths[target]; blocked {
		return "", denied_error{reason}
	}
	return target, nil
}

// denied_error 表示请求本身合法、但被网关的策略拒绝，
// 与「请求根本看不懂」区分开，好让前端拿到 403 而不是 400。
type denied_error struct{ reason string }

// Error 实现 error 接口
func (e denied_error) Error() string { return e.reason }

// strip_prefix_uri 在没有 PATH_INFO 时从 REQUEST_URI 里剥掉查询串
func strip_prefix_uri(uri string) string {
	if uri == "" {
		return ""
	}
	if index := strings.IndexByte(uri, '?'); index >= 0 {
		return uri[:index]
	}
	return uri
}

// request_body 按 CONTENT_LENGTH 截出请求体，长度不合法时视为空体
func request_body(stdin io.Reader) (io.Reader, error) {
	raw := strings.TrimSpace(env("CONTENT_LENGTH"))
	if raw == "" {
		return nil, nil
	}
	length, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || length < 0 {
		return nil, fmt.Errorf("CONTENT_LENGTH 不是合法长度：%q", raw)
	}
	if length == 0 {
		return nil, nil
	}
	return io.LimitReader(stdin, length), nil
}

// copy_env_headers 把 CGI 的 HTTP_* 变量搬成请求头。
//
// 只搬面板用得到的那些：面板不认识的头（尤其是浏览器的 Cookie）
// 一概不带过去，减少凭据在进程之间无谓流转。
func copy_env_headers(req *http.Request) {
	keep := []string{"Content-Type", "Accept", "Accept-Language", "Origin", "Referer", "X-Requested-With"}
	for _, name := range keep {
		value := env("HTTP_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")))
		if name == "Content-Type" {
			value = env("CONTENT_TYPE")
		}
		if value != "" {
			req.Header.Set(name, value)
		}
	}
	// 浏览器自带的凭据原样带上：网关只负责补，不负责换
	if auth := env("HTTP_AUTHORIZATION"); auth != "" {
		req.Header.Set("Authorization", auth)
	}
	// 不把 Accept-Encoding 传下去，避免上游压缩后还要在这里解压一次
	req.Header.Del("Accept-Encoding")
}

// write_response 把上游响应按 CGI 规范写到标准输出
func write_response(out io.Writer, resp *http.Response) error {
	if err := write_status(out, resp.StatusCode); err != nil {
		return err
	}
	for name, values := range resp.Header {
		if is_hop_by_hop(name) {
			continue
		}
		for _, value := range values {
			if _, err := fmt.Fprintf(out, "%s: %s\r\n", name, value); err != nil {
				return err
			}
		}
	}
	if _, err := io.WriteString(out, "\r\n"); err != nil {
		return err
	}
	_, err := io.Copy(out, resp.Body)
	return err
}

// write_status 只在非 200 时写 Status 行，其余情况按 CGI 约定默认为 200
func write_status(out io.Writer, status int) error {
	if status == http.StatusOK {
		return nil
	}
	_, err := fmt.Fprintf(out, "Status: %d %s\r\n", status, http.StatusText(status))
	return err
}

// is_hop_by_hop 判断是否为逐跳首部，这些头只对相邻两个节点有意义，不能往下传
func is_hop_by_hop(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
		"Te", "Trailer", "Transfer-Encoding", "Upgrade":
		return true
	}
	return false
}

// write_error 输出一个与面板同格式的错误响应，前端因此能照常展示原因
func write_error(out io.Writer, status int, code, message string) error {
	body, err := json.Marshal(map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
	if err != nil {
		return err
	}
	body = append(body, '\n')

	var buffer bytes.Buffer
	if err = write_status(&buffer, status); err != nil {
		return err
	}
	fmt.Fprintf(&buffer, "Content-Type: application/json; charset=utf-8\r\n")
	fmt.Fprintf(&buffer, "Cache-Control: no-store\r\n")
	fmt.Fprintf(&buffer, "Content-Length: %d\r\n\r\n", len(body))
	buffer.Write(body)
	_, err = out.Write(buffer.Bytes())
	return err
}

// env 读取环境变量，顺带把空白清理掉
func env(name string) string {
	return strings.TrimSpace(os.Getenv(name))
}
