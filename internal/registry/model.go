package registry

import "strings"

// 注册项类型
const (
	KindSource = "source"
	KindAction = "action"
)

// 参数类型
const (
	ParamString  = "string"
	ParamNumber  = "number"
	ParamBoolean = "boolean"
	ParamSelect  = "select"
	ParamSecret  = "secret"
)

// 传输类型
const (
	TransportHTTP = "http"
	TransportTCP  = "tcp"
	TransportUDP  = "udp"
	TransportMQTT = "mqtt"
	TransportExec = "exec"
)

// 提取类型
const (
	ExtractRaw   = "raw"
	ExtractJSON  = "json"
	ExtractRegex = "regex"
)

// ParamSpec 描述一个运行时参数的约束
type ParamSpec struct {
	Type     string   `json:"type"`
	Label    string   `json:"label,omitempty"`
	Required bool     `json:"required,omitempty"`
	Default  any      `json:"default,omitempty"`
	Min      *float64 `json:"min,omitempty"`
	Max      *float64 `json:"max,omitempty"`
	Options  []string `json:"options,omitempty"`
}

// HTTPSpec 描述一次 HTTP 调用
type HTTPSpec struct {
	Method    string            `json:"method,omitempty"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers,omitempty"`
	Body      string            `json:"body,omitempty"`
	TimeoutMS int               `json:"timeout_ms,omitempty"`
}

// TCPSpec 描述一次 TCP 往返
type TCPSpec struct {
	Address     string `json:"address"`
	Payload     string `json:"payload,omitempty"`
	Encoding    string `json:"encoding,omitempty"`
	TimeoutMS   int    `json:"timeout_ms,omitempty"`
	ExpectReply bool   `json:"expect_reply,omitempty"`
}

// UDPSpec 描述一次 UDP 报文发送
type UDPSpec struct {
	Address     string `json:"address"`
	Payload     string `json:"payload,omitempty"`
	Encoding    string `json:"encoding,omitempty"`
	TimeoutMS   int    `json:"timeout_ms,omitempty"`
	ExpectReply bool   `json:"expect_reply,omitempty"`
}

// MQTT 工作模式：动作通常发布，信息源通常订阅
const (
	MQTTModePublish   = "publish"
	MQTTModeSubscribe = "subscribe"
)

// MQTTSpec 描述 MQTT 发布或订阅
type MQTTSpec struct {
	Broker    string `json:"broker"`
	Mode      string `json:"mode,omitempty"`
	ClientID  string `json:"client_id,omitempty"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	Topic     string `json:"topic"`
	QoS       int    `json:"qos,omitempty"`
	Retain    bool   `json:"retain,omitempty"`
	Payload   string `json:"payload,omitempty"`
	Encoding  string `json:"encoding,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

// EffectiveMode 返回 MQTT 的实际工作模式，未声明时按发布处理
func (m *MQTTSpec) EffectiveMode() string {
	if m == nil || strings.TrimSpace(m.Mode) == "" {
		return MQTTModePublish
	}
	return m.Mode
}

// IsSubscribe 判断这条 MQTT 配置是否走订阅推送
func (m *MQTTSpec) IsSubscribe() bool {
	return m.EffectiveMode() == MQTTModeSubscribe
}

// Subscribes 说明该注册项是否依赖 MQTT 订阅，调度器据此跳过轮询
func (e *Entry) Subscribes() bool {
	return e.Transport.Type == TransportMQTT && e.Transport.MQTT.IsSubscribe()
}

// ExecSpec 描述一次受限的本地程序调用
type ExecSpec struct {
	Executable string   `json:"executable"`
	Args       []string `json:"args,omitempty"`
	TimeoutMS  int      `json:"timeout_ms,omitempty"`
}

// TransportSpec 汇总所有传输方式的配置，只有 Type 对应字段会被使用
type TransportSpec struct {
	Type string    `json:"type"`
	HTTP *HTTPSpec `json:"http,omitempty"`
	TCP  *TCPSpec  `json:"tcp,omitempty"`
	UDP  *UDPSpec  `json:"udp,omitempty"`
	MQTT *MQTTSpec `json:"mqtt,omitempty"`
	Exec *ExecSpec `json:"exec,omitempty"`
}

// ScheduleSpec 描述信息源的轮询间隔
type ScheduleSpec struct {
	IntervalMS int `json:"interval_ms"`
}

// ExtractSpec 描述如何从响应体中取出值
type ExtractSpec struct {
	Type      string `json:"type"`
	Path      string `json:"path,omitempty"`
	Pattern   string `json:"pattern,omitempty"`
	Group     int    `json:"group,omitempty"`
	ValueType string `json:"value_type,omitempty"`
}

// ConfirmSpec 描述动作执行前的二次确认弹窗
type ConfirmSpec struct {
	Enabled bool   `json:"enabled,omitempty"`
	Title   string `json:"title,omitempty"`
	Message string `json:"message,omitempty"`
}

// UISpec 描述前端如何渲染这个注册项
type UISpec struct {
	Type      string       `json:"type"`
	Unit      string       `json:"unit,omitempty"`
	Label     string       `json:"label,omitempty"`
	Precision int          `json:"precision,omitempty"`
	Variant   string       `json:"variant,omitempty"`
	Order     int          `json:"order,omitempty"`
	Confirm   *ConfirmSpec `json:"confirm,omitempty"`
}

// Entry 是注册表中的一条信息源或动作
type Entry struct {
	ID        string               `json:"id"`
	Kind      string               `json:"kind"`
	Name      string               `json:"name"`
	Group     string               `json:"group,omitempty"`
	Enabled   bool                 `json:"enabled"`
	Params    map[string]ParamSpec `json:"params,omitempty"`
	Transport TransportSpec        `json:"transport"`
	Schedule  *ScheduleSpec        `json:"schedule,omitempty"`
	Extract   *ExtractSpec         `json:"extract,omitempty"`
	UI        UISpec               `json:"ui"`
}

// IntervalMS 返回信息源的有效轮询间隔，未配置时回落到默认值
func (e *Entry) IntervalMS(fallback int) int {
	if e.Schedule == nil || e.Schedule.IntervalMS <= 0 {
		return fallback
	}
	return e.Schedule.IntervalMS
}
