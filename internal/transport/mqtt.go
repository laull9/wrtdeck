package transport

import (
	"context"
	"errors"

	"owdash/internal/registry"
)

// mqtt_transport 是 MQTT 的占位实现。
// 骨架阶段先不引入 eclipse-paho，等 Registry 与 SSE 链路稳定后再接线，
// 这样能保持二进制体积和依赖数量最小。
type mqtt_transport struct{}

// init 注册 MQTT 占位实现
func init() {
	Register(&mqtt_transport{})
}

// Type 返回传输名称
func (m *mqtt_transport) Type() string { return registry.TransportMQTT }

// Available 在接线 paho 之前恒为 false，注册表会给出明确提示
func (m *mqtt_transport) Available() bool { return false }

// Do 在未接线时直接返回错误，避免静默失败
func (m *mqtt_transport) Do(ctx context.Context, spec registry.TransportSpec, opts Options) (*Result, error) {
	return nil, errors.New("MQTT 传输尚未接线，需要引入 eclipse-paho 后启用")
}
