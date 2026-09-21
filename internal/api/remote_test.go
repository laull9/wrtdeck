package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestClientIP 测试在配置可信代理与否时对 X-Forwarded-For 的解析
func TestClientIP(t *testing.T) {
	proxies := []string{"127.0.0.1", "10.0.0.0/8"}

	// 来源不受信任时忽略 X-Forwarded-For
	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req1.RemoteAddr = "203.0.113.10:1234"
	req1.Header.Set("X-Forwarded-For", "198.51.100.20")
	if got := client_ip(req1, proxies); got != "203.0.113.10" {
		t.Fatalf("非可信代理来源应返回 RemoteAddr，得到 %s", got)
	}

	// 来源受信任时解析 X-Forwarded-For 最左侧未受信任 IP
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req2.RemoteAddr = "127.0.0.1:1234"
	req2.Header.Set("X-Forwarded-For", "198.51.100.20, 10.0.0.5")
	if got := client_ip(req2, proxies); got != "198.51.100.20" {
		t.Fatalf("可信代理来源应提取客户端真实 IP，得到 %s", got)
	}

	// 来源受信任且仅有 X-Real-Ip 时正确读取
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req3.RemoteAddr = "10.1.2.3:1234"
	req3.Header.Set("X-Real-Ip", "198.51.100.30")
	if got := client_ip(req3, proxies); got != "198.51.100.30" {
		t.Fatalf("可信代理来源应支持 X-Real-Ip，得到 %s", got)
	}
}

// TestHostAllowed 测试主机名白名单、本机名与非法字符拦截
func TestHostAllowed(t *testing.T) {
	allowed := []string{"panel.example.com", "*.lan.example.com"}

	if !host_allowed("localhost:8080", allowed) {
		t.Fatalf("localhost 应当被放行")
	}
	if !host_allowed("127.0.0.1:8080", allowed) {
		t.Fatalf("127.0.0.1 应当被放行")
	}
	if !host_allowed("panel.example.com", allowed) {
		t.Fatalf("白名单中的域名应当被放行")
	}
	if !host_allowed("sub.lan.example.com:443", allowed) {
		t.Fatalf("通配符白名单匹配的主机应当被放行")
	}
	if !host_allowed("router.lan:8080", allowed) {
		t.Fatalf(".lan 本地受信任后缀应当被放行")
	}
	if host_allowed("evil.com", allowed) {
		t.Fatalf("未在白名单的公网域名应当被拦截")
	}
	if host_allowed("evil.com;script-src", allowed) {
		t.Fatalf("含非法字符的主机名应当被拦截")
	}

	// 白名单为空时放行合法主机名，拦截畸形主机名
	if !host_allowed("luci.example.com", nil) {
		t.Fatalf("白名单为空时合法域名应当被放行")
	}
	if host_allowed("bad\r\nhost", nil) {
		t.Fatalf("白名单为空时含非法字符的主机名仍应被拦截")
	}
}

// TestSameOrigin 测试同源校验与跨站来源检查
func TestSameOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://192.168.1.1:8080/api/v1/actions/test/run", nil)
	req.Host = "192.168.1.1:8080"

	// 缺少来源头
	ok, present := same_origin(req)
	if ok || present {
		t.Fatalf("无 Origin/Referer 时应当返回 present=false")
	}

	// 来源与 Host 一致
	req.Header.Set("Origin", "http://192.168.1.1:8080")
	ok, present = same_origin(req)
	if !ok || !present {
		t.Fatalf("同源 Origin 应当判定为 true")
	}

	// 跨站 Origin
	req.Header.Set("Origin", "http://evil.com")
	ok, present = same_origin(req)
	if ok || !present {
		t.Fatalf("跨站 Origin 应当判定为 false")
	}
}
