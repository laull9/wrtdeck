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

// Do 按 allowlist 校验后执行外部程序
func (e *exec_transport) Do(ctx context.Context, spec registry.TransportSpec, opts Options) (*Result, error) {
	if spec.Exec == nil {
		return nil, fmt.Errorf("缺少 transport.exec 配置")
	}
	if !opts.ExecEnabled {
		return nil, ErrExecDisabled
	}
	cfg := spec.Exec
	absolute, err := filepath.Abs(cfg.Executable)
	if err != nil {
		return nil, fmt.Errorf("解析可执行文件路径失败: %w", err)
	}
	if !exec_allowed(absolute, opts.ExecAllowlist) {
		return nil, fmt.Errorf("可执行文件 %s 不在 allowlist 中", absolute)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout_of(cfg.TimeoutMS, opts))
	defer cancel()

	cmd := exec.CommandContext(ctx, absolute, cfg.Args...)
	output, err := cmd.CombinedOutput()
	res := &Result{
		StatusCode: 200,
		Body:       truncate(output, opts.MaxBodyBytes),
		BodyText:   preview(output, 200),
		Detail:     fmt.Sprintf("%s 退出", filepath.Base(absolute)),
	}
	if err != nil {
		return res, fmt.Errorf("执行失败: %w", err)
	}
	return res, nil
}

// exec_allowed 判断目标程序是否命中 allowlist，空列表表示全部拒绝
func exec_allowed(path string, allowlist []string) bool {
	for _, item := range allowlist {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if item == path || item == filepath.Base(path) {
			return true
		}
	}
	return false
}
