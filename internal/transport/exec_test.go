package transport

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestExecAllowed 测试 Exec 放宽为黑名单机制与临时目录拦截
func TestExecAllowed(t *testing.T) {
	blocklist := []string{"rm", "/usr/bin/reboot"}

	// 常规系统命令在黑名单为空或未命中时应当允许执行
	if !exec_allowed("/bin/cat", blocklist) {
		t.Fatalf("未在黑名单的系统命令 /bin/cat 应当允许执行")
	}
	if !exec_allowed("/bin/echo", blocklist) {
		t.Fatalf("未在黑名单的系统命令 /bin/echo 应当允许执行")
	}

	// 临时目录下的文件一律拒绝
	if exec_allowed("/tmp/cat", blocklist) {
		t.Fatalf("/tmp 下的文件必须被严格拒绝")
	}
	if exec_allowed("/dev/shm/uptime", blocklist) {
		t.Fatalf("/dev/shm 下的文件必须被严格拒绝")
	}
	if exec_allowed("/var/run/echo", blocklist) {
		t.Fatalf("/var/run 下的文件必须被严格拒绝")
	}

	// 命中黑名单命令名被拒绝
	if exec_allowed("/bin/rm", blocklist) {
		t.Fatalf("命中黑名单命令名的程序必须被拒绝")
	}
	if exec_allowed("/usr/bin/reboot", blocklist) {
		t.Fatalf("命中黑名单路径的程序必须被拒绝")
	}
}

// TestResolveExecutable 测试命令名解析为系统实际路径
func TestResolveExecutable(t *testing.T) {
	path, err := resolve_executable("cat")
	if err != nil {
		t.Fatalf("解析常用命令 cat 失败: %v", err)
	}
	if !filepath.IsAbs(path) || !strings.HasSuffix(path, "cat") {
		t.Fatalf("解析 cat 应返回绝对路径，得到 %s", path)
	}

	abs_path, err := resolve_executable("/bin/sh")
	if err != nil {
		t.Fatalf("解析绝对路径失败: %v", err)
	}
	if abs_path != "/bin/sh" {
		t.Fatalf("绝对路径应保持原样，得到 %s", abs_path)
	}
}
