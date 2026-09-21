package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
)

// 会话表的上限。设备上同时在线的面板寥寥无几，给足冗余即可，
// 上限的意义是避免异常情况下无限增长把内存吃光。
const session_max = 64

// session 是一次登录产生的工作凭据
type session struct {
	token   string
	created time.Time
	expires time.Time
	remote  string
}

// session_store 保存已登录的会话，只驻内存。
//
// 不落盘是刻意的：设备闪存写放大很敏感，而且进程重启后重新登录一次
// 恰恰是「口令改过、旧会话必须失效」最简单可靠的实现。
type session_store struct {
	mu    sync.Mutex
	items map[string]*session
	ttl   time.Duration
}

// new_session_store 创建会话表
func new_session_store(ttl time.Duration) *session_store {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return &session_store{items: map[string]*session{}, ttl: ttl}
}

// issue 签发一个新会话，返回凭据与有效秒数
func (s *session_store) issue(remote string) (string, int, error) {
	token, err := random_session_token()
	if err != nil {
		return "", 0, err
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune(now)
	if len(s.items) >= session_max {
		s.drop_oldest()
	}
	s.items[token] = &session{token: token, created: now, expires: now.Add(s.ttl), remote: remote}
	return token, int(s.ttl.Seconds()), nil
}

// lookup 校验凭据，命中且未过期时返回会话副本
func (s *session_store) lookup(token string) (session, bool) {
	if token == "" {
		return session{}, false
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	found := ""
	for key, item := range s.items {
		if now.After(item.expires) {
			delete(s.items, key)
			continue
		}
		// 逐个定长比较，避免用返回时间差把凭据一个字节一个字节试出来
		if subtle.ConstantTimeCompare([]byte(key), []byte(token)) == 1 {
			found = key
		}
	}
	if found == "" {
		return session{}, false
	}
	return *s.items[found], true
}

// revoke 作废一个会话（登出）
func (s *session_store) revoke(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, token)
}

// clear 作废全部会话，口令变更后调用，让所有旧登录立刻失效
func (s *session_store) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = map[string]*session{}
}

// count 返回当前有效会话数
func (s *session_store) count() int {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune(now)
	return len(s.items)
}

// prune 清掉已过期的会话，调用方必须已持锁
func (s *session_store) prune(now time.Time) {
	for key, item := range s.items {
		if now.After(item.expires) {
			delete(s.items, key)
		}
	}
}

// drop_oldest 丢弃最早创建的会话，调用方必须已持锁
func (s *session_store) drop_oldest() {
	oldest_key := ""
	var oldest time.Time
	for key, item := range s.items {
		if oldest_key == "" || item.created.Before(oldest) {
			oldest_key, oldest = key, item.created
		}
	}
	if oldest_key != "" {
		delete(s.items, oldest_key)
	}
}

// random_session_token 生成 URL 安全的会话凭据
func random_session_token() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成会话凭据失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
