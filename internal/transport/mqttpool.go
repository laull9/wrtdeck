package transport

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"wrtdeck/internal/mqtt"
	"wrtdeck/internal/registry"
)

// mqtt_idle_timeout 是订阅全部取消后连接的保留时长，
// 既让连续点击按钮不必重复握手，也避免在 OpenWrt 上长期占用 socket。
const mqtt_idle_timeout = 60 * time.Second

// MQTTOptions 是连接池的调优参数
type MQTTOptions struct {
	KeepaliveSeconds  int
	ConnectTimeout    time.Duration
	MaxReconnectDelay time.Duration
	MaxPayloadBytes   int
}

// DefaultMQTTOptions 返回一份可直接使用的连接池参数
func DefaultMQTTOptions() MQTTOptions {
	return MQTTOptions{
		KeepaliveSeconds:  30,
		ConnectTimeout:    5 * time.Second,
		MaxReconnectDelay: 30 * time.Second,
		MaxPayloadBytes:   256 * 1024,
	}
}

// message_handler 是订阅回调，收到消息时由上层完成提取与推送
type message_handler func(topic string, payload []byte)

// mqtt_subscription 是一条主题订阅
type mqtt_subscription struct {
	topic   string
	qos     int
	handler message_handler
}

// mqtt_conn 是一个 broker 上的共享长连接，订阅由它统一维护并在重连后自动恢复
type mqtt_conn struct {
	key    string
	url    string
	opts   MQTTOptions
	pool   *MQTTPool
	client *mqtt.Client
	cancel context.CancelFunc

	mu       sync.Mutex
	next_sub int
	subs     map[int]*mqtt_subscription
	idle     *time.Timer
	closed   bool
}

// MQTTPool 按 broker 维护共享长连接，同一 broker 不会重复建连
type MQTTPool struct {
	opts   MQTTOptions
	logger *log.Logger

	mu      sync.Mutex
	conns   map[string]*mqtt_conn
	watcher func(broker string, up bool)
	closed  bool
}

// NewMQTTPool 创建连接池，logger 为 nil 时使用标准日志
func NewMQTTPool(opts MQTTOptions, logger *log.Logger) *MQTTPool {
	if opts.KeepaliveSeconds <= 0 || opts.ConnectTimeout <= 0 || opts.MaxReconnectDelay <= 0 {
		opts = DefaultMQTTOptions()
	}
	if logger == nil {
		logger = log.Default()
	}
	return &MQTTPool{opts: opts, logger: logger, conns: make(map[string]*mqtt_conn)}
}

// SetConnectionWatcher 注册连接状态回调，broker 为规范化后的地址，
// 回调在 mqtt 客户端的协程里触发，实现方不得阻塞。
func (p *MQTTPool) SetConnectionWatcher(f func(broker string, up bool)) {
	p.mu.Lock()
	p.watcher = f
	p.mu.Unlock()
}

// watch 触发连接状态回调
func (p *MQTTPool) watch(broker string, up bool) {
	p.mu.Lock()
	watcher := p.watcher
	p.mu.Unlock()
	if watcher != nil {
		watcher(broker, up)
	}
}

// Publish 向指定主题发布一条消息，必要时建立共享连接
func (p *MQTTPool) Publish(ctx context.Context, spec *registry.MQTTSpec, payload []byte) error {
	conn, err := p.get_or_create(spec)
	if err != nil {
		return err
	}
	err = conn.publish(ctx, spec.Topic, payload, spec.QoS, spec.Retain)
	conn.arm_idle()
	return err
}

// Subscribe 订阅主题并返回取消函数，取消后引用归零则回收连接
func (p *MQTTPool) Subscribe(spec *registry.MQTTSpec, handler message_handler) (func(), error) {
	conn, err := p.get_or_create(spec)
	if err != nil {
		return nil, err
	}
	id, err := conn.add_subscription(spec.Topic, spec.QoS, handler)
	if err != nil {
		conn.arm_idle()
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			conn.remove_subscription(id)
			conn.arm_idle()
		})
	}, nil
}

// ConnCount 返回当前保持的连接数，供健康检查与调试使用
func (p *MQTTPool) ConnCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.conns)
}

// Close 断开所有连接，进程退出时调用
func (p *MQTTPool) Close() {
	p.mu.Lock()
	p.closed = true
	conns := make([]*mqtt_conn, 0, len(p.conns))
	for _, c := range p.conns {
		conns = append(conns, c)
	}
	p.conns = make(map[string]*mqtt_conn)
	p.mu.Unlock()

	for _, c := range conns {
		c.shutdown()
	}
}

// forget 把一条已关闭的连接移出连接表，避免后续取到死连接
func (p *MQTTPool) forget(key string) {
	p.mu.Lock()
	delete(p.conns, key)
	p.mu.Unlock()
}

// get_or_create 取出或建立一个共享连接
func (p *MQTTPool) get_or_create(spec *registry.MQTTSpec) (*mqtt_conn, error) {
	if spec == nil {
		return nil, errors.New("缺少 transport.mqtt 配置")
	}
	broker, err := registry.NormalizeBrokerURL(spec.Broker)
	if err != nil {
		return nil, err
	}
	key := conn_key(broker, spec.Username, spec.Password, spec.ClientID)

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, errors.New("MQTT 连接池已关闭")
	}
	if conn, ok := p.conns[key]; ok {
		conn.stop_idle()
		return conn, nil
	}
	conn := p.dial(broker, key, spec)
	p.conns[key] = conn
	return conn, nil
}

// dial 建立一个 broker 连接，客户端会在后台自行建连与重连
func (p *MQTTPool) dial(broker, key string, spec *registry.MQTTSpec) *mqtt_conn {
	client_id := spec.ClientID
	if client_id == "" {
		client_id = derive_client_id(key)
	}

	ctx, cancel := context.WithCancel(context.Background())
	conn := &mqtt_conn{
		key:    key,
		url:    broker,
		opts:   p.opts,
		pool:   p,
		cancel: cancel,
		subs:   make(map[int]*mqtt_subscription),
	}
	conn.client = mqtt.NewClient(mqtt.Options{
		BrokerURL:          broker,
		ClientID:           client_id,
		Username:           spec.Username,
		Password:           spec.Password,
		Keepalive:          time.Duration(p.opts.KeepaliveSeconds) * time.Second,
		ConnectTimeout:     p.opts.ConnectTimeout,
		MaxReconnectDelay:  p.opts.MaxReconnectDelay,
		CleanSession:       true,
		OnMessage:          conn.on_message,
		OnConnectionChange: func(up bool) { p.watch(broker, up) },
		Logger:             p.logger,
	})
	conn.client.Start(ctx)
	return conn
}

// publish 等待连接就绪后发布消息
func (c *mqtt_conn) publish(ctx context.Context, topic string, payload []byte, qos int, retain bool) error {
	if err := c.client.AwaitConnection(ctx); err != nil {
		return fmt.Errorf("连接 MQTT broker %s 失败: %w", c.url, err)
	}
	if err := c.client.Publish(ctx, topic, payload, qos, retain); err != nil {
		return fmt.Errorf("MQTT 发布到 %s 失败: %w", topic, err)
	}
	return nil
}

// add_subscription 登记一条订阅并立即向 broker 收敛
func (c *mqtt_conn) add_subscription(topic string, qos int, handler message_handler) (int, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, errors.New("MQTT 连接已关闭")
	}
	c.next_sub++
	id := c.next_sub
	c.subs[id] = &mqtt_subscription{topic: topic, qos: qos, handler: handler}
	desired := c.desired_locked()
	c.mu.Unlock()

	if err := c.client.SetSubscriptions(desired); err != nil {
		c.mu.Lock()
		delete(c.subs, id)
		c.mu.Unlock()
		return 0, err
	}
	return id, nil
}

// remove_subscription 撤销一条订阅，同一主题不再被引用时才真正退订
func (c *mqtt_conn) remove_subscription(id int) {
	c.mu.Lock()
	if _, ok := c.subs[id]; !ok {
		c.mu.Unlock()
		return
	}
	delete(c.subs, id)
	desired := c.desired_locked()
	c.mu.Unlock()

	// 连接断开时收敛会失败，重连后由客户端按期望集合自动对齐，无需在此重试
	_ = c.client.SetSubscriptions(desired)
}

// desired_locked 汇总当前期望订阅的主题与 QoS，调用方需持有锁
func (c *mqtt_conn) desired_locked() map[string]int {
	out := make(map[string]int, len(c.subs))
	for _, sub := range c.subs {
		if qos, ok := out[sub.topic]; !ok || sub.qos > qos {
			out[sub.topic] = sub.qos
		}
	}
	return out
}

// on_message 把收到的消息分发给命中的订阅，通配符由本地匹配完成
func (c *mqtt_conn) on_message(topic string, payload []byte) {
	c.mu.Lock()
	list := make([]*mqtt_subscription, 0, len(c.subs))
	for _, sub := range c.subs {
		list = append(list, sub)
	}
	c.mu.Unlock()

	for _, sub := range list {
		if !mqtt.TopicMatches(sub.topic, topic) {
			continue
		}
		sub.handler(topic, payload)
	}
}

// stop_idle 取消空闲回收计时
func (c *mqtt_conn) stop_idle() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.idle != nil {
		c.idle.Stop()
		c.idle = nil
	}
}

// arm_idle 在没有订阅时启动空闲回收，仍有订阅则不动作
func (c *mqtt_conn) arm_idle() {
	c.mu.Lock()
	if c.closed || len(c.subs) > 0 || c.idle != nil {
		c.mu.Unlock()
		return
	}
	c.idle = time.AfterFunc(mqtt_idle_timeout, func() {
		c.mu.Lock()
		idle_now := len(c.subs) == 0
		c.mu.Unlock()
		if !idle_now {
			return
		}
		// 先从连接表摘除再断开，避免后续取到已关闭的连接
		c.pool.forget(c.key)
		c.shutdown()
	})
	c.mu.Unlock()
}

// shutdown 断开连接并标记关闭
func (c *mqtt_conn) shutdown() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	if c.idle != nil {
		c.idle.Stop()
		c.idle = nil
	}
	c.mu.Unlock()

	_ = c.client.Close()
	c.cancel()
}
