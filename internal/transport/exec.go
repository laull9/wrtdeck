package transport

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"wrtdeck/internal/registry"
)

// exec_transport 执行本地程序，只接受 executable + argv，绝不经过 shell
type exec_transport struct{}

// init 注册 Exec 传输实现
func init() {
	Register(&exec_transport{})
}

// Type 返回传输名称
func (e *exec_transport) Type() string { return registry.TransportExec }

// Available 表示 Exec 传输已编译，但能否执行还要看配置是否放开
func (e *exec_transport) Available() bool { return true }

// Do 经过安全校验与黑名单判定后执行外部程序
func (e *exec_transport) Do(ctx context.Context, spec registry.TransportSpec, opts Options) (*Result, error) {
	if spec.Exec == nil {
		return nil, fmt.Errorf("缺少 transport.exec 配置")
	}
	if !opts.ExecEnabled {
		return nil, ErrExecDisabled
	}
	cfg := spec.Exec
	target_path, err := resolve_executable(cfg.Executable)
	if err != nil {
		return nil, fmt.Errorf("解析可执行文件失败: %w", err)
	}
	if !exec_allowed(target_path, opts.ExecBlocklist) {
		return nil, fmt.Errorf("可执行文件 %s 被黑名单或安全策略拦截", target_path)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout_of(cfg.TimeoutMS, opts))
	defer cancel()

	cmd := exec.CommandContext(ctx, target_path, cfg.Args...)
	output, err := cmd.CombinedOutput()
	res := &Result{
		StatusCode: 200,
		Body:       truncate(output, opts.MaxBodyBytes),
		BodyText:   preview(output, 200),
		Detail:     fmt.Sprintf("%s 退出", filepath.Base(target_path)),
	}
	if err != nil {
		return res, fmt.Errorf("执行失败: %w", err)
	}
	return res, nil
}

// resolve_executable 解析命令名为系统中的绝对路径
func resolve_executable(name string) (string, error) {
	target := strings.TrimSpace(name)
	if target == "" {
		return "", fmt.Errorf("可执行文件不能为空")
	}
	if !filepath.IsAbs(target) {
		if resolved, err := exec.LookPath(target); err == nil {
			target = resolved
		}
	}
	absolute, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}

// insecure_dirs 定义禁止执行程序的临时与易写目录
var insecure_dirs = []string{"/tmp", "/var/tmp", "/dev/shm", "/run", "/var/run"}

// is_insecure_path 检查路径是否位于不安全的临时目录中
func is_insecure_path(path string) bool {
	clean := filepath.Clean(path)
	for _, dir := range insecure_dirs {
		if clean == dir || strings.HasPrefix(clean, dir+"/") {
			return true
		}
	}
	return false
}

// exec_allowed 判断目标程序是否放行：放宽常规系统命令，仅拦截黑名单与临时目录
func exec_allowed(path string, blocklist []string) bool {
	clean := filepath.Clean(path)
	if is_insecure_path(clean) {
		return false
	}
	base := strings.ToLower(filepath.Base(clean))
	for _, item := range blocklist {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if strings.EqualFold(item, base) {
			return false
		}
		if filepath.IsAbs(item) {
			if filepath.Clean(item) == clean {
				return false
			}
		} else {
			if lp, err := exec.LookPath(item); err == nil {
				if abs_lp, err := filepath.Abs(lp); err == nil && filepath.Clean(abs_lp) == clean {
					return false
				}
			}
		}
	}
	return true
}
