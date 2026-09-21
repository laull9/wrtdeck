package transport

import (
	"testing"
)

// TestBlockedTarget 测试拦截云元数据端点
func TestBlockedTarget(t *testing.T) {
	blocked := []string{
		"http://169.254.169.254/latest/meta-data/",
		"https://169.254.169.254/secret",
		"http://instance-data/latest/meta-data/",
	}
	for _, target := range blocked {
		if !is_blocked_target(target) {
			t.Fatalf("应当拦截云元数据目标：%s", target)
		}
	}

	normal := []string{
		"http://192.168.1.1/api/temp",
		"https://api.weather.com/v1",
		"http://127.0.0.1:8080/health",
	}
	for _, target := range normal {
		if is_blocked_target(target) {
			t.Fatalf("正常目标不应被拦截：%s", target)
		}
	}
}
