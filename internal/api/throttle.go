package api

import (
	"sync"
	"time"
)

// 登录限速的参数。
//
// 设备面板的口令空间远小于服务端应用（用户往往图省事），因此限速是主要防线：
// 单来源连续失败若干次即锁定一段时间，连续被锁则锁定时长翻倍；
// 另有一条全局闸门，用于挡住换着来源地址打过来的分布式尝试。
const (
	// 失败计数的统计窗口，窗口内累计到位就触发锁定
	throttle_window = 15 * time.Minute
	// 单次锁定的上限，避免退避到把设备主人自己也永久挡在门外
	throttle_max_lock = time.Hour
	// 全局闸门：统计窗口内所有来源的失败总数
	throttle_global_window = time.Minute
	throttle_global_fails  = 50
	throttle_global_lock   = time.Minute
	// 表的大小上限，超过就整表重置，避免大量伪造来源把内存撑爆
	throttle_max_sources = 1024
)

// attempt_record 是单个来源的失败记录
type attempt_record struct {
	fails         int
	first_fail    time.Time
	streak        int
	blocked_until time.Time
}

// global_record 是全体来源共享的失败记录
type global_record struct {
	fails      int
	first_fail time.Time
	until      time.Time
}

// login_throttle 限制登录尝试频率，抵挡口令暴力破解
type login_throttle struct {
	mu       sync.Mutex
	by_ip    map[string]*attempt_record
	global   global_record
	max      int
	lock_for time.Duration
}

// new_login_throttle 创建限速器，max 为窗口内允许的失败次数
func new_login_throttle(max int, lock_for time.Duration) *login_throttle {
	if max <= 0 {
		max = 5
	}
	if lock_for <= 0 {
		lock_for = 15 * time.Minute
	}
	return &login_throttle{by_ip: map[string]*attempt_record{}, max: max, lock_for: lock_for}
}

// Allow 判断该来源现在是否可以尝试登录，被拒时返回剩余等待时长
func (t *login_throttle) Allow(remote string, now time.Time) (bool, time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if now.Before(t.global.until) {
		return false, t.global.until.Sub(now)
	}
	record, ok := t.by_ip[remote]
	if !ok {
		return true, 0
	}
	if now.Before(record.blocked_until) {
		return false, record.blocked_until.Sub(now)
	}
	// 统计窗口过了就重新开始计数，否则偶尔输错几次会累积成永久锁定
	if now.Sub(record.first_fail) > throttle_window {
		delete(t.by_ip, remote)
	}
	return true, 0
}

// Fail 记录一次失败；达到阈值即锁定该来源，并返回锁定时长（未锁定为 0）
func (t *login_throttle) Fail(remote string, now time.Time) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.count_global(now)

	if len(t.by_ip) >= throttle_max_sources {
		t.by_ip = map[string]*attempt_record{}
	}
	record, ok := t.by_ip[remote]
	if !ok {
		record = &attempt_record{first_fail: now}
		t.by_ip[remote] = record
	}
	if now.Sub(record.first_fail) > throttle_window {
		record.fails = 0
		record.first_fail = now
	}
	record.fails++
	if record.fails < t.max {
		return 0
	}
	// 连续被锁则翻倍退避：第一次锁 15 分钟，之后 30、60 分钟封顶
	record.streak++
	lock := t.lock_for * time.Duration(1<<uint(min_int(record.streak-1, 6)))
	if lock > throttle_max_lock {
		lock = throttle_max_lock
	}
	record.blocked_until = now.Add(lock)
	record.fails = 0
	record.first_fail = now
	return lock
}

// Succeed 在登录成功后清空该来源的失败记录
func (t *login_throttle) Succeed(remote string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.by_ip, remote)
}

// count_global 累计全局失败数，窗口内超限就短暂关上所有来源的门，调用方必须已持锁
func (t *login_throttle) count_global(now time.Time) {
	if now.Sub(t.global.first_fail) > throttle_global_window {
		t.global.fails = 0
		t.global.first_fail = now
	}
	t.global.fails++
	if t.global.fails >= throttle_global_fails {
		t.global.until = now.Add(throttle_global_lock)
		t.global.fails = 0
		t.global.first_fail = now
	}
}

// min_int 返回较小值
func min_int(a, b int) int {
	if a < b {
		return a
	}
	return b
}
