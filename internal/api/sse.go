package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"wrtdeck/internal/state"
)

// heartbeat_interval 是 SSE 心跳间隔，用来穿过中间设备的空闲超时
const heartbeat_interval = 20 * time.Second

// handle_events 建立 SSE 长连接，持续推送状态与动作事件
func (s *Server) handle_events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		write_error(w, http.StatusInternalServerError, "no_flush", "当前连接不支持流式推送")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	events, cancel := s.hub.Subscribe()
	defer cancel()

	// 首帧告诉前端连接已就绪，便于界面显示在线状态
	write_sse(w, "hello", map[string]any{"server": s.info(), "at": time.Now()})
	flusher.Flush()

	heartbeat := time.NewTicker(heartbeat_interval)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			// 注释帧用作心跳，客户端会忽略
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case event, open := <-events:
			if !open {
				return
			}
			if err := write_sse(w, event.Type, event.Data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// write_sse 按 SSE 协议写出一帧事件
func write_sse(w http.ResponseWriter, event_type string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		payload = []byte(`{"error":"序列化事件失败"}`)
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event_type, payload)
	return err
}

// 编译期断言：Event 结构必须能被 SSE 直接序列化
var _ = state.Event{}
