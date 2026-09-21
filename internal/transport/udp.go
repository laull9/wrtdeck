package transport

import (
	"context"
	"fmt"
	"net"

	"owdash/internal/registry"
)

// udp_transport 负责发送 UDP 报文，可选等待一次单包回应
type udp_transport struct{}

// init 注册 UDP 传输实现
func init() {
	Register(&udp_transport{})
}

// Type 返回传输名称
func (u *udp_transport) Type() string { return registry.TransportUDP }

// Available 表示 UDP 传输始终可用
func (u *udp_transport) Available() bool { return true }

// Do 发送一个 UDP 数据报
func (u *udp_transport) Do(ctx context.Context, spec registry.TransportSpec, opts Options) (*Result, error) {
	if spec.UDP == nil {
		return nil, fmt.Errorf("缺少 transport.udp 配置")
	}
	cfg := spec.UDP
	payload, err := encode_payload(cfg.Payload, cfg.Encoding)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout_of(cfg.TimeoutMS, opts))
	defer cancel()

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "udp", cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("解析目标 %s 失败: %w", cfg.Address, err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}

	if _, err = conn.Write(payload); err != nil {
		return nil, fmt.Errorf("发送失败: %w", err)
	}

	res := &Result{StatusCode: 200, Detail: fmt.Sprintf("已向 %s 发送 %d 字节", cfg.Address, len(payload))}
	if !cfg.ExpectReply {
		return res, nil
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return res, fmt.Errorf("等待回应失败: %w", err)
	}
	res.Body = buf[:n]
	res.BodyText = preview(res.Body, 200)
	return res, nil
}
