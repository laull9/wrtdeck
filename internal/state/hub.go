package state

import (
	"sync"
	"time"
)

// 事件类型
const (
	EventSourceUpdated   = "source.updated"
	EventActionFinished  = "action.finished"
	EventRegistryChanged = "registry.changed"
)

// subscriber_buffer 是单个订阅者的缓冲深度，写满时直接丢弃旧事件
const subscriber_buffer = 16

// Event 是要推送给浏览器的实时事件
type Event struct {
	Type string    `json:"type"`
	At   time.Time `json:"at"`
	Data any       `json:"data"`
}

// Hub 负责把事件广播给所有 SSE 订阅者
type Hub struct {
	mu     sync.RWMutex
	next   int
	subs   map[int]chan Event
	closed bool
}

// NewHub 创建一个事件中心
func NewHub() *Hub {
	return &Hub{subs: make(map[int]chan Event)}
}

// Publish 向所有订阅者广播事件，写不进去的订阅者会被跳过
func (h *Hub) Publish(event_type string, data any) {
	event := Event{Type: event_type, At: time.Now(), Data: data}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.closed {
		return
	}
	for _, ch := range h.subs {
		select {
		case ch <- event:
		default:
		}
	}
}

// Subscribe 注册一个订阅者并返回事件通道与取消函数
func (h *Hub) Subscribe() (<-chan Event, func()) {
	h.mu.Lock()
	if h.closed {
		closed := make(chan Event)
		close(closed)
		h.mu.Unlock()
		return closed, func() {}
	}
	id := h.next
	h.next++
	ch := make(chan Event, subscriber_buffer)
	h.subs[id] = ch
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, id)
			h.mu.Unlock()
			close(ch)
		})
	}
	return ch, cancel
}

// Subscribers 返回当前订阅者数量，用于健康检查
func (h *Hub) Subscribers() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}

// Close 关闭事件中心并断开所有订阅者
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for id, ch := range h.subs {
		delete(h.subs, id)
		close(ch)
	}
}
