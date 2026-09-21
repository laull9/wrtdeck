package api

import (
	"net/http"
	"time"

	"wrtdeck/internal/config"
	"wrtdeck/internal/engine"
	"wrtdeck/internal/registry"
	"wrtdeck/internal/state"
	"wrtdeck/internal/transport"
)

// Server 汇总 API 层需要的全部依赖
type Server struct {
	cfg      *config.Config
	store    *registry.Store
	states   *state.Store
	hub      *state.Hub
	history  *state.History
	pool     *transport.MQTTPool
	exec     *engine.Executor
	sched    *engine.Scheduler
	secrets  *config.Secrets
	handoffs *handoff_store
	sessions *session_store
	logins   *login_throttle
	webui    http.Handler
	version  string
	dev      bool
	started  time.Time
}

// NewServer 创建 API 服务
func NewServer(cfg *config.Config, store *registry.Store, states *state.Store, hub *state.Hub,
	history *state.History, pool *transport.MQTTPool, exec *engine.Executor, sched *engine.Scheduler,
	secrets *config.Secrets, webui http.Handler, version string, dev bool) *Server {
	if history == nil {
		history = state.NewHistory(0)
	}
	return &Server{
		cfg:      cfg,
		store:    store,
		states:   states,
		hub:      hub,
		history:  history,
		pool:     pool,
		exec:     exec,
		sched:    sched,
		secrets:  secrets,
		handoffs: new_handoff_store(),
		sessions: new_session_store(cfg.Auth.SessionTTL()),
		logins:   new_login_throttle(cfg.Auth.MaxAttempts(), cfg.Auth.Lockout()),
		webui:    webui,
		version:  version,
		dev:      dev,
		started:  time.Now(),
	}
}

// Handler 组装最终的路由与中间件栈
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", s.handle_health)
	mux.HandleFunc("GET /api/v1/dashboard", s.handle_dashboard)
	mux.HandleFunc("GET /api/v1/registry", s.handle_registry_list)
	mux.HandleFunc("PUT /api/v1/registry/{id}", s.handle_registry_put)
	mux.HandleFunc("DELETE /api/v1/registry/{id}", s.handle_registry_delete)
	mux.HandleFunc("POST /api/v1/actions/{id}/run", s.handle_action_run)
	mux.HandleFunc("POST /api/v1/sources/{id}/refresh", s.handle_source_refresh)
	mux.HandleFunc("GET /api/v1/sources/{id}/history", s.handle_source_history)
	mux.HandleFunc("GET /api/v1/events", s.handle_events)

	// 登录与会话：口令换会话凭据、查询凭据状态、改口令、登出
	mux.HandleFunc("POST /api/v1/session/login", s.handle_session_login)
	mux.HandleFunc("GET /api/v1/session/me", s.handle_session_me)
	mux.HandleFunc("POST /api/v1/session/password", s.handle_session_password)
	mux.HandleFunc("POST /api/v1/session/logout", s.handle_session_logout)
	// 设备本机（LuCI 薄壳）申请一次性登录码，浏览器用它换会话凭据
	mux.HandleFunc("POST /api/v1/session/handoff", s.handle_session_handoff)
	mux.HandleFunc("POST /api/v1/session/redeem", s.handle_session_redeem)

	mux.Handle("/", s.webui)

	return middleware(mux,
		with_logging,
		s.with_security_headers,
		func(next http.Handler) http.Handler { return with_cors(s.dev, next) },
		func(next http.Handler) http.Handler { return with_limit(s.cfg.Limits.MaxRequestBytes, next) },
		s.with_auth,
	)
}

// server_info 是给前端展示的服务元信息。
// 这里不含任何凭据，只暴露前端决定「显示什么」所需的状态。
type server_info struct {
	Version     string    `json:"version"`
	UptimeS     float64   `json:"uptime_s"`
	StartedAt   time.Time `json:"started_at"`
	Dev         bool      `json:"dev"`
	AuthOff     bool      `json:"auth_disabled"`
	MustChange  bool      `json:"must_change_password"`
	SessionTTLM int       `json:"session_ttl_minutes"`
	TLS         bool      `json:"tls_enabled"`
	Sources     int       `json:"sources"`
	Actions     int       `json:"actions"`
	Subscribers int       `json:"subscribers"`
}

// health_response 是健康检查的返回体
type health_response struct {
	Status  string      `json:"status"`
	Version string      `json:"version"`
	UptimeS float64     `json:"uptime_s"`
	Server  server_info `json:"server"`
}

// handle_health 返回服务健康状态，不需要鉴权
func (s *Server) handle_health(w http.ResponseWriter, r *http.Request) {
	write_json(w, http.StatusOK, health_response{
		Status:  "ok",
		Version: s.version,
		UptimeS: time.Since(s.started).Seconds(),
		Server:  s.info(),
	})
}

// source_item 是信息源注册项与其运行时状态的组合
type source_item struct {
	Entry *registry.Entry    `json:"entry"`
	State *state.SourceState `json:"state"`
}

// dashboard_response 是 Dashboard 首屏所需的全部数据
type dashboard_response struct {
	Server  server_info       `json:"server"`
	Sources []source_item     `json:"sources"`
	Actions []*registry.Entry `json:"actions"`
}

// handle_dashboard 返回所有信息源状态与动作定义
func (s *Server) handle_dashboard(w http.ResponseWriter, r *http.Request) {
	snapshot := s.states.Snapshot()
	entries := s.store.List()

	sources := make([]source_item, 0, len(entries))
	actions := make([]*registry.Entry, 0, len(entries))
	for _, entry := range entries {
		if entry.Kind == registry.KindAction {
			actions = append(actions, entry)
			continue
		}
		st := snapshot[entry.ID]
		if st == nil {
			st = &state.SourceState{ID: entry.ID, Status: state.StatusUnknown}
		}
		sources = append(sources, source_item{Entry: entry, State: st})
	}

	write_json(w, http.StatusOK, dashboard_response{
		Server:  s.info(),
		Sources: sources,
		Actions: actions,
	})
}

// info 汇总服务元信息
func (s *Server) info() server_info {
	entries := s.store.List()
	sources, actions := 0, 0
	for _, entry := range entries {
		if entry.Kind == registry.KindSource {
			sources++
		} else {
			actions++
		}
	}
	// 鉴权关掉的开发模式下没有「口令」这回事，因此不再报「需要先改口令」：
	// 否则每次开 dev 都会被一个既改不掉也无所谓的弹窗挡住。
	return server_info{
		Version:     s.version,
		UptimeS:     time.Since(s.started).Seconds(),
		StartedAt:   s.started,
		Dev:         s.dev,
		AuthOff:     s.cfg.Auth.Disabled,
		MustChange:  s.secrets.MustChange() && !s.cfg.Auth.Disabled,
		SessionTTLM: int(s.cfg.Auth.SessionTTL().Minutes()),
		TLS:         s.cfg.TLS.Enabled,
		Sources:     sources,
		Actions:     actions,
		Subscribers: s.hub.Subscribers(),
	}
}
