package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// ubus_timeout 是询问 rpcd「这个会话登录了没有」的上限
const ubus_timeout = 3 * time.Second

// luci_cookie_names 是 LuCI 会话 cookie 的名字。
// 明文与加密访问各有一枚，加密优先，最后退回历史版本用过的通用名。
var luci_cookie_names = []string{"sysauth_https", "sysauth_http", "sysauth"}

// authorize 决定是否替这次请求补上凭据。
//
// 规则只有一条：**先证明调用方确实登录过 LuCI，再注入 Token**。
// 网关脚本挂在 CGI 目录下，不在 LuCI 的鉴权范围内，任何人都够得着它；
// 少了这一步校验，「校验通过就注入」会直接变成一条不用口令的后门。
//
// 校验不通过时不做任何降级处理，请求照样转发：面板自己还有一道鉴权，
// 前端也能靠一次性登录码完成交接，功能不会因此中断。
//
// **实机边界（2026-09-21 在 OpenWrt 25.12.5 上实测）**：这一段在原生 LuCI 上
// 实际走不到——LuCI 把会话 cookie 的作用域限定在它自己的路径下：
//
//	Set-Cookie: sysauth_https=<sid>; path=/cgi-bin/luci/; SameSite=strict; HttpOnly
//
// （见 /usr/share/ucode/luci/dispatcher.uc，path 取自 build_url()）。cookie 带
// path 前缀匹配，浏览器因此**不会**把它发给 /cgi-bin/<cgi_prefix>/wrtdeck-api，
// session_id 恒为空，probe_session 一次也不会被调用。实测请求时序：
//
//	GET  .../wrtdeck-api/api/v1/session/me   401   <- 这里没拿到 cookie
//	POST .../wrtdeck-api/api/v1/session/redeem 200 <- 前端改走一次性登录码
//	GET  .../wrtdeck-api/api/v1/dashboard  (Bearer) 200
//
// 也就是说，真正让用户免登录的是薄壳经 rpcd 换来的那张一次性登录码，
// 不是这里。之所以保留这条分支：它 fail-closed（拿不到 cookie 就什么都不注入），
// 且一旦 LuCI 把 cookie 放到 / 上（或前面挂了一层自己签发 cookie 的反向代理），
// 它就会自动生效。改动这里前请先确认那个前提是否已经改变。
func authorize(req *http.Request, cookie string, opts Options) error {
	if req.Header.Get("Authorization") != "" {
		// 浏览器自己带着凭据（一次性登录码换来的会话），原样转发即可
		return nil
	}
	if !opts.Inject || opts.Token == "" {
		return nil
	}
	if !opts.VerifyLuci {
		// 不校验却要注入，等于把面板凭据送给所有能访问设备 Web 端口的人
		opts.log("已跳过凭据注入：verify_luci_session 关闭时不允许注入 Token")
		return nil
	}
	sid := session_id(cookie)
	if sid == "" {
		return nil
	}
	if !probe_session(opts, sid) {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+opts.Token)
	return nil
}

// session_id 从 Cookie 里取出 LuCI 的会话标识
func session_id(cookie string) string {
	if cookie == "" {
		return ""
	}
	jars := map[string]string{}
	for _, item := range strings.Split(cookie, ";") {
		name, value, found := strings.Cut(strings.TrimSpace(item), "=")
		if !found {
			continue
		}
		jars[strings.TrimSpace(name)] = strings.TrimSpace(value)
	}
	for _, name := range luci_cookie_names {
		if value := jars[name]; value != "" {
			return value
		}
	}
	return ""
}

// probe_session 询问 rpcd 这个会话是否已经登录。
// rpcd 不可用或 ubus 缺失时一律按「未登录」处理，宁可不注入。
func probe_session(opts Options, sid string) bool {
	payload, err := json.Marshal(map[string]string{"ubus_rpc_session": sid})
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), ubus_timeout)
	defer cancel()

	command := exec.CommandContext(ctx, opts.UbusBin, "call", "session", "get", string(payload))
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err = command.Run(); err != nil {
		opts.log("校验 LuCI 会话失败（%v）：%s", err, strings.TrimSpace(stderr.String()))
		return false
	}
	return session_is_logged_in(stdout.Bytes())
}

// session_is_logged_in 判断 ubus 返回的会话数据里是否挂着用户名。
// 只有真正登录过的会话才有 username，匿名会话拿不到，
// 因此它比「ACL 非空」之类的信号更不容易误判。
func session_is_logged_in(raw []byte) bool {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false
	}
	if has_username(payload) {
		return true
	}
	values, ok := payload["values"].(map[string]any)
	return ok && has_username(values)
}

// has_username 检查一层对象里是否带非空用户名字段
func has_username(fields map[string]any) bool {
	for _, key := range []string{"username", "user"} {
		if name, ok := fields[key].(string); ok && strings.TrimSpace(name) != "" {
			return true
		}
	}
	return false
}
