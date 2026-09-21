package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"wrtdeck/internal/registry"
	"wrtdeck/internal/state"
	"wrtdeck/internal/transport"
)

// handle_registry_list 返回全部注册项
func (s *Server) handle_registry_list(w http.ResponseWriter, r *http.Request) {
	write_json(w, http.StatusOK, map[string]any{"entries": s.store.List()})
}

// handle_registry_put 幂等地注册或更新一条注册项
func (s *Server) handle_registry_put(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var entry registry.Entry
	if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
		write_error(w, http.StatusBadRequest, "bad_json", "请求体不是合法 JSON: "+err.Error())
		return
	}
	// 路径上的 ID 优先级最高，保证 PUT 语义幂等
	entry.ID = id
	if err := registry.Validate(&entry); err != nil {
		write_error(w, http.StatusBadRequest, "invalid_entry", err.Error())
		return
	}
	if existing, ok := s.store.Get(id); ok && existing.Kind != entry.Kind {
		write_error(w, http.StatusConflict, "kind_changed", "已存在的注册项类型不同，不能通过更新切换")
		return
	}
	if err := s.store.Put(&entry); err != nil {
		write_error(w, http.StatusInternalServerError, "store_failed", "写入注册表失败: "+err.Error())
		return
	}
	if entry.Kind == registry.KindSource && entry.Enabled {
		go s.exec.RefreshSource(r.Context(), entry.ID)
	}
	s.after_registry_change(entry.ID, "upsert")
	write_json(w, http.StatusOK, entry)
}

// handle_registry_delete 删除一条注册项
func (s *Server) handle_registry_delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	deleted, err := s.store.Delete(id)
	if err != nil {
		write_error(w, http.StatusInternalServerError, "store_failed", "删除失败: "+err.Error())
		return
	}
	if !deleted {
		write_error(w, http.StatusNotFound, "not_found", "注册项不存在")
		return
	}
	s.states.Delete(id)
	s.history.Delete(id)
	s.after_registry_change(id, "delete")
	w.WriteHeader(http.StatusNoContent)
}

// after_registry_change 让调度器收敛到最新注册表，并广播变更事件
func (s *Server) after_registry_change(id, action string) {
	s.sched.Sync()
	s.hub.Publish(state.EventRegistryChanged, map[string]any{"id": id, "action": action})
}

// run_request 是执行动作的请求体
type run_request struct {
	Params map[string]any `json:"params"`
}

// run_response 是动作执行的返回体
type run_response struct {
	ID      string `json:"id"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	Latency int64  `json:"latency_ms"`
	Preview string `json:"preview,omitempty"`
}

// handle_action_run 执行一个动作
func (s *Server) handle_action_run(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	entry, ok := s.store.Get(id)
	if !ok || entry.Kind != registry.KindAction {
		write_error(w, http.StatusNotFound, "not_found", "动作不存在")
		return
	}
	if !entry.Enabled {
		write_error(w, http.StatusConflict, "disabled", "该动作已被禁用")
		return
	}

	req := run_request{}
	if r.Body != nil {
		// 无参动作允许完全不带请求体，此时 io.EOF 属于正常情况
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			write_error(w, http.StatusBadRequest, "bad_json", "请求体不是合法 JSON: "+err.Error())
			return
		}
	}
	if req.Params == nil {
		req.Params = map[string]any{}
	}

	// 单次动作的执行时间上限由传输层超时决定，这里只兜底取消信号
	result, err := s.exec.RunAction(r.Context(), entry, req.Params)
	if err != nil {
		write_error(w, http.StatusBadGateway, "run_failed", err.Error())
		return
	}
	write_json(w, http.StatusOK, run_response{
		ID:      entry.ID,
		OK:      true,
		Message: result.Detail,
		Latency: result.LatencyMS,
		Preview: result.BodyText,
	})
}

// handle_source_refresh 手动触发一次信息源采集
func (s *Server) handle_source_refresh(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.store.Get(id); !ok {
		write_error(w, http.StatusNotFound, "not_found", "注册项不存在")
		return
	}
	err := s.exec.RefreshSource(r.Context(), id)
	if errors.Is(err, transport.ErrMQTTSubscribePush) {
		write_error(w, http.StatusConflict, "push_source", err.Error())
		return
	}
	if err != nil {
		write_error(w, http.StatusBadRequest, "refresh_failed", err.Error())
		return
	}
	write_json(w, http.StatusAccepted, map[string]any{"id": id, "status": "accepted"})
}
