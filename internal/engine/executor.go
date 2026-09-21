// Package engine 把注册表、模板、传输和提取串成两条执行链：信息源采集与动作执行。
package engine

import (
	"context"
	"fmt"
	"time"

	"owdash/internal/config"
	"owdash/internal/extract"
	"owdash/internal/registry"
	"owdash/internal/state"
	"owdash/internal/template"
	"owdash/internal/transport"
)

// Executor 负责把一条注册项跑成一个真实结果
type Executor struct {
	store        *registry.Store
	states       *state.Store
	hub          *state.Hub
	secrets      *config.Secrets
	opts         transport.Options
	action_slots chan struct{}
}

// NewExecutor 创建执行器，action_workers 决定动作并发上限
func NewExecutor(store *registry.Store, states *state.Store, hub *state.Hub,
	secrets *config.Secrets, opts transport.Options, action_workers int) *Executor {
	if action_workers <= 0 {
		action_workers = 4
	}
	return &Executor{
		store:        store,
		states:       states,
		hub:          hub,
		secrets:      secrets,
		opts:         opts,
		action_slots: make(chan struct{}, action_workers),
	}
}

// Options 返回当前传输层约束
func (x *Executor) Options() transport.Options { return x.opts }

// scope 构造一次执行可见的模板作用域
func (x *Executor) scope(entry *registry.Entry, params map[string]any) template.Scope {
	merged := default_params(entry)
	for k, v := range params {
		merged[k] = v
	}
	secrets := x.secrets.ValuesCopy()
	// 暴露自身 API Token，方便注册项回调本服务（例如示例中的自检信息源）
	secrets["api_token"] = x.secrets.Token()
	return template.Scope{Params: merged, Secrets: secrets}
}

// RunSource 执行一次信息源采集，并把结果写入状态缓存后广播
func (x *Executor) RunSource(ctx context.Context, entry *registry.Entry) *state.SourceState {
	// 采集中这一帧沿用上一次的读数，避免前端每次轮询都闪一下空值
	running := &state.SourceState{ID: entry.ID, Status: state.StatusRunning, UpdatedAt: time.Now()}
	if prev := x.states.Get(entry.ID); prev != nil {
		running.Value = prev.Value
		running.Text = prev.Text
		running.LatencyMS = prev.LatencyMS
		running.UpdatedAt = prev.UpdatedAt
	}
	x.states.Set(running)
	x.hub.Publish(state.EventSourceUpdated, running)

	value, latency, detail, err := x.collect(ctx, entry)
	now := time.Now()

	final := &state.SourceState{ID: entry.ID, UpdatedAt: now, LatencyMS: latency, Detail: detail}
	if err != nil {
		prev := x.states.Get(entry.ID)
		final.Status = state.StatusError
		final.Error = err.Error()
		if prev != nil {
			final.Value = prev.Value
			final.Text = prev.Text
		}
	} else {
		final.Status = state.StatusOK
		final.Value = value.Value
		final.Text = value.Text
	}

	x.states.Set(final)
	x.hub.Publish(state.EventSourceUpdated, final)
	return final
}

// collect 完成渲染、传输和提取三步，返回提取结果
func (x *Executor) collect(ctx context.Context, entry *registry.Entry) (*extract.Result, int64, string, error) {
	rendered, err := render_transport(entry.Transport, x.scope(entry, nil))
	if err != nil {
		return nil, 0, "", err
	}
	start := time.Now()
	res, err := transport.Do(ctx, rendered, x.opts)
	latency := time.Since(start).Milliseconds()
	if res == nil {
		return nil, latency, "", err
	}
	if err != nil {
		return nil, latency, res.BodyText, err
	}
	value, err := extract.Extract(entry.Extract, res.Body)
	if err != nil {
		return nil, latency, res.BodyText, err
	}
	return value, latency, res.Detail, nil
}

// RunAction 执行一个动作，返回传输结果供前端提示
func (x *Executor) RunAction(ctx context.Context, entry *registry.Entry, params map[string]any) (*transport.Result, error) {
	select {
	case x.action_slots <- struct{}{}:
		defer func() { <-x.action_slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	checked, err := ValidateParams(entry, params)
	if err != nil {
		x.finish_action(entry, false, err.Error())
		return nil, err
	}
	rendered, err := render_transport(entry.Transport, x.scope(entry, checked))
	if err != nil {
		x.finish_action(entry, false, err.Error())
		return nil, err
	}
	res, err := transport.Do(ctx, rendered, x.opts)
	if err != nil {
		x.finish_action(entry, false, err.Error())
		return res, err
	}
	x.finish_action(entry, true, "")
	return res, nil
}

// finish_action 广播动作执行结果，前端据此弹 Toast
func (x *Executor) finish_action(entry *registry.Entry, ok bool, message string) {
	payload := map[string]any{
		"id":      entry.ID,
		"name":    entry.Name,
		"ok":      ok,
		"message": message,
	}
	x.hub.Publish(state.EventActionFinished, payload)
}

// default_params 取出注册项声明的参数默认值
func default_params(entry *registry.Entry) map[string]any {
	out := make(map[string]any, len(entry.Params))
	for name, spec := range entry.Params {
		if spec.Default != nil {
			out[name] = spec.Default
		}
	}
	return out
}

// RefreshSource 立即异步触发一次信息源刷新，自动刷新与手动刷新共用这条路径
func (x *Executor) RefreshSource(ctx context.Context, id string) error {
	entry, ok := x.store.Get(id)
	if !ok {
		return fmt.Errorf("注册项 %s 不存在", id)
	}
	if entry.Kind != registry.KindSource {
		return fmt.Errorf("注册项 %s 不是信息源", id)
	}
	go x.RunSource(ctx, entry)
	return nil
}
