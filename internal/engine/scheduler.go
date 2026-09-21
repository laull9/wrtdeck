package engine

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"wrtdeck/internal/registry"
	"wrtdeck/internal/state"
)

// stale_factor 表示超过多少个轮询周期没更新就把信息源降级为 stale
const stale_factor = 3

// janitor_interval 是过期状态巡检的间隔
const janitor_interval = 10 * time.Second

// source_job 是一个信息源的轮询协程
type source_job struct {
	entry       *registry.Entry
	fingerprint string
	ctx         context.Context
	cancel      context.CancelFunc
	running     atomic.Bool
}

// Scheduler 按注册表驱动所有信息源的定时采集
type Scheduler struct {
	exec           *Executor
	store          *registry.Store
	states         *state.Store
	hub            *state.Hub
	subs           *SubscriberManager
	fallback_ms    int
	min_interval   time.Duration
	source_workers chan struct{}

	mu   sync.Mutex
	jobs map[string]*source_job

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewScheduler 创建调度器，fallback_ms 是未配置 interval 时的默认周期
func NewScheduler(exec *Executor, store *registry.Store, states *state.Store, hub *state.Hub,
	source_workers, fallback_ms, min_interval_ms int) *Scheduler {
	if source_workers <= 0 {
		source_workers = 4
	}
	if min_interval_ms <= 0 {
		min_interval_ms = 1000
	}
	return &Scheduler{
		exec:           exec,
		store:          store,
		states:         states,
		hub:            hub,
		fallback_ms:    fallback_ms,
		min_interval:   time.Duration(min_interval_ms) * time.Millisecond,
		source_workers: make(chan struct{}, source_workers),
		jobs:           make(map[string]*source_job),
	}
}

// SetSubscribers 注入订阅管理器，轮询与订阅在 Sync 时一并收敛
func (s *Scheduler) SetSubscribers(m *SubscriberManager) { s.subs = m }

// Start 启动调度器并按当前注册表建立轮询协程
func (s *Scheduler) Start(parent context.Context) {
	s.mu.Lock()
	s.ctx, s.cancel = context.WithCancel(parent)
	s.mu.Unlock()

	s.Sync()
	s.wg.Add(1)
	go s.janitor()
}

// Stop 停止全部轮询协程并等待退出
func (s *Scheduler) Stop() {
	if s.subs != nil {
		s.subs.Stop()
	}

	s.mu.Lock()
	cancel := s.cancel
	jobs := s.jobs
	s.jobs = make(map[string]*source_job)
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	for _, job := range jobs {
		job.cancel()
	}
	s.wg.Wait()
}

// Sync 让轮询协程集合与注册表保持一致，新增、变更、删除都会在此收敛。
// 订阅型信息源不参与轮询，交给订阅管理器处理。
func (s *Scheduler) Sync() {
	s.sync_jobs()
	// 订阅收敛会做网络握手，放在锁外执行，避免阻塞调度器本身
	if s.subs != nil {
		s.subs.Sync()
	}
}

// sync_jobs 收敛轮询协程集合
func (s *Scheduler) sync_jobs() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx == nil {
		return
	}

	wanted := make(map[string]*registry.Entry)
	for _, entry := range s.store.ListByKind(registry.KindSource) {
		if entry.Enabled && !entry.Subscribes() {
			wanted[entry.ID] = entry
		}
	}

	for id, job := range s.jobs {
		entry, ok := wanted[id]
		if !ok || fingerprint(entry) != job.fingerprint {
			job.cancel()
			delete(s.jobs, id)
		}
	}

	for id, entry := range wanted {
		if _, ok := s.jobs[id]; ok {
			continue
		}
		s.jobs[id] = s.start_job(entry)
	}
}

// start_job 为一个信息源建立轮询协程，调用方需持有锁
func (s *Scheduler) start_job(entry *registry.Entry) *source_job {
	ctx, cancel := context.WithCancel(s.ctx)
	job := &source_job{
		entry:       entry,
		fingerprint: fingerprint(entry),
		ctx:         ctx,
		cancel:      cancel,
	}
	s.wg.Add(1)
	go s.loop(job)
	return job
}

// loop 是该信息源的轮询主循环，启动后立即采集一次
func (s *Scheduler) loop(job *source_job) {
	defer s.wg.Done()
	interval := s.interval_of(job.entry)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	s.tick(job)
	for {
		select {
		case <-job.ctx.Done():
			return
		case <-ticker.C:
			s.tick(job)
		}
	}
}

// tick 触发一次采集，上一次未完成或工作池已满时直接跳过
func (s *Scheduler) tick(job *source_job) {
	if !job.running.CompareAndSwap(false, true) {
		return
	}
	select {
	case s.source_workers <- struct{}{}:
	default:
		job.running.Store(false)
		return
	}
	go func() {
		defer func() {
			<-s.source_workers
			job.running.Store(false)
		}()
		s.exec.RunSource(job.ctx, job.entry)
	}()
}

// interval_of 计算信息源的实际轮询周期，并保证不低于下限
func (s *Scheduler) interval_of(entry *registry.Entry) time.Duration {
	interval := time.Duration(entry.IntervalMS(s.fallback_ms)) * time.Millisecond
	if interval < s.min_interval {
		return s.min_interval
	}
	return interval
}

// janitor 定期把长期未更新的信息源降级为 stale
func (s *Scheduler) janitor() {
	defer s.wg.Done()
	ticker := time.NewTicker(janitor_interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.sweep()
			if s.subs != nil {
				s.subs.Retry()
			}
		}
	}
}

// sweep 遍历所有状态，把超时未更新的标记为 stale
func (s *Scheduler) sweep() {
	now := time.Now()
	for _, entry := range s.store.ListByKind(registry.KindSource) {
		if !entry.Enabled {
			continue
		}
		st := s.states.Get(entry.ID)
		if st == nil || st.Status != state.StatusOK {
			continue
		}
		if now.Sub(st.UpdatedAt) > stale_factor*s.interval_of(entry) {
			if next := s.states.MarkStale(entry.ID); next != nil {
				s.hub.Publish(state.EventSourceUpdated, next)
			}
		}
	}
}

// fingerprint 用来判断注册项是否发生了变化
func fingerprint(entry *registry.Entry) string {
	data, err := json.Marshal(entry)
	if err != nil {
		return entry.ID
	}
	return string(data)
}
