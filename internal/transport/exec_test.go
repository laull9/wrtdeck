package transport

import (
	"testing"
)

// TestExecAllowed 测试 Exec 白名单绝对路径与拒绝临时目录
func TestExecAllowed(t *testing.T) {
	allowlist := []string{"/bin/echo", "/usr/bin/uptime"}

	if !exec_allowed("/bin/echo", allowlist) {
		t.Fatalf("白名单内的绝对路径应当允许执行")
	}

	// 临时目录下的同名文件必须被拒绝
	if exec_allowed("/tmp/echo", allowlist) {
		t.Fatalf("/tmp 下的文件必须被严格拒绝")
	}
	if exec_allowed("/dev/shm/uptime", allowlist) {
		t.Fatalf("/dev/shm 下的文件必须被严格拒绝")
	}
	if exec_allowed("/var/run/echo", allowlist) {
		t.Fatalf("/var/run 下的文件必须被严格拒绝")
	}

	// 未在白名单的路径必须被拒绝
	if exec_allowed("/bin/ls", allowlist) {
		t.Fatalf("不在白名单的程序应当被拒绝")
	}
}
