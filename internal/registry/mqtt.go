package registry

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// mqtt_default_ports 给出各 scheme 的默认端口，便于把 broker 地址补全
var mqtt_default_ports = map[string]string{
	"mqtt":  "1883",
	"tcp":   "1883",
	"mqtts": "8883",
	"ssl":   "8883",
	"tls":   "8883",
	"ws":    "80",
	"wss":   "443",
}

// has_template 判断文本里是否包含模板引用，含引用时跳过静态格式校验
func has_template(text string) bool {
	return strings.Contains(text, "${")
}

// NormalizeBrokerURL 把 broker 补全成带 scheme 的标准 URL，未写 scheme 时按 mqtt 处理
func NormalizeBrokerURL(broker string) (string, error) {
	raw := strings.TrimSpace(broker)
	if raw == "" {
		return "", fmt.Errorf("transport.mqtt.broker 不能为空")
	}
	// 含模板引用时无法在注册阶段确定最终地址，原样透传给运行期解析
	if has_template(raw) {
		return raw, nil
	}
	if !strings.Contains(raw, "://") {
		raw = "mqtt://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("transport.mqtt.broker %q 不是合法地址: %w", broker, err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	port, ok := mqtt_default_ports[scheme]
	if !ok {
		return "", fmt.Errorf("transport.mqtt.broker 的 scheme %q 不受支持，可用 %s", scheme, supported_scheme_list())
	}
	if parsed.Hostname() == "" {
		return "", fmt.Errorf("transport.mqtt.broker %q 缺少主机名", broker)
	}
	if parsed.Port() == "" {
		parsed.Host = parsed.Hostname() + ":" + port
	}
	return parsed.String(), nil
}

// supported_scheme_list 返回排序后的可用 scheme 列表，用于错误提示
func supported_scheme_list() string {
	out := make([]string, 0, len(mqtt_default_ports))
	for name := range mqtt_default_ports {
		out = append(out, name)
	}
	sort.Strings(out)
	return strings.Join(out, "、")
}

// ValidateMQTT 校验 MQTT 配置，kind 用于限制订阅只能用于信息源
func ValidateMQTT(spec *MQTTSpec, kind string) error {
	if spec == nil {
		return fmt.Errorf("缺少 transport.mqtt 配置")
	}
	if _, err := NormalizeBrokerURL(spec.Broker); err != nil {
		return err
	}
	if strings.TrimSpace(spec.Topic) == "" {
		return fmt.Errorf("transport.mqtt.topic 不能为空")
	}
	switch spec.EffectiveMode() {
	case MQTTModePublish:
	case MQTTModeSubscribe:
		if kind == KindAction {
			return fmt.Errorf("transport.mqtt.mode=subscribe 只能用于信息源，动作请使用 publish")
		}
	default:
		return fmt.Errorf("transport.mqtt.mode %q 不受支持，只能是 publish 或 subscribe", spec.Mode)
	}
	if spec.QoS < 0 || spec.QoS > 2 {
		return fmt.Errorf("transport.mqtt.qos 只能是 0、1 或 2，当前为 %d", spec.QoS)
	}
	if spec.Retain && spec.IsSubscribe() {
		return fmt.Errorf("transport.mqtt.retain 只对发布有意义，订阅请去掉该字段")
	}
	if err := ValidateEncoding(spec.Encoding); err != nil {
		return fmt.Errorf("transport.mqtt.encoding %w", err)
	}
	return nil
}

// supported_encodings 是载荷允许的编码方式，与 transport 层的编码实现保持一致
var supported_encodings = map[string]bool{
	"": true, "text": true, "json": true, "hex": true, "base64": true,
}

// ValidateEncoding 校验载荷编码是否受支持，空值表示按文本处理
func ValidateEncoding(encoding string) error {
	if supported_encodings[strings.ToLower(strings.TrimSpace(encoding))] {
		return nil
	}
	return fmt.Errorf("%q 不受支持，只能是 text、json、hex 或 base64", encoding)
}
