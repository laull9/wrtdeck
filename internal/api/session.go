package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"wrtdeck/internal/config"
)

// 一次性登录码的参数：有效期只够浏览器完成一次跳转，容量上限避免被本地进程刷爆内存
const (
	handoff_ttl     = 60 * time.Second
	handoff_max     = 32
	handoff_code_by = 24
)

// 登录与改口令的请求体上限，口令再长也用不到 4KB
const credential_body_max = 4096

// handoff_store 保存尚未兑换的一次性登录码，只驻内存，进程重启即全部失效
type handoff_store struct {
	mu    sync.Mutex
	items map[string]time.Time
}

// new_handoff_store 创建登录码表
func new_handoff_store() *handoff_store {
	return &handoff_store{items: map[string]time.Time{}}
}

// issue 签发一个登录码并登记有效期，返回码与有效秒数
func (s *handoff_store) issue() (string, int, error) {
	code, err := random_code()
	if err != nil {
		return "", 0, err
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	// 顺手清理过期项，容量上限是最后一道保险
	for key, expire_at := range s.items {
		if now.After(expire_at) {
			delete(s.items, key)
		}
	}
	if len(s.items) >= handoff_max {
		s.drop_oldest()
	}
	s.items[code] = now.Add(handoff_ttl)
	return code, int(handoff_ttl.Seconds()), nil
}

// drop_oldest 丢弃最早失效的一项，调用方必须已持锁
func (s *handoff_store) drop_oldest() {
	oldest_key := ""
	var oldest time.Time
	for key, expire_at := range s.items {
		if oldest_key == "" || expire_at.Before(oldest) {
			oldest_key, oldest = key, expire_at
		}
	}
	if oldest_key != "" {
		delete(s.items, oldest_key)
	}
}

// redeem 校验并立即作废一个登录码，成功返回 true；逐个定长比较避免时间侧信道
func (s *handoff_store) redeem(code string) bool {
	if code == "" {
		return false
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	found := ""
	for key, expire_at := range s.items {
		if now.After(expire_at) {
			delete(s.items, key)
			continue
		}
		if subtle.ConstantTimeCompare([]byte(key), []byte(code)) == 1 {
			found = key
		}
	}
	if found == "" {
		return false
	}
	delete(s.items, found)
	return true
}

// random_code 生成 URL 安全的一次性登录码
func random_code() (string, error) {
	buf := make([]byte, handoff_code_by)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成登录码失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// login_request 是口令登录的请求体；remember 只影响前端把凭据放在哪个存储里
type login_request struct {
	Password string `json:"password"`
	Remember bool   `json:"remember"`
}

// password_request 是修改口令的请求体
type password_request struct {
	Current string `json:"current_password"`
	New     string `json:"new_password"`
}

// session_response 是所有登录相关接口共用的返回体。
// Token 是会话凭据，与 API Token 用法相同（Authorization: Bearer），但会过期。
type session_response struct {
	Token              string `json:"token"`
	ExpiresInS         int    `json:"expires_in_s"`
	MustChangePassword bool   `json:"must_change_password"`
	Mode               string `json:"mode"`
}

// handoff_response 是登录码签发接口的返回体
type handoff_response struct {
	Code      string `json:"code"`
	ExpiresIn int    `json:"expires_in_s"`
}

// audit 记录鉴权事件。只写来源与结果，口令、Token、登录码一律不进日志。
func audit(event, remote, detail string) {
	if detail == "" {
		log.Printf("[auth] %s remote=%s", event, remote)
		return
	}
	log.Printf("[auth] %s remote=%s %s", event, remote, detail)
}

// handle_session_login 用口令换取会话凭据。
//
// 这是面板的主要入口。防御按代价从低到高叠了三层：
//  1. 仍是初始口令时只允许内网来源登录，避免默认口令被公网扫描直接命中；
//  2. 单来源连续失败即锁定并成倍退避，另有一条全局闸门挡分布式尝试；
//  3. 口令用 PBKDF2-HMAC-SHA256 校验，命中失败也不透露是口令错还是账号不存在。
func (s *Server) handle_session_login(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Auth.Disabled {
		write_error(w, http.StatusBadRequest, "auth_disabled", "服务未启用鉴权，无需登录")
		return
	}
	var body login_request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, credential_body_max)).Decode(&body); err != nil {
		write_error(w, http.StatusBadRequest, "bad_request", "请求体不是合法的 JSON")
		return
	}

	remote := s.client_ip(r)
	now := time.Now()

	if s.secrets.MustChange() && !s.is_private_request(r) {
		audit("login.blocked", remote, "初始口令期间拒绝外网登录")
		write_error(w, http.StatusForbidden, "setup_required",
			"面板仍在使用初始口令，请先在同一局域网内登录并修改口令")
		return
	}

	if ok, wait := s.logins.Allow(remote, now); !ok {
		audit("login.throttled", remote, "等待 "+wait.Round(time.Second).String())
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		write_error(w, http.StatusTooManyRequests, "locked_out",
			"登录尝试过于频繁，请 "+human_wait(wait)+"后再试")
		return
	}

	if !s.secrets.VerifyPassword(body.Password) {
		lock := s.logins.Fail(remote, now)
		audit("login.failed", remote, "")
		if lock > 0 {
			audit("login.locked", remote, "锁定时长 "+lock.String())
			w.Header().Set("Retry-After", strconv.Itoa(int(lock.Seconds())+1))
			write_error(w, http.StatusTooManyRequests, "locked_out",
				"口令连续错误，已暂时拒绝该来源登录，请 "+human_wait(lock)+"后再试")
			return
		}
		write_error(w, http.StatusUnauthorized, "bad_credentials", "口令不正确")
		return
	}

	s.logins.Succeed(remote)
	token, ttl, err := s.sessions.issue(remote)
	if err != nil {
		write_error(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	audit("login.ok", remote, "")
	write_json(w, http.StatusOK, session_response{
		Token:              token,
		ExpiresInS:         ttl,
		MustChangePassword: s.secrets.MustChange(),
		Mode:               "password",
	})
}

// handle_session_logout 作废当前会话
func (s *Server) handle_session_logout(w http.ResponseWriter, r *http.Request) {
	ctx := auth_from(r)
	if ctx.role == role_session {
		s.sessions.revoke(ctx.token)
		audit("logout", s.client_ip(r), "")
	}
	write_json(w, http.StatusOK, map[string]bool{"ok": true})
}

// handle_session_me 回报当前凭据的身份与状态，前端用它判断凭据是否还有效。
//
// 它在「必须改口令」期间也放行，否则前端连自己该不该弹改密框都问不出来。
func (s *Server) handle_session_me(w http.ResponseWriter, r *http.Request) {
	ctx := auth_from(r)
	resp := session_response{
		MustChangePassword: s.secrets.MustChange(),
		Mode:               ctx.role,
	}
	if ctx.role == role_session {
		if item, ok := s.sessions.lookup(ctx.token); ok {
			resp.ExpiresInS = int(time.Until(item.expires).Seconds())
		}
	}
	write_json(w, http.StatusOK, resp)
}

// handle_session_password 修改口令。
//
// 口令变更后所有既有会话立刻失效：这是「发现口令泄漏后改一下就好」的前提。
// 改用会话登录的调用方会拿到一张新凭据，不需要重新输一次口令。
func (s *Server) handle_session_password(w http.ResponseWriter, r *http.Request) {
	ctx := auth_from(r)
	var body password_request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, credential_body_max)).Decode(&body); err != nil {
		write_error(w, http.StatusBadRequest, "bad_request", "请求体不是合法的 JSON")
		return
	}

	remote := s.client_ip(r)
	now := time.Now()
	if ok, wait := s.logins.Allow(remote, now); !ok {
		audit("password.throttled", remote, "")
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		write_error(w, http.StatusTooManyRequests, "locked_out",
			"尝试过于频繁，请 "+human_wait(wait)+"后再试")
		return
	}
	if !s.secrets.VerifyPassword(body.Current) {
		lock := s.logins.Fail(remote, now)
		audit("password.rejected", remote, "当前口令不符")
		if lock > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(lock.Seconds())+1))
			write_error(w, http.StatusTooManyRequests, "locked_out",
				"当前口令连续错误，已暂时拒绝该来源的请求，请 "+human_wait(lock)+"后再试")
			return
		}
		write_error(w, http.StatusUnauthorized, "bad_credentials", "当前口令不正确")
		return
	}
	if err := config.CheckPasswordStrength(body.New); err != nil {
		write_error(w, http.StatusBadRequest, "weak_password", err.Error())
		return
	}
	if body.New == body.Current {
		write_error(w, http.StatusBadRequest, "same_password", "新口令与当前口令相同")
		return
	}
	if err := s.secrets.SetPassword(body.New, false); err != nil {
		write_error(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	s.logins.Succeed(remote)
	// 换了口令，所有旧会话一律作废；用会话登录的调用方换一张新的
	s.sessions.clear()
	audit("password.changed", remote, "")
	resp := session_response{MustChangePassword: false, Mode: ctx.role}
	if ctx.role == role_session {
		token, ttl, err := s.sessions.issue(remote)
		if err != nil {
			write_error(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		resp.Token, resp.ExpiresInS = token, ttl
	}
	write_json(w, http.StatusOK, resp)
}

// handle_session_handoff 为设备本机的调用方签发一次性登录码。
//
// LuCI 薄壳以 root 身份调这个接口，把短码交给浏览器，于是长期凭据不必出现在
// 地址栏、浏览器历史或 iframe 的 src 里。它要求来源是回环地址且持有有效凭据，
// 公网上的任何来源都够不着。
func (s *Server) handle_session_handoff(w http.ResponseWriter, r *http.Request) {
	// 「经过代理」这一条不能省：转发之后的 RemoteAddr 是代理自己的回环地址，
	// 只看它的话，任何能碰到设备 Web 端口的人都能顺手换一张登录码。
	if arrived_via_proxy(r) || !is_loopback_addr(r.RemoteAddr) {
		audit("handoff.blocked", s.client_ip(r), "非本机直连来源")
		write_error(w, http.StatusForbidden, "remote_not_allowed", "登录码只能由设备本机申请")
		return
	}
	code, ttl, err := s.handoffs.issue()
	if err != nil {
		write_error(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	audit("handoff.issued", s.client_ip(r), "")
	write_json(w, http.StatusOK, handoff_response{Code: code, ExpiresIn: ttl})
}

// handle_session_redeem 用一次性登录码换取会话凭据，登录码用后即废。
// 与其它写接口一样要求同源：跨站页面既拿不到码，也不该能在这里发起请求。
func (s *Server) handle_session_redeem(w http.ResponseWriter, r *http.Request) {
	if ok, present := same_origin(r); !present || !ok {
		audit("redeem.blocked", s.client_ip(r), same_origin_reason(present, ok))
		write_error(w, http.StatusForbidden, "cross_origin", "请求不是来自面板自身页面，拒绝兑换登录码")
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, credential_body_max)).Decode(&body); err != nil {
		write_error(w, http.StatusBadRequest, "bad_request", "请求体不是合法的 JSON")
		return
	}
	if !s.handoffs.redeem(strings.TrimSpace(body.Code)) {
		audit("redeem.rejected", s.client_ip(r), "登录码无效或已过期")
		write_error(w, http.StatusUnauthorized, "bad_code", "登录码无效或已过期，请重新进入面板")
		return
	}
	token, ttl, err := s.sessions.issue(s.client_ip(r))
	if err != nil {
		write_error(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	audit("redeem.ok", s.client_ip(r), "")
	write_json(w, http.StatusOK, session_response{
		Token:              token,
		ExpiresInS:         ttl,
		MustChangePassword: s.secrets.MustChange(),
		Mode:               "handoff",
	})
}

// client_ip 取得当前请求的来源客户端 IP
func (s *Server) client_ip(r *http.Request) string {
	return client_ip(r, s.cfg.Auth.TrustedProxies)
}

// is_private_request 判断请求来源是否属于局域网或本机回环
func (s *Server) is_private_request(r *http.Request) bool {
	raw := s.client_ip(r)
	ip := net.ParseIP(raw)
	return is_private_ip(ip)
}
