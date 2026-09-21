// Package state 保存信息源的运行时状态，并向 SSE 订阅者广播事件。
// 所有状态只驻留内存，重启后重新采集，因此不会因为高频数据磨损 flash。
package state

import (
	"sync"
	"time"
)

// 信息源状态取值
const (
	StatusUnknown = "unknown"
	StatusRunning = "running"
	// StatusWaiting 表示订阅已经建立，正在等待 broker 推送第一条消息
	StatusWaiting = "waiting"
	StatusOK      = "ok"
	StatusError   = "error"
	StatusStale   = "stale"
)

// SourceState 是单个信息源的运行快照
type SourceState struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Value     any       `json:"value"`
	Text      string    `json:"text,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	LatencyMS int64     `json:"latency_ms"`
	Error     string    `json:"error,omitempty"`
	Detail    string    `json:"detail,omitempty"`
}

// Clone 返回状态副本，避免调用方直接持有内部结构
func (s *SourceState) Clone() *SourceState {
	if s == nil {
		return nil
	}
	out := *s
	return &out
}

// Store 是信息源状态的内存索引
type Store struct {
	mu    sync.RWMutex
	items map[string]*SourceState
}

// NewStore 创建一个空的状态存储
func NewStore() *Store {
	return &Store{items: make(map[string]*SourceState)}
}

// Set 写入或覆盖一个信息源状态
func (s *Store) Set(st *SourceState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[st.ID] = st.Clone()
}

// Get 取出一个信息源状态副本
func (s *Store) Get(id string) *SourceState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.items[id].Clone()
}

// Snapshot 返回全部状态副本
func (s *Store) Snapshot() map[string]*SourceState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]*SourceState, len(s.items))
	for id, st := range s.items {
		out[id] = st.Clone()
	}
	return out
}

// Delete 移除某个信息源的状态
func (s *Store) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, id)
}

// MarkRunning 把信息源标记为采集中的状态
func (s *Store) MarkRunning(id string) *SourceState {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.items[id]
	if st == nil {
		st = &SourceState{ID: id}
		s.items[id] = st
	}
	st.Status = StatusRunning
	return st.Clone()
}

// MarkStale 把长时间没有更新的信息源降级为 stale
func (s *Store) MarkStale(id string) *SourceState {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.items[id]
	if st == nil {
		return nil
	}
	if st.Status == StatusOK {
		st.Status = StatusStale
	}
	return st.Clone()
}
