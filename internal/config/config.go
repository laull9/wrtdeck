package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
)

// AuthConfig 控制 API 鉴权行为
type AuthConfig struct {
	Disabled bool   `json:"disabled,omitempty"`
	TokenEnv string `json:"token_env,omitempty"`
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
	Listen   string       `json:"listen"`
	DataDir  string       `json:"data_dir"`
	SeedDemo bool         `json:"seed_demo"`
	Auth     AuthConfig   `json:"auth"`
	Exec     ExecConfig   `json:"exec"`
	Limits   LimitsConfig `json:"limits"`
	Workers  WorkerConfig `json:"workers"`
	MQTT     MQTTConfig   `json:"mqtt"`

	path string
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
	if err = json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	cfg.path = path
	cfg.normalize()
	return cfg, nil
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
