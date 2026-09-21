package transport

import (
	"context"
	"errors"
	"fmt"

	"owdash/internal/registry"
)

// ErrMQTTSubscribePush 表示订阅型信息源由 broker 主动推送，不能按轮询方式采集
var ErrMQTTSubscribePush = errors.New("MQTT 订阅型信息源由 broker 推送，无需手动采集")

// mqtt_transport 通过共享连接池完成 MQTT 发布，连接复用见 mqttpool.go
type mqtt_transport struct{}

// init 注册 MQTT 传输实现
func init() {
	Register(&mqtt_transport{})
}

// Type 返回传输名称
func (m *mqtt_transport) Type() string { return registry.TransportMQTT }

// Available 表示 MQTT 已经接线
func (m *mqtt_transport) Available() bool { return true }

// Do 发布一条 MQTT 消息；订阅型信息源的取值由连接池的回调驱动
func (m *mqtt_transport) Do(ctx context.Context, spec registry.TransportSpec, opts Options) (*Result, error) {
	if spec.MQTT == nil {
		return nil, fmt.Errorf("缺少 transport.mqtt 配置")
	}
	if spec.MQTT.IsSubscribe() {
		return nil, ErrMQTTSubscribePush
	}
	if opts.MQTT == nil {
		return nil, errors.New("MQTT 连接池未初始化")
	}
	payload, err := encode_payload(spec.MQTT.Payload, spec.MQTT.Encoding)
	if err != nil {
		return nil, err
	}
	if opts.MaxBodyBytes > 0 && len(payload) > opts.MaxBodyBytes {
		return nil, fmt.Errorf("MQTT 载荷 %d 字节超过上限 %d 字节", len(payload), opts.MaxBodyBytes)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout_of(spec.MQTT.TimeoutMS, opts))
	defer cancel()

	if err = opts.MQTT.Publish(ctx, spec.MQTT, payload); err != nil {
		return nil, err
	}
	return &Result{
		StatusCode: 200,
		Detail:     fmt.Sprintf("已发布到 %s（QoS %d）", spec.MQTT.Topic, spec.MQTT.QoS),
	}, nil
}
