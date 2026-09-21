// Package transport 是统一的协议执行层，Source 和 Action 都通过它访问外部设备。
package transport

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"wrtdeck/internal/registry"
)

// 载荷编码方式
const (
	EncodingText   = "text"
	EncodingJSON   = "json"
	EncodingHex    = "hex"
	EncodingBase64 = "base64"
)

// Options 是传输层的全局约束，由 daemon 配置注入
type Options struct {
	MaxBodyBytes   int
	DefaultTimeout time.Duration
	ExecEnabled    bool
	ExecAllowlist  []string
	// MQTT 是进程内共享的 broker 连接池，未初始化时 MQTT 传输不可用
	MQTT *MQTTPool
}

// DefaultOptions 返回一份只填了安全默认值的传输约束，方便测试与独立调用
func DefaultOptions() Options {
	return Options{
		MaxBodyBytes:   256 * 1024,
		DefaultTimeout: 5 * time.Second,
	}
}

// Result 是一次传输调用的产物
type Result struct {
	StatusCode int    `json:"status_code"`
	Body       []byte `json:"-"`
	BodyText   string `json:"body_text,omitempty"`
	LatencyMS  int64  `json:"latency_ms"`
	Detail     string `json:"detail,omitempty"`
}

// Transport 是所有协议必须实现的接口
type Transport interface {
	Type() string
	Available() bool
	Do(ctx context.Context, spec registry.TransportSpec, opts Options) (*Result, error)
}

// registry_map 保存已注册的传输实现
var registry_map = make(map[string]Transport)

// Register 注册一个传输实现，由各协议文件在 init 中调用
func Register(t Transport) {
	registry_map[t.Type()] = t
}

// Get 按名称取出传输实现
func Get(name string) (Transport, error) {
	t, ok := registry_map[name]
	if !ok {
		return nil, fmt.Errorf("未知的传输类型 %q", name)
	}
	return t, nil
}

// Names 列出所有已注册的传输类型
func Names() []string {
	out := make([]string, 0, len(registry_map))
	for name := range registry_map {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Do 是统一的执行入口，负责查表并校验可用性
func Do(ctx context.Context, spec registry.TransportSpec, opts Options) (*Result, error) {
	t, err := Get(spec.Type)
	if err != nil {
		return nil, err
	}
	if !t.Available() {
		return nil, fmt.Errorf("传输 %s 在当前构建中未启用", spec.Type)
	}
	start := time.Now()
	res, err := t.Do(ctx, spec, opts)
	if res != nil {
		res.LatencyMS = time.Since(start).Milliseconds()
	}
	return res, err
}

// timeout_of 返回本次调用应使用的超时，配置值优先
func timeout_of(ms int, opts Options) time.Duration {
	if ms > 0 {
		return time.Duration(ms) * time.Millisecond
	}
	if opts.DefaultTimeout > 0 {
		return opts.DefaultTimeout
	}
	return 5 * time.Second
}

// encode_payload 按声明的编码方式把文本转成字节
func encode_payload(payload, encoding string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", EncodingText:
		return []byte(payload), nil
	case EncodingJSON:
		return []byte(payload), nil
	case EncodingHex:
		data, err := hex.DecodeString(strings.TrimSpace(payload))
		if err != nil {
			return nil, fmt.Errorf("十六进制载荷不合法: %w", err)
		}
		return data, nil
	case EncodingBase64:
		data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload))
		if err != nil {
			return nil, fmt.Errorf("base64 载荷不合法: %w", err)
		}
		return data, nil
	default:
		return nil, fmt.Errorf("不支持的载荷编码 %q", encoding)
	}
}

// truncate 按上限裁剪响应体，避免设备返回超大内容吃满内存
func truncate(body []byte, limit int) []byte {
	if limit <= 0 || len(body) <= limit {
		return body
	}
	return body[:limit]
}

// preview 生成用于展示的响应摘要
func preview(body []byte, limit int) string {
	text := strings.TrimSpace(string(body))
	if limit > 0 && len(text) > limit {
		return text[:limit] + "..."
	}
	return text
}

// ErrExecDisabled 表示 Exec 传输被配置关闭
var ErrExecDisabled = errors.New("exec 传输默认关闭，需要在配置中显式启用")
