package engine

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"owdash/internal/registry"
	"owdash/internal/state"
	"owdash/internal/transport"
)

// sub_retry_interval 是订阅建立失败后的重试间隔
const sub_retry_interval = 30 * time.Second

// err_missing_mqtt 表示注册项缺少 MQTT 配置
var err_missing_mqtt = errors.New("缺少 transport.mqtt 配置")

// mqtt_sub 是一条活跃的 MQTT 订阅
type mqtt_sub struct {
	id          string
	entry       *registry.Entry
	broker      string
	fingerprint string
	cancel      func()
	busy        atomic.Bool
	next_try    time.Time
}

// SubscriberManager 维护 MQTT 订阅型信息源的生命周期，
// 注册表变化时与它收敛，连接本身由 transport 的连接池复用。
type SubscriberManager struct {
	exec  *Executor
	store *registry.Store
	pool  *transport.MQTTPool

	mu     sync.Mutex
	active map[string]*mqtt_sub
	closed bool
}

// NewSubscriberManager 创建订阅管理器，pool 为 nil 时整个管理器为空操作
func NewSubscriberManager(exec *Executor, store *registry.Store, pool *transport.MQTTPool) *SubscriberManager {
	return &SubscriberManager{
		exec:   exec,
		store:  store,
		pool:   pool,
		active: make(map[string]*mqtt_sub),
	}
}

// Sync 让订阅集合与注册表保持一致，新增、变更、禁用都会在此收敛
func (m *SubscriberManager) Sync() {
	if m.pool == nil {
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	wanted := make(map[string]*registry.Entry)
	for _, entry := range m.store.ListByKind(registry.KindSource) {
		if entry.Enabled && entry.Subscribes() {
			wanted[entry.ID] = entry
		}
	}

	var to_cancel []func()
	for id, sub := range m.active {
		entry, ok := wanted[id]
		if !ok || fingerprint(entry) != sub.fingerprint {
			if sub.cancel != nil {
				to_cancel = append(to_cancel, sub.cancel)
			}
			delete(m.active, id)
		}
	}

	pending := make([]*mqtt_sub, 0)
	for id, entry := range wanted {
		if _, ok := m.active[id]; ok {
			continue
		}
		// 先占位，避免并发 Sync 重复建订阅
		sub := &mqtt_sub{id: id, entry: entry, fingerprint: fingerprint(entry), cancel: func() {}}
		m.active[id] = sub
		pending = append(pending, sub)
	}
	m.mu.Unlock()

	for _, cancel := range to_cancel {
		cancel()
	}
	for _, sub := range pending {
		m.attach(sub)
	}
}

// attach 渲染订阅配置并真正建立订阅，失败时登记重试时间
func (m *SubscriberManager) attach(sub *mqtt_sub) {
	entry := sub.entry
	rendered, err := render_transport(entry.Transport, m.exec.scope(entry, nil))
	if err != nil {
		m.fail(sub, "渲染订阅配置失败", err)
		return
	}
	if rendered.MQTT == nil {
		m.fail(sub, "缺少 MQTT 配置", err_missing_mqtt)
		return
	}
	spec := rendered.MQTT
	cancel, err := m.pool.Subscribe(spec, m.handler(sub))
	if err != nil {
		m.fail(sub, "建立订阅失败", err)
		return
	}
	broker, _ := registry.NormalizeBrokerURL(spec.Broker)

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		cancel()
		return
	}
	sub.cancel = cancel
	sub.broker = broker
	sub.next_try = time.Time{}
	m.mu.Unlock()

	m.exec.MarkSourceWaiting(entry, "已订阅 "+spec.Topic+"，等待 broker 推送")
}

// OnConnectionChange 由连接池回调，把 broker 的连通性映射到具体订阅源的状态
func (m *SubscriberManager) OnConnectionChange(broker string, up bool) {
	if m.pool == nil {
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	targets := make([]*mqtt_sub, 0)
	for _, sub := range m.active {
		if sub.broker == broker {
			targets = append(targets, sub)
		}
	}
	m.mu.Unlock()

	reason := fmt.Errorf("%s 连接已断开，等待自动重连", broker)
	for _, sub := range targets {
		if !up {
			m.exec.MarkSourceError(sub.entry, "broker 连接断开", reason)
			continue
		}
		// 重连后订阅由连接池自动恢复，这里只把状态从错误拉回等待
		switch m.exec.SourceStatus(sub.id) {
		case state.StatusError, state.StatusStale:
			m.exec.MarkSourceWaiting(sub.entry, "已重连 "+broker+"，等待消息")
		}
	}
}

// fail 记录订阅失败状态并安排下次重试
func (m *SubscriberManager) fail(sub *mqtt_sub, detail string, err error) {
	if err == nil {
		err = err_missing_mqtt
	}
	m.exec.MarkSourceError(sub.entry, detail, err)
	m.mu.Lock()
	sub.next_try = time.Now().Add(sub_retry_interval)
	m.mu.Unlock()
	log.Printf("信息源 %s %s: %v", sub.entry.ID, detail, err)
}

// handler 生成订阅回调，同一信息源的消息串行处理，处理不过来时丢弃旧消息
func (m *SubscriberManager) handler(sub *mqtt_sub) func(topic string, payload []byte) {
	limit := m.exec.Options().MaxBodyBytes
	return func(topic string, payload []byte) {
		if !sub.busy.CompareAndSwap(false, true) {
			return
		}
		defer sub.busy.Store(false)
		if limit > 0 && len(payload) > limit {
			payload = payload[:limit]
		}
		m.exec.ApplyMessage(sub.entry, topic, payload)
	}
}

// Retry 重试到期的失败订阅，由调度器的巡检线程周期调用
func (m *SubscriberManager) Retry() {
	if m.pool == nil {
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	now := time.Now()
	pending := make([]*mqtt_sub, 0)
	for _, sub := range m.active {
		if !sub.next_try.IsZero() && now.After(sub.next_try) {
			sub.next_try = time.Time{}
			pending = append(pending, sub)
		}
	}
	m.mu.Unlock()

	for _, sub := range pending {
		m.attach(sub)
	}
}

// Count 返回当前活跃订阅数
func (m *SubscriberManager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.active)
}

// Stop 取消全部订阅
func (m *SubscriberManager) Stop() {
	m.mu.Lock()
	m.closed = true
	cancels := make([]func(), 0, len(m.active))
	for _, sub := range m.active {
		if sub.cancel != nil {
			cancels = append(cancels, sub.cancel)
		}
	}
	m.active = make(map[string]*mqtt_sub)
	m.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
}
