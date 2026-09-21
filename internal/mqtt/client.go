// Package mqtt 是一个只依赖标准库的 MQTT 3.1.1 客户端。
//
// 刻意选择 3.1.1 而不是 5.0：3.1.1 是物联网设备事实上的通用版本，
// ESP32、Tasmota、Zigbee2MQTT 以及绝大多数局域网 broker 都支持它，
// 而 5.0 会把这些设备全部排除在外。协议实现只覆盖本项目真正用到的部分：
// CONNECT / PUBLISH(0,1,2) / SUBSCRIBE / UNSUBSCRIBE / PINGREQ / DISCONNECT。
package mqtt

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ack_timeout 是等待 PUBACK / SUBACK 这类应答的上限
const ack_timeout = 10 * time.Second

// Options 描述一条 MQTT 连接的参数
type Options struct {
	BrokerURL         string
	ClientID          string
	Username          string
	Password          string
	Keepalive         time.Duration
	ConnectTimeout    time.Duration
	MaxReconnectDelay time.Duration
	// CleanSession 为 true 时不在 broker 上保留会话，断线期间的消息会被丢弃
	CleanSession bool

	// OnMessage 在收到订阅命中的消息时调用，实现方不得阻塞
	OnMessage func(topic string, payload []byte)
	// OnConnectionChange 在连接建立或断开时调用，实现方不得阻塞
	OnConnectionChange func(up bool)
	Logger             *log.Logger
}

// Client 是一条 MQTT 3.1.1 长连接，自带保活、重连与订阅恢复
type Client struct {
	opts Options

	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	started bool
	closed  bool
	conn    net.Conn
	writer  *bufio.Writer
	reader  *bufio.Reader
	ready   chan struct{}
	// link 在连接建立时创建、断开时关闭，用于唤醒所有等待应答的调用方
	link    chan struct{}
	next_id uint16

	// pending 是等待应答的请求：报文标识符 → 结果通道
	pending map[uint16]chan byte
	// inbound_qos2 记录已收到 PUBLISH 但还没完成 PUBREL 的报文标识符
	inbound_qos2 map[uint16]bool
	// wanted 是期望的订阅集合，applied 是当前连接上已确认生效的订阅
	wanted  map[string]int
	applied map[string]int

	// reconcile_mu 串行化订阅收敛，避免并发下发重复的 SUBSCRIBE
	reconcile_mu sync.Mutex
}

// NewClient 创建客户端对象，真正的建连由 Start 启动的后台协程负责
func NewClient(opts Options) *Client {
	if opts.Keepalive <= 0 {
		opts.Keepalive = 30 * time.Second
	}
	if opts.ConnectTimeout <= 0 {
		opts.ConnectTimeout = 5 * time.Second
	}
	if opts.MaxReconnectDelay <= 0 {
		opts.MaxReconnectDelay = 30 * time.Second
	}
	if opts.Logger == nil {
		opts.Logger = log.Default()
	}
	return &Client{
		opts:         opts,
		pending:      make(map[uint16]chan byte),
		inbound_qos2: make(map[uint16]bool),
		wanted:       make(map[string]int),
		applied:      make(map[string]int),
		ready:        make(chan struct{}),
	}
}

// Start 启动连接与重连协程，ctx 结束后不再重连
func (c *Client) Start(ctx context.Context) {
	run_ctx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	if c.started || c.closed {
		c.mu.Unlock()
		cancel()
		return
	}
	c.started = true
	c.ctx = run_ctx
	c.cancel = cancel
	c.mu.Unlock()
	go c.run(run_ctx)
}

// base_ctx 返回内部运行上下文，未启动时退化为后台上下文
func (c *Client) base_ctx() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx != nil {
		return c.ctx
	}
	return context.Background()
}

// run 是连接主循环：建连 → 收敛订阅 → 收报文 → 断开 → 退避重试
func (c *Client) run(ctx context.Context) {
	attempt := 0
	for {
		if ctx.Err() != nil {
			return
		}
		conn, reader, err := c.dial(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			attempt++
			c.opts.Logger.Printf("MQTT %s 连接失败（第 %d 次）: %v", c.opts.BrokerURL, attempt, err)
			if !c.sleep(ctx, c.backoff(attempt)) {
				return
			}
			continue
		}

		attempt = 0
		c.attach(conn, reader)
		// 必须先启动读协程再收敛订阅，否则 SUBACK 无人接收会一路等到超时
		read_done := make(chan error, 1)
		go func() { read_done <- c.read_loop(ctx, reader) }()

		c.notify(true)
		if err = c.reconcile(); err != nil && ctx.Err() == nil {
			c.opts.Logger.Printf("MQTT %s 恢复订阅失败: %v", c.opts.BrokerURL, err)
		}

		read_err := <-read_done
		c.detach()

		if ctx.Err() != nil {
			return
		}
		c.notify(false)
		c.opts.Logger.Printf("MQTT %s 连接中断: %v", c.opts.BrokerURL, read_err)
		attempt++
		if !c.sleep(ctx, c.backoff(attempt)) {
			return
		}
	}
}

// dial 建立 TCP（或 TLS）连接并完成 MQTT 握手
func (c *Client) dial(ctx context.Context) (net.Conn, *bufio.Reader, error) {
	parsed, err := url.Parse(c.opts.BrokerURL)
	if err != nil {
		return nil, nil, fmt.Errorf("解析 broker 地址失败: %w", err)
	}
	// WebSocket 承载的 MQTT 需要额外的帧协议，本实现不支持，提前给出明确错误
	switch strings.ToLower(parsed.Scheme) {
	case "mqtt", "tcp", "mqtts", "ssl", "tls":
	default:
		return nil, nil, fmt.Errorf("broker scheme %q 不受支持，可用 mqtt、tcp、mqtts、ssl 或 tls", parsed.Scheme)
	}
	address := parsed.Host
	if parsed.Port() == "" {
		address = net.JoinHostPort(parsed.Hostname(), default_port(parsed.Scheme))
	}

	dialer := &net.Dialer{Timeout: c.opts.ConnectTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, nil, fmt.Errorf("连接 %s 失败: %w", address, err)
	}
	if is_tls_scheme(parsed.Scheme) {
		tls_conn := tls.Client(conn, &tls.Config{ServerName: parsed.Hostname(), MinVersion: tls.VersionTLS12})
		_ = tls_conn.SetDeadline(time.Now().Add(c.opts.ConnectTimeout))
		if err = tls_conn.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, nil, fmt.Errorf("TLS 握手失败: %w", err)
		}
		conn = tls_conn
	}

	reader := bufio.NewReaderSize(conn, 8192)
	if err = c.handshake(conn, reader); err != nil {
		conn.Close()
		return nil, nil, err
	}
	return conn, reader, nil
}

// handshake 发送 CONNECT 并校验 CONNACK
func (c *Client) handshake(conn net.Conn, reader *bufio.Reader) error {
	if err := conn.SetDeadline(time.Now().Add(c.opts.ConnectTimeout)); err != nil {
		return err
	}
	keepalive := uint16(c.opts.Keepalive / time.Second)
	request := build_connect(c.opts.ClientID, c.opts.Username, c.opts.Password, keepalive, c.opts.CleanSession)
	if _, err := conn.Write(request); err != nil {
		return fmt.Errorf("发送 CONNECT 失败: %w", err)
	}
	reply, err := read_packet(reader)
	if err != nil {
		return fmt.Errorf("等待 CONNACK 失败: %w", err)
	}
	if reply.ptype != packet_connack {
		return fmt.Errorf("期望 CONNACK，实际收到报文类型 %d", reply.ptype)
	}
	if len(reply.body) < 2 {
		return errors.New("CONNACK 报文长度非法")
	}
	if code := reply.body[1]; code != connack_accepted {
		return fmt.Errorf("broker 拒绝连接: %s", connack_text(code))
	}
	return conn.SetDeadline(time.Time{})
}

// attach 记录当前连接并放行等待建连的调用方
func (c *Client) attach(conn net.Conn, reader *bufio.Reader) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		conn.Close()
		return
	}
	c.conn = conn
	c.writer = bufio.NewWriter(conn)
	// 复用握手里的读缓冲，避免丢掉与 CONNACK 同段到达的后续报文
	c.reader = reader
	// 新连接上还没有任何订阅生效，交由 reconcile 重新下发
	c.applied = make(map[string]int)
	c.link = make(chan struct{})
	close(c.ready)
	c.ready = make(chan struct{})
	c.mu.Unlock()
	c.opts.Logger.Printf("MQTT %s 已连接（client_id=%s，协议 3.1.1）", c.opts.BrokerURL, c.opts.ClientID)
}

// detach 清理断开的连接并唤醒所有等待应答的调用方
func (c *Client) detach() {
	c.mu.Lock()
	conn := c.conn
	c.conn = nil
	c.writer = nil
	c.reader = nil
	// 只清空等待表并关闭 link，不关闭结果通道，避免与并发的 deliver 争抢造成 panic
	for id := range c.pending {
		delete(c.pending, id)
	}
	if c.link != nil {
		close(c.link)
		c.link = nil
	}
	c.inbound_qos2 = make(map[uint16]bool)
	c.applied = make(map[string]int)
	c.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
}

// Close 发送 DISCONNECT 并关闭连接，之后不再重连
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	cancel := c.cancel
	// 唤醒所有等待建连的调用方，避免它们一直挂到 ctx 超时
	select {
	case <-c.ready:
	default:
		close(c.ready)
	}
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	_ = c.send(wrap(packet_disconnect, 0, nil))
	c.detach()
	return nil
}

// AwaitConnection 等待连接就绪，ctx 结束时返回错误
func (c *Client) AwaitConnection(ctx context.Context) error {
	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return errors.New("MQTT 客户端已关闭")
		}
		if c.conn != nil {
			c.mu.Unlock()
			return nil
		}
		ready := c.ready
		c.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ready:
		}
	}
}

// send 串行化写出一条报文，避免多协程交错写坏帧
func (c *Client) send(data []byte) error {
	c.mu.Lock()
	conn, writer := c.conn, c.writer
	if conn == nil || writer == nil {
		c.mu.Unlock()
		return errors.New("MQTT 未连接")
	}
	_ = conn.SetWriteDeadline(time.Now().Add(c.opts.ConnectTimeout))
	_, err := writer.Write(data)
	if err == nil {
		err = writer.Flush()
	}
	c.mu.Unlock()
	if err != nil {
		return fmt.Errorf("发送 MQTT 报文失败: %w", err)
	}
	return nil
}

// backoff 计算第 attempt 次重连的等待时长
func (c *Client) backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Duration(attempt) * time.Second
	if delay > c.opts.MaxReconnectDelay {
		return c.opts.MaxReconnectDelay
	}
	return delay
}

// sleep 等待一段时间，ctx 结束时返回 false
func (c *Client) sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// notify 通知连接状态变化，回调在独立协程里执行以免阻塞主循环
func (c *Client) notify(up bool) {
	if c.opts.OnConnectionChange == nil {
		return
	}
	go c.opts.OnConnectionChange(up)
}

// default_port 返回各 scheme 的默认端口
func default_port(scheme string) string {
	switch strings.ToLower(scheme) {
	case "mqtts", "ssl", "tls":
		return "8883"
	default:
		return "1883"
	}
}

// is_tls_scheme 判断该 scheme 是否需要 TLS
func is_tls_scheme(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "mqtts", "ssl", "tls":
		return true
	default:
		return false
	}
}
