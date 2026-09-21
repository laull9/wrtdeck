package api

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// proxy_headers 是反向代理会补上的来源标记
var proxy_headers = []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Real-Ip"}

// client_ip 根据可信反代配置提取客户端真实 IP，未配置或非可信来源时使用连接远端 IP
func client_ip(r *http.Request, trusted_proxies []string) string {
	remote := remote_ip(r.RemoteAddr)
	if remote == nil {
		return "unknown"
	}
	if !is_trusted_proxy(remote, trusted_proxies) {
		return remote.String()
	}

	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			part := strings.TrimSpace(parts[i])
			ip := net.ParseIP(part)
			if ip == nil {
				continue
			}
			if !is_trusted_proxy(ip, trusted_proxies) {
				return ip.String()
			}
		}
		if first := strings.TrimSpace(parts[0]); first != "" {
			if ip := net.ParseIP(first); ip != nil {
				return ip.String()
			}
		}
	}

	if xri := strings.TrimSpace(r.Header.Get("X-Real-Ip")); xri != "" {
		if ip := net.ParseIP(xri); ip != nil {
			return ip.String()
		}
	}
	return remote.String()
}

// is_trusted_proxy 判断给定 IP 是否属于受信任的反向代理列表
func is_trusted_proxy(ip net.IP, trusted_proxies []string) bool {
	if ip == nil || len(trusted_proxies) == 0 {
		return false
	}
	for _, raw := range trusted_proxies {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if strings.Contains(raw, "/") {
			_, subnet, err := net.ParseCIDR(raw)
			if err == nil && subnet.Contains(ip) {
				return true
			}
			continue
		}
		target := net.ParseIP(raw)
		if target != nil && target.Equal(ip) {
			return true
		}
	}
	return false
}

// is_private_ip 判断 IP 是否属于私有地址、回环或本地链路单播
func is_private_ip(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// is_loopback_addr 判断地址字符串是否为回环地址
func is_loopback_addr(remote string) bool {
	ip := remote_ip(remote)
	return ip != nil && ip.IsLoopback()
}

// remote_ip 从 RemoteAddr 中解析出 IP，解析失败返回 nil
func remote_ip(remote string) net.IP {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	return net.ParseIP(strings.Trim(host, "[]"))
}

// arrived_via_proxy 判断请求头中是否携带了反向代理标记
func arrived_via_proxy(r *http.Request) bool {
	for _, name := range proxy_headers {
		if strings.TrimSpace(r.Header.Get(name)) != "" {
			return true
		}
	}
	return false
}

// host_allowed 校验 Host 是否在允许的主机名列表中，为空时允许所有合法格式的主机名
func host_allowed(hostport string, allowed_hosts []string) bool {
	host := strings.ToLower(hostport)
	if parsed_host, _, err := net.SplitHostPort(hostport); err == nil {
		host = strings.ToLower(parsed_host)
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return false
	}
	if strings.ContainsAny(host, "/\\ \t\r\n;\"'") {
		return false
	}
	if len(allowed_hosts) == 0 {
		return true
	}
	for _, item := range allowed_hosts {
		target := strings.ToLower(strings.TrimSpace(item))
		if target == "" {
			continue
		}
		if target == host || (strings.HasPrefix(target, "*.") && strings.HasSuffix(host, target[1:])) {
			return true
		}
	}
	return host_is_local_name(host)
}

// host_is_local_name 判断主机名是否指向本机或受信任的局域网域名
func host_is_local_name(hostport string) bool {
	host := strings.ToLower(hostport)
	if parsed_host, _, err := net.SplitHostPort(hostport); err == nil {
		host = strings.ToLower(parsed_host)
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return false
	}
	if net.ParseIP(host) != nil || host == "localhost" {
		return true
	}
	if name, err := os.Hostname(); err == nil && name != "" {
		name = strings.ToLower(name)
		if host == name || strings.HasPrefix(host, name+".") {
			return true
		}
	}
	for _, suffix := range []string{".lan", ".local", ".home", ".home.arpa", ".internal"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// same_origin 判断请求是否与服务同源，第二项返回是否存在来源头
func same_origin(r *http.Request) (bool, bool) {
	origin := request_origin(r)
	if origin == "" {
		return false, false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false, true
	}
	return strings.EqualFold(parsed.Host, r.Host), true
}

// same_origin_reason 将同源判定结果转换为用于审计的文字说明
func same_origin_reason(present, ok bool) string {
	if !present {
		return "缺少 Origin/Referer"
	}
	if !ok {
		return "来源与服务不同源"
	}
	return ""
}

// request_origin 取出来源页面的 协议://主机，优先读 Origin，退回 Referer
func request_origin(r *http.Request) string {
	if origin := r.Header.Get("Origin"); origin != "" {
		return origin
	}
	referer := r.Header.Get("Referer")
	if referer == "" {
		return ""
	}
	parsed, err := url.Parse(referer)
	if err != nil {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

// human_wait 把等待时长格式化为中文文字
func human_wait(d time.Duration) string {
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	return d.Round(time.Minute).String()
}
