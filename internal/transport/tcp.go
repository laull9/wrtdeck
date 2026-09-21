package transport

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"

	"owdash/internal/registry"
)

// tcp_transport 负责执行一次 TCP 交互，可选读取一次响应
type tcp_transport struct{}

// init 注册 TCP 传输实现
func init() {
	Register(&tcp_transport{})
}

// Type 返回传输名称
func (t *tcp_transport) Type() string { return registry.TransportTCP }

// Available 表示 TCP 传输始终可用
func (t *tcp_transport) Available() bool { return true }

// Do 建立 TCP 连接并发送载荷
func (t *tcp_transport) Do(ctx context.Context, spec registry.TransportSpec, opts Options) (*Result, error) {
	if spec.TCP == nil {
		return nil, fmt.Errorf("缺少 transport.tcp 配置")
	}
	cfg := spec.TCP
	payload, err := encode_payload(cfg.Payload, cfg.Encoding)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout_of(cfg.TimeoutMS, opts))
	defer cancel()

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("连接 %s 失败: %w", cfg.Address, err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}

	if len(payload) > 0 {
		if _, err = conn.Write(payload); err != nil {
			return nil, fmt.Errorf("发送失败: %w", err)
		}
	}

	res := &Result{StatusCode: 200, Detail: fmt.Sprintf("已向 %s 发送 %d 字节", cfg.Address, len(payload))}
	if !cfg.ExpectReply {
		return res, nil
	}

	limit := opts.MaxBodyBytes
	if limit <= 0 {
		limit = 256 * 1024
	}
	data, err := io.ReadAll(io.LimitReader(conn, int64(limit)))
	if err != nil && !strings.Contains(err.Error(), "deadline") {
		return res, fmt.Errorf("读取响应失败: %w", err)
	}
	res.Body = truncate(data, opts.MaxBodyBytes)
	res.BodyText = preview(data, 200)
	return res, nil
}
