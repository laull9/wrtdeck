package api

import (
	"fmt"
	"testing"
	"time"
)

// TestIPv6Normalization 测试 IPv6 聚合至 /64 网段
func TestIPv6Normalization(t *testing.T) {
	ip1 := "2001:db8:abcd:0012:0000:0000:0000:0001"
	ip2 := "2001:db8:abcd:0012:ffff:ffff:ffff:ffff"

	norm1 := normalize_source(ip1)
	norm2 := normalize_source(ip2)

	if norm1 != norm2 {
		t.Fatalf("同一 /64 网段下的 IPv6 应归一为相同标识，得到 %s 与 %s", norm1, norm2)
	}

	ipv4 := "192.168.1.50"
	if norm_v4 := normalize_source(ipv4); norm_v4 != ipv4 {
		t.Fatalf("IPv4 应当保持原样，得到 %s", norm_v4)
	}
}

// TestEvictOldestNotWipingTable 测试达到容量上限时只逐出最早记录而不重置整表
func TestEvictOldestNotWipingTable(t *testing.T) {
	throttle := new_login_throttle(3, 15*time.Minute)
	now := time.Now()

	target_ip := "198.51.100.99"
	// 让 target_ip 失败 3 次进入锁定
	for i := 0; i < 3; i++ {
		throttle.Fail(target_ip, now)
	}
	if allowed, _ := throttle.Allow(target_ip, now); allowed {
		t.Fatalf("target_ip 应当处于锁定状态")
	}

	// 模拟涌入 1024 个其它来源的错误
	for i := 0; i < throttle_max_sources; i++ {
		source := fmt.Sprintf("10.0.%d.%d", (i>>8)&0xff, i&0xff)
		throttle.Fail(source, now.Add(time.Duration(i)*time.Millisecond))
	}

	// 锁定中的 target_ip 不能因为表满而被意外释放
	if allowed, _ := throttle.Allow(target_ip, now); allowed {
		t.Fatalf("表满逐出不应导致正在锁定的 target_ip 被误清空解封")
	}
}
