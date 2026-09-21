package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Store 是注册表的内存索引 + JSON 持久化，只有写操作才落盘
type Store struct {
	path    string
	mu      sync.RWMutex
	entries map[string]*Entry
}

// NewStore 创建注册表，path 为空表示只驻留内存
func NewStore(path string) *Store {
	return &Store{path: path, entries: make(map[string]*Entry)}
}

// Load 从磁盘读取注册表，文件不存在时视为空表
func (s *Store) Load() error {
	if s.path == "" {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var entries []*Entry
	if err = json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("解析注册表失败: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = make(map[string]*Entry, len(entries))
	for _, e := range entries {
		s.entries[e.ID] = e
	}
	return nil
}

// Save 原子写回磁盘，先写临时文件再 rename，避免断电写坏
func (s *Store) Save() error {
	if s.path == "" {
		return nil
	}
	s.mu.RLock()
	entries := make([]*Entry, 0, len(s.entries))
	for _, e := range s.entries {
		entries = append(entries, e)
	}
	s.mu.RUnlock()
	sort_entries(entries)

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if err = os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// List 返回按分组和顺序排好的注册项副本
func (s *Store) List() []*Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Entry, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, clone(e))
	}
	sort_entries(out)
	return out
}

// ListByKind 返回指定类型（source/action）的注册项副本
func (s *Store) ListByKind(kind string) []*Entry {
	all := s.List()
	out := make([]*Entry, 0, len(all))
	for _, e := range all {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// Get 按键取回注册项副本
func (s *Store) Get(id string) (*Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[id]
	if !ok {
		return nil, false
	}
	return clone(e), true
}

// Put 写入或覆盖一个注册项，并立即落盘
func (s *Store) Put(e *Entry) error {
	s.mu.Lock()
	s.entries[e.ID] = clone(e)
	s.mu.Unlock()
	return s.Save()
}

// Delete 删除一个注册项，键不存在时返回 false
func (s *Store) Delete(id string) (bool, error) {
	s.mu.Lock()
	_, ok := s.entries[id]
	delete(s.entries, id)
	s.mu.Unlock()
	if !ok {
		return false, nil
	}
	return true, s.Save()
}

// Count 返回注册项总数
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

// clone 用 JSON 往返复制注册项，避免调用方拿到内部引用
func clone(e *Entry) *Entry {
	data, err := json.Marshal(e)
	if err != nil {
		return &Entry{}
	}
	out := &Entry{}
	if err = json.Unmarshal(data, out); err != nil {
		return &Entry{}
	}
	return out
}

// sort_entries 按分组、UI 顺序、ID 稳定排序
func sort_entries(entries []*Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Group != b.Group {
			return a.Group < b.Group
		}
		if a.UI.Order != b.UI.Order {
			return a.UI.Order < b.UI.Order
		}
		return a.ID < b.ID
	})
}
