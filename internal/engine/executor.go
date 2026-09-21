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
	history      *state.History
	secrets      *config.Secrets
	opts         transport.Options
	action_slots chan struct{}
}

// NewExecutor 创建执行器，action_workers 决定动作并发上限
func NewExecutor(store *registry.Store, states *state.Store, hub *state.Hub, history *state.History,
	secrets *config.Secrets, opts transport.Options, action_workers int) *Executor {
	if action_workers <= 0 {
		action_workers = 4
	}
	if history == nil {
		history = state.NewHistory(0)
	}
	return &Executor{
		store:        store,
		states:       states,
		hub:          hub,
		history:      history,
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
	final := &state.SourceState{ID: entry.ID, UpdatedAt: time.Now(), LatencyMS: latency, Detail: detail}
	if err != nil {
		return x.store_error(entry, final, err)
	}
	final.Status = state.StatusOK
	final.Value = value.Value
	final.Text = value.Text
	return x.commit(final)
}

// ApplyMessage 用 MQTT 推送的消息更新信息源状态，与轮询共用同一条提取链路
func (x *Executor) ApplyMessage(entry *registry.Entry, topic string, payload []byte) *state.SourceState {
	value, err := extract.Extract(entry.Extract, payload)
	final := &state.SourceState{ID: entry.ID, UpdatedAt: time.Now(), Detail: "来自 " + topic}
	if err != nil {
		return x.store_error(entry, final, err)
	}
	final.Status = state.StatusOK
	final.Value = value.Value
	final.Text = value.Text
	return x.commit(final)
}

// MarkSourceWaiting 标记订阅已建立但尚未收到第一条消息
func (x *Executor) MarkSourceWaiting(entry *registry.Entry, detail string) *state.SourceState {
	st := &state.SourceState{ID: entry.ID, UpdatedAt: time.Now(), Status: state.StatusWaiting, Detail: detail}
	if prev := x.states.Get(entry.ID); prev != nil {
		st.Value = prev.Value
		st.Text = prev.Text
	}
	x.states.Set(st)
	x.hub.Publish(state.EventSourceUpdated, st)
	return st
}

// MarkSourceError 标记订阅建立失败，沿用上一次读数
func (x *Executor) MarkSourceError(entry *registry.Entry, detail string, err error) *state.SourceState {
	st := &state.SourceState{ID: entry.ID, UpdatedAt: time.Now(), Detail: detail}
	return x.store_error(entry, st, err)
}

// commit 写入状态、记录采样并广播
func (x *Executor) commit(st *state.SourceState) *state.SourceState {
	x.states.Set(st)
	x.history.Append(st.ID, state.Sample{
		At:        st.UpdatedAt,
		Value:     st.Value,
		Status:    st.Status,
		LatencyMS: st.LatencyMS,
	})
	x.hub.Publish(state.EventSourceUpdated, st)
	return st
}

// store_error 在失败时沿用上一次读数，避免界面闪空
func (x *Executor) store_error(entry *registry.Entry, st *state.SourceState, err error) *state.SourceState {
	st.Status = state.StatusError
	st.Error = err.Error()
	if prev := x.states.Get(entry.ID); prev != nil {
		st.Value = prev.Value
		st.Text = prev.Text
		if st.LatencyMS == 0 {
			st.LatencyMS = prev.LatencyMS
		}
	}
	return x.commit(st)
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

// SourceStatus 返回某个信息源当前的状态取值，未知时返回 unknown
func (x *Executor) SourceStatus(id string) string {
	if st := x.states.Get(id); st != nil && st.Status != "" {
		return st.Status
	}
	return state.StatusUnknown
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
	if entry.Subscribes() {
		return transport.ErrMQTTSubscribePush
	}
	go x.RunSource(ctx, entry)
	return nil
}
