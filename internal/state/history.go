package state

import (
	"sync"
	"time"
)

// Sample 是一次采集留下的采样点，仅驻内存
type Sample struct {
	At        time.Time `json:"at"`
	Value     any       `json:"value"`
	Status    string    `json:"status"`
	LatencyMS int64     `json:"latency_ms"`
}

// History 是各信息源的定长采样缓冲，配置为 0 时完全不记录，
// 因此默认不会因为高频采集而磨损 OpenWrt 设备的 flash。
type History struct {
	limit int

	mu    sync.RWMutex
	items map[string][]Sample
}

// NewHistory 创建历史缓冲，limit 为每个信息源保留的采样条数
func NewHistory(limit int) *History {
	if limit < 0 {
		limit = 0
	}
	return &History{limit: limit, items: make(map[string][]Sample)}
}

// Enabled 表示当前配置是否开启了采样记录
func (h *History) Enabled() bool { return h.limit > 0 }

// Limit 返回每个信息源保留的采样条数
func (h *History) Limit() int { return h.limit }

// Append 追加一条采样，超出容量时丢弃最旧的一条
func (h *History) Append(id string, s Sample) {
	if h.limit <= 0 || id == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	buf := append(h.items[id], s)
	if len(buf) > h.limit {
		buf = append([]Sample(nil), buf[len(buf)-h.limit:]...)
	}
	h.items[id] = buf
}

// Get 返回某个信息源的采样，limit 大于 0 时只取最近 limit 条
func (h *History) Get(id string, limit int) []Sample {
	h.mu.RLock()
	defer h.mu.RUnlock()
	buf := h.items[id]
	if limit > 0 && len(buf) > limit {
		buf = buf[len(buf)-limit:]
	}
	return append([]Sample(nil), buf...)
}

// Delete 清掉某个信息源的采样，注册项被删除时调用
func (h *History) Delete(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.items, id)
}

// Count 返回已记录采样的信息源数量，用于健康检查
func (h *History) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.items)
}
