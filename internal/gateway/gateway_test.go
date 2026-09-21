package gateway

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// upstream 起一个假的面板本体，把收到的请求头回敬给调用方，
// 这样测试就能直接断言网关到底转发了什么过去。
func upstream(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		for _, name := range []string{"Authorization", "X-Forwarded-For", "X-Forwarded-Host", "Origin", "Cookie"} {
			w.Header().Set("X-Echo-"+name, r.Header.Get(name))
		}
		if testing.Verbose() {
			log.Printf("上游收到请求头：%v", r.Header)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"path":"` + r.URL.Path + `","query":"` + r.URL.RawQuery + `","host":"` + r.Host + `"}`))
	}))
	t.Cleanup(server.Close)
	return server
}

// fake_ubus 造一个假的 ubus 命令，用来固定「会话校验」这一步的结果
func fake_ubus(t *testing.T, output string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ubus")
	script := "#!/bin/sh\nprintf '%s' '" + output + "'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("写入假 ubus 失败: %v", err)
	}
	return path
}

// run_cgi 在指定环境下跑一次网关，返回模拟 CGI 环境与输出
func run_cgi(t *testing.T, env map[string]string, stdin string, opts Options) string {
	t.Helper()
	for key, value := range env {
		t.Setenv(key, value)
	}
	opts.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	opts.Stdout = &out
	if opts.Timeout == 0 {
		opts.Timeout = 5_000_000_000
	}
	if err := Run(opts); err != nil {
		t.Fatalf("网关运行失败: %v", err)
	}
	return out.String()
}

// header_of 从 CGI 输出里取出一个响应头
func header_of(raw, name string) string {
	for _, line := range strings.Split(raw, "\r\n") {
		if line == "" {
			break
		}
		if key, value, found := strings.Cut(line, ":"); found && strings.EqualFold(strings.TrimSpace(key), name) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// body_of 取出 CGI 输出里的响应体
func body_of(raw string) string {
	if index := strings.Index(raw, "\r\n\r\n"); index >= 0 {
		return raw[index+4:]
	}
	return raw
}

// 非 API 路径一律拒绝，网关不能变成通用代理
func TestRejectsNonAPIPath(t *testing.T) {
	out := run_cgi(t, map[string]string{"PATH_INFO": "/etc/passwd", "REQUEST_METHOD": "GET"}, "", Options{})
	if !strings.Contains(out, "400 Bad Request") {
		t.Fatalf("期望 400，实际输出：%s", out)
	}
}

// 登录码接口必须被网关挡住：它只许设备本机申请，而网关自己就跑在本机
func TestBlocksHandoff(t *testing.T) {
	out := run_cgi(t, map[string]string{"PATH_INFO": "/api/v1/session/handoff", "REQUEST_METHOD": "POST"}, "{}", Options{})
	if !strings.Contains(out, "403 Forbidden") {
		t.Fatalf("期望 403，实际输出：%s", out)
	}
	if !strings.Contains(out, "登录码只能由设备本机申请") {
		t.Fatalf("拒绝原因没有说清楚：%s", out)
	}
}

// 没有 luci 会话时不得注入凭据，且必须留下代理标记
func TestNoInjectionWithoutLuciSession(t *testing.T) {
	panel := upstream(t)
	out := run_cgi(t, map[string]string{
		"PATH_INFO":      "/api/v1/dashboard",
		"REQUEST_METHOD": "GET",
		"REMOTE_ADDR":    "203.0.113.7",
		"HTTP_HOST":      "luci.example.com",
		"HTTP_COOKIE":    "sysauth_https=deadbeef",
	}, "", Options{Upstream: panel.URL, Token: "secret-token", VerifyLuci: true, Inject: true})

	if got := header_of(out, "X-Echo-Authorization"); got != "" {
		t.Fatalf("会话无效时不应注入凭据，实际注入了 %q", got)
	}
	if got := header_of(out, "X-Echo-X-Forwarded-For"); got != "203.0.113.7" {
		t.Fatalf("缺少代理来源标记，实际为 %q", got)
	}
	if got := header_of(out, "X-Echo-X-Forwarded-Host"); got != "luci.example.com" {
		t.Fatalf("缺少原始 Host 标记，实际为 %q", got)
	}
}

// luci 会话有效时替浏览器注入 API Token，实现从 LuCI 进来免登录
func TestInjectsTokenWithValidLuciSession(t *testing.T) {
	panel := upstream(t)
	ubus := fake_ubus(t, `{"values":{"username":"root"}}`)
	out := run_cgi(t, map[string]string{
		"PATH_INFO":      "/api/v1/dashboard",
		"REQUEST_METHOD": "GET",
		"REMOTE_ADDR":    "203.0.113.7",
		"HTTP_HOST":      "luci.example.com",
		"HTTP_COOKIE":    "sysauth_https=abc123",
	}, "", Options{Upstream: panel.URL, Token: "secret-token", VerifyLuci: true, Inject: true, UbusBin: ubus})

	if got := header_of(out, "X-Echo-Authorization"); got != "Bearer secret-token" {
		t.Fatalf("会话有效时应注入凭据，实际为 %q", got)
	}
}

// 浏览器自带凭据时原样转发，网关不做替换
func TestKeepsClientCredential(t *testing.T) {
	panel := upstream(t)
	out := run_cgi(t, map[string]string{
		"PATH_INFO":          "/api/v1/dashboard",
		"REQUEST_METHOD":     "GET",
		"HTTP_HOST":          "luci.example.com",
		"HTTP_AUTHORIZATION": "Bearer client-token",
	}, "", Options{Upstream: panel.URL, Token: "secret-token", VerifyLuci: true, Inject: true})

	if got := header_of(out, "X-Echo-Authorization"); got != "Bearer client-token" {
		t.Fatalf("应保留浏览器自带的凭据，实际为 %q", got)
	}
}

// 浏览器的 Cookie 不带进面板，减少凭据在进程之间无谓流转
func TestDropsBrowserCookie(t *testing.T) {
	panel := upstream(t)
	out := run_cgi(t, map[string]string{
		"PATH_INFO":      "/api/v1/health",
		"REQUEST_METHOD": "GET",
		"HTTP_HOST":      "luci.example.com",
		"HTTP_COOKIE":    "sysauth_https=abc123",
	}, "", Options{Upstream: panel.URL})

	if got := header_of(out, "X-Echo-Cookie"); got != "" {
		t.Fatalf("不应把浏览器 Cookie 转发给面板，实际为 %q", got)
	}
}

// 校验 LuCI 会话这一关默认开着，关掉时不允许再注入
func TestRefusesInjectionWhenVerificationOff(t *testing.T) {
	panel := upstream(t)
	out := run_cgi(t, map[string]string{
		"PATH_INFO":      "/api/v1/dashboard",
		"REQUEST_METHOD": "GET",
		"HTTP_HOST":      "luci.example.com",
		"HTTP_COOKIE":    "sysauth_https=abc123",
	}, "", Options{Upstream: panel.URL, Token: "secret-token", VerifyLuci: false, Inject: true})

	if got := header_of(out, "X-Echo-Authorization"); got != "" {
		t.Fatalf("关闭校验后不得注入凭据，实际注入了 %q", got)
	}
}

// 查询串、方法与请求体都要完整带到面板本体
func TestForwardsQueryAndBody(t *testing.T) {
	var seen_method, seen_body, seen_query string
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(r.Body)
		seen_method, seen_body, seen_query = r.Method, body.String(), r.URL.RawQuery
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(panel.Close)

	run_cgi(t, map[string]string{
		"PATH_INFO":      "/api/v1/session/login",
		"REQUEST_METHOD": "POST",
		"QUERY_STRING":   "flag=1",
		"CONTENT_TYPE":   "application/json",
		"CONTENT_LENGTH": "16",
		"HTTP_HOST":      "luci.example.com",
	}, `{"password":"x"}`, Options{Upstream: panel.URL})

	if seen_method != "POST" || seen_query != "flag=1" || seen_body != `{"password":"x"}` {
		t.Fatalf("转发不完整：method=%q query=%q body=%q", seen_method, seen_query, seen_body)
	}
}

// 会话数据里没有用户名就当作没登录
func TestSessionLoggedInDetection(t *testing.T) {
	cases := []struct {
		raw    string
		expect bool
	}{
		{`{"values":{"username":"root"}}`, true},
		{`{"username":"root"}`, true},
		{`{"values":{"username":"  "}}`, false},
		{`{"values":{}}`, false},
		{`{"acls":{"unauthenticated":{}}}`, false},
		{`not json`, false},
	}
	for _, item := range cases {
		if got := session_is_logged_in([]byte(item.raw)); got != item.expect {
			t.Fatalf("session_is_logged_in(%s) = %v，期望 %v", item.raw, got, item.expect)
		}
	}
}

// Cookie 里的会话标识要能按优先级取出来
func TestSessionIDPicksHTTPSFirst(t *testing.T) {
	if got := session_id("foo=1; sysauth_http=plain; sysauth_https=secure"); got != "secure" {
		t.Fatalf("应优先取 sysauth_https，实际 %q", got)
	}
	if got := session_id("sysauth=legacy"); got != "legacy" {
		t.Fatalf("应能退回通用的 sysauth，实际 %q", got)
	}
	if got := session_id("other=1"); got != "" {
		t.Fatalf("没有会话 cookie 时应返回空串，实际 %q", got)
	}
}

// 响应头里的逐跳首部不能往下传
func TestStripsHopByHopHeaders(t *testing.T) {
	if !is_hop_by_hop("Connection") || !is_hop_by_hop("transfer-encoding") {
		t.Fatal("逐跳首部应当被识别出来")
	}
	if is_hop_by_hop("Content-Type") {
		t.Fatal("Content-Type 不是逐跳首部，不该被过滤")
	}
}

// 上游不可达时给出可读的 502，而不是空响应
func TestUnreachableUpstream(t *testing.T) {
	for key, value := range map[string]string{"PATH_INFO": "/api/v1/health", "REQUEST_METHOD": "GET"} {
		t.Setenv(key, value)
	}
	var out bytes.Buffer
	err := Run(Options{
		Upstream: "http://127.0.0.1:1",
		Stdin:    strings.NewReader(""),
		Stdout:   &out,
		Timeout:  2_000_000_000,
		Logger:   log.New(&bytes.Buffer{}, "", 0),
	})
	if err != nil {
		t.Fatalf("网关不应向上抛错: %v", err)
	}
	t.Logf("CGI 输出：%q", out.String())
	if !strings.Contains(out.String(), "502 Bad Gateway") {
		t.Fatalf("期望 502，实际输出：%s", out.String())
	}
}
