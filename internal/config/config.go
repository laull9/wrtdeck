package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 内置默认值，与架构文档中的资源预算保持一致
const (
	default_listen            = "0.0.0.0:8080"
	default_source_workers    = 4
	default_action_workers    = 4
	default_min_interval_ms   = 1000
	default_max_body_bytes    = 256 * 1024
	default_max_request_bytes = 64 * 1024
	default_timeout_ms        = 5000
	default_history_limit     = 0
	default_mqtt_keepalive_s  = 30
	default_mqtt_connect_ms   = 5000
	default_mqtt_reconnect_ms = 30000

	// 认证与会话
	default_session_ttl_minutes = 720
	default_max_login_attempts  = 5
	default_lockout_minutes     = 15

	// 同源网关等待面板本体的默认上限
	default_gateway_timeout_ms = 15000
)

// AuthConfig 控制 API 鉴权行为
type AuthConfig struct {
	Disabled bool   `json:"disabled,omitempty"`
	TokenEnv string `json:"token_env,omitempty"`

	// SessionTTLMinutes 是登录会话的有效期，默认 12 小时
	SessionTTLMinutes int `json:"session_ttl_minutes,omitempty"`
	// MaxLoginAttempts 是单个来源在窗口内允许的失败次数，超出即锁定
	MaxLoginAttempts int `json:"max_login_attempts,omitempty"`
	// LockoutMinutes 是首次锁定的时长，连续失败会成倍延长
	LockoutMinutes int `json:"lockout_minutes,omitempty"`
}

// TLSConfig 控制面板自身的 HTTPS。
//
// 面板可能被直接暴露在公网，而口令是明文提交的，因此这里提供两种做法：
// 自带证书文件（推荐，配合域名与 Let's Encrypt），或自动生成自签证书
// （只适合局域网，浏览器会提示不受信任）。
type TLSConfig struct {
	Enabled  bool   `json:"enabled,omitempty"`
	CertFile string `json:"cert_file,omitempty"`
	KeyFile  string `json:"key_file,omitempty"`
	// AutoSelfSigned 为 nil 表示未配置，按「启用时自动生成自签证书」处理
	AutoSelfSigned *bool `json:"auto_self_signed,omitempty"`
	// RedirectListen 非空时额外监听该地址，把明文请求 308 跳到 HTTPS
	RedirectListen string `json:"redirect_listen,omitempty"`
}

// SelfSignedEnabled 返回未提供证书文件时是否自动生成自签证书
func (t TLSConfig) SelfSignedEnabled() bool {
	return t.AutoSelfSigned == nil || *t.AutoSelfSigned
}

// SessionTTL 返回会话有效期
func (a AuthConfig) SessionTTL() time.Duration {
	minutes := a.SessionTTLMinutes
	if minutes <= 0 {
		minutes = default_session_ttl_minutes
	}
	return time.Duration(minutes) * time.Minute
}

// MaxAttempts 返回单来源允许的登录失败次数
func (a AuthConfig) MaxAttempts() int {
	if a.MaxLoginAttempts <= 0 {
		return default_max_login_attempts
	}
	return a.MaxLoginAttempts
}

// Lockout 返回首次锁定时长
func (a AuthConfig) Lockout() time.Duration {
	minutes := a.LockoutMinutes
	if minutes <= 0 {
		minutes = default_lockout_minutes
	}
	return time.Duration(minutes) * time.Minute
}

// GatewayConfig 控制「面板被 LuCI 同源内嵌」时用的反向代理网关。
//
// 设备上的 Web 服务器（uhttpd）只能服务静态文件，不能反向代理，
// 所以面板 API 要由一个 CGI 网关转交给只监听回环的面板本体。
// 网关同时负责认出已经登录 LuCI 的浏览器，并替它注入面板凭据，
// 于是用户从 LuCI 点进来不必再输一次面板口令。
type GatewayConfig struct {
	// Upstream 是面板本体地址；留空时按 listen 推导出回环地址
	Upstream string `json:"upstream,omitempty"`
	// VerifyLuciSession 为 nil 表示启用：校验浏览器带来的 LuCI 会话
	VerifyLuciSession *bool `json:"verify_luci_session,omitempty"`
	// InjectToken 为 nil 表示启用：LuCI 会话有效时代替浏览器注入 API Token
	InjectToken *bool `json:"inject_token,omitempty"`
	// TimeoutMS 是网关等待面板本体的最长时间
	TimeoutMS int `json:"timeout_ms,omitempty"`
}

// VerifyLuci 返回是否校验 LuCI 会话，默认开启
func (g GatewayConfig) VerifyLuci() bool {
	return g.VerifyLuciSession == nil || *g.VerifyLuciSession
}

// Inject 返回是否注入 API Token，默认开启
func (g GatewayConfig) Inject() bool {
	return g.InjectToken == nil || *g.InjectToken
}

// Timeout 返回网关等待面板本体的最长时间
func (g GatewayConfig) Timeout() time.Duration {
	ms := g.TimeoutMS
	if ms <= 0 {
		ms = default_gateway_timeout_ms
	}
	return time.Duration(ms) * time.Millisecond
}

// ExecConfig 控制 Exec 传输的开关与白名单
type ExecConfig struct {
	Enabled   bool     `json:"enabled"`
	Allowlist []string `json:"allowlist,omitempty"`
}

// LimitsConfig 是各类资源上限
type LimitsConfig struct {
	MaxBodyBytes     int `json:"max_body_bytes,omitempty"`
	MaxRequestBytes  int `json:"max_request_bytes,omitempty"`
	DefaultTimeoutMS int `json:"default_timeout_ms,omitempty"`
	MinIntervalMS    int `json:"min_interval_ms,omitempty"`
	HistoryLimit     int `json:"history_limit,omitempty"`
}

// WorkerConfig 是两类工作池的并发度
type WorkerConfig struct {
	Source int `json:"source,omitempty"`
	Action int `json:"action,omitempty"`
}

// MQTTConfig 控制 MQTT 连接池的行为，同一个 broker 只维护一条长连接
type MQTTConfig struct {
	KeepaliveSeconds    int `json:"keepalive_seconds,omitempty"`
	ConnectTimeoutMS    int `json:"connect_timeout_ms,omitempty"`
	MaxReconnectDelayMS int `json:"max_reconnect_delay_ms,omitempty"`
}

// Config 是 daemon 的完整配置
type Config struct {
	Listen   string        `json:"listen"`
	DataDir  string        `json:"data_dir"`
	SeedDemo bool          `json:"seed_demo"`
	Auth     AuthConfig    `json:"auth"`
	TLS      TLSConfig     `json:"tls"`
	Gateway  GatewayConfig `json:"gateway"`
	Exec     ExecConfig    `json:"exec"`
	Limits   LimitsConfig  `json:"limits"`
	Workers  WorkerConfig  `json:"workers"`
	MQTT     MQTTConfig    `json:"mqtt"`

	path string
}

// UpstreamURL 推导出面板本体的回环访问地址，供同源网关转发使用。
// 监听地址写成通配地址时换成回环，协议由 TLS 开关决定。
func (c *Config) UpstreamURL() string {
	if base := strings.TrimSpace(c.Gateway.Upstream); base != "" {
		return strings.TrimRight(base, "/")
	}
	scheme := "http"
	if c.TLS.Enabled {
		scheme = "https"
	}
	host, port, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return scheme + "://127.0.0.1:8080"
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return scheme + "://" + net.JoinHostPort(host, port)
}

// Default 返回一份填好默认值的配置
func Default() *Config {
	return &Config{
		Listen:  default_listen,
		Workers: WorkerConfig{Source: default_source_workers, Action: default_action_workers},
		Limits: LimitsConfig{
			MaxBodyBytes:     default_max_body_bytes,
			MaxRequestBytes:  default_max_request_bytes,
			DefaultTimeoutMS: default_timeout_ms,
			MinIntervalMS:    default_min_interval_ms,
			HistoryLimit:     default_history_limit,
		},
		MQTT: MQTTConfig{
			KeepaliveSeconds:    default_mqtt_keepalive_s,
			ConnectTimeoutMS:    default_mqtt_connect_ms,
			MaxReconnectDelayMS: default_mqtt_reconnect_ms,
		},
	}
}

// Parse 解析一份配置内容，全程不触碰文件系统。
// 网关这类「每次请求起一个进程」的场景用它装载配置，
// 免得一次浏览器访问顺带写出一个配置文件。
func Parse(data []byte) (*Config, error) {
	cfg := Default()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	cfg.normalize()
	return cfg, nil
}

// Load 读取配置文件，文件不存在时返回默认配置并写出一份样例
func Load(path string) (*Config, error) {
	cfg := Default()
	cfg.path = path
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if err = cfg.Save(); err != nil {
				return nil, fmt.Errorf("写入默认配置失败: %w", err)
			}
			return cfg, nil
		}
		return nil, err
	}
	loaded, err := Parse(data)
	if err != nil {
		return nil, err
	}
	loaded.path = path
	return loaded, nil
}

// Save 原子写回配置文件
func (c *Config) Save() error {
	if c.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err = os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err = os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// normalize 把非法或缺失的数值补成默认值
func (c *Config) normalize() {
	if c.Listen == "" {
		c.Listen = default_listen
	}
	if c.Workers.Source <= 0 {
		c.Workers.Source = default_source_workers
	}
	if c.Workers.Action <= 0 {
		c.Workers.Action = default_action_workers
	}
	if c.Limits.MaxBodyBytes <= 0 {
		c.Limits.MaxBodyBytes = default_max_body_bytes
	}
	if c.Limits.MaxRequestBytes <= 0 {
		c.Limits.MaxRequestBytes = default_max_request_bytes
	}
	if c.Limits.DefaultTimeoutMS <= 0 {
		c.Limits.DefaultTimeoutMS = default_timeout_ms
	}
	if c.Limits.MinIntervalMS <= 0 {
		c.Limits.MinIntervalMS = default_min_interval_ms
	}
	if c.MQTT.KeepaliveSeconds <= 0 {
		c.MQTT.KeepaliveSeconds = default_mqtt_keepalive_s
	}
	if c.MQTT.ConnectTimeoutMS <= 0 {
		c.MQTT.ConnectTimeoutMS = default_mqtt_connect_ms
	}
	if c.MQTT.MaxReconnectDelayMS <= 0 {
		c.MQTT.MaxReconnectDelayMS = default_mqtt_reconnect_ms
	}
}

// RegistryPath 返回注册表文件路径
func (c *Config) RegistryPath() string { return filepath.Join(c.DataDir, "registry.json") }

// SecretsPath 返回密钥文件路径
func (c *Config) SecretsPath() string { return filepath.Join(c.DataDir, "secrets.json") }
