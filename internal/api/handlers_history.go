package api

import (
	"net/http"
	"strconv"

	"wrtdeck/internal/registry"
	"wrtdeck/internal/state"
)

// history_response 是信息源采样历史的返回体
type history_response struct {
	ID      string         `json:"id"`
	Enabled bool           `json:"enabled"`
	Limit   int            `json:"limit"`
	Samples []state.Sample `json:"samples"`
}

// handle_source_history 返回某个信息源的采样历史。
// 采样只驻内存，limits.history_limit 为 0 时恒返回空列表。
func (s *Server) handle_source_history(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	entry, ok := s.store.Get(id)
	if !ok {
		write_error(w, http.StatusNotFound, "not_found", "注册项不存在")
		return
	}
	if entry.Kind != registry.KindSource {
		write_error(w, http.StatusBadRequest, "not_source", "只有信息源才有采样历史")
		return
	}

	limit := parse_limit(r.URL.Query().Get("limit"), s.history.Limit())
	samples := s.history.Get(id, limit)
	if samples == nil {
		samples = []state.Sample{}
	}
	write_json(w, http.StatusOK, history_response{
		ID:      id,
		Enabled: s.history.Enabled(),
		Limit:   s.history.Limit(),
		Samples: samples,
	})
}

// parse_limit 解析 limit 查询参数，非法或未提供时回落到默认值
func parse_limit(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	if fallback > 0 && value > fallback {
		return fallback
	}
	return value
}
