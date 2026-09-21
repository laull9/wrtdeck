package registry

import (
	"fmt"
	"regexp"
	"strings"
)

// id 只允许小写字母、数字、短横线和下划线，保证可以作为 URL 路径片段
var id_pattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// supported_kinds 列举合法注册项类型
var supported_kinds = map[string]bool{
	KindSource: true,
	KindAction: true,
}

// supported_transports 列举骨架阶段已接线的传输类型
var supported_transports = map[string]bool{
	TransportHTTP: true,
	TransportTCP:  true,
	TransportUDP:  true,
	TransportMQTT: true,
	TransportExec: true,
}

// Validate 校验注册项的自洽性，注册时和构造种子数据时都会调用
func Validate(e *Entry) error {
	if e == nil {
		return fmt.Errorf("注册项为空")
	}
	if !id_pattern.MatchString(e.ID) {
		return fmt.Errorf("非法的 id %q：只允许小写字母、数字、- 和 _", e.ID)
	}
	if !supported_kinds[e.Kind] {
		return fmt.Errorf("非法的 kind %q：只允许 source 或 action", e.Kind)
	}
	if strings.TrimSpace(e.Name) == "" {
		return fmt.Errorf("name 不能为空")
	}
	if !supported_transports[e.Transport.Type] {
		return fmt.Errorf("非法的 transport.type %q", e.Transport.Type)
	}
	if e.UI.Type == "" {
		if e.Kind == KindAction {
			e.UI.Type = "button"
		} else {
			e.UI.Type = "metric"
		}
	}
	if err := validate_params(e); err != nil {
		return err
	}
	if err := validate_transport(e); err != nil {
		return err
	}
	if e.Kind == KindSource {
		if e.Schedule == nil || e.Schedule.IntervalMS <= 0 {
			e.Schedule = &ScheduleSpec{IntervalMS: 5000}
		}
		if e.Extract == nil {
			e.Extract = &ExtractSpec{Type: ExtractRaw}
		}
		if err := validate_extract(e.Extract); err != nil {
			return err
		}
	}
	return CheckTemplateRefs(e)
}

// validate_params 检查每个参数的声明是否合法
func validate_params(e *Entry) error {
	for name, p := range e.Params {
		switch p.Type {
		case ParamString, ParamSecret:
		case ParamNumber:
			if p.Min != nil && p.Max != nil && *p.Min > *p.Max {
				return fmt.Errorf("参数 %s 的 min 大于 max", name)
			}
		case ParamBoolean:
		case ParamSelect:
			if len(p.Options) == 0 {
				return fmt.Errorf("参数 %s 是 select，但未提供 options", name)
			}
		default:
			return fmt.Errorf("参数 %s 的类型 %q 不受支持", name, p.Type)
		}
	}
	return nil
}

// validate_transport 检查传输类型与对应配置是否匹配
func validate_transport(e *Entry) error {
	t := e.Transport
	switch t.Type {
	case TransportHTTP:
		if t.HTTP == nil || strings.TrimSpace(t.HTTP.URL) == "" {
			return fmt.Errorf("transport.http.url 不能为空")
		}
	case TransportTCP:
		if t.TCP == nil || strings.TrimSpace(t.TCP.Address) == "" {
			return fmt.Errorf("transport.tcp.address 不能为空")
		}
	case TransportUDP:
		if t.UDP == nil || strings.TrimSpace(t.UDP.Address) == "" {
			return fmt.Errorf("transport.udp.address 不能为空")
		}
	case TransportMQTT:
		if t.MQTT == nil || strings.TrimSpace(t.MQTT.Broker) == "" {
			return fmt.Errorf("transport.mqtt.broker 不能为空")
		}
		if strings.TrimSpace(t.MQTT.Topic) == "" {
			return fmt.Errorf("transport.mqtt.topic 不能为空")
		}
	case TransportExec:
		if t.Exec == nil || strings.TrimSpace(t.Exec.Executable) == "" {
			return fmt.Errorf("transport.exec.executable 不能为空")
		}
	}
	return nil
}

// validate_extract 检查提取配置是否合法
func validate_extract(x *ExtractSpec) error {
	switch x.Type {
	case ExtractRaw, "":
	case ExtractJSON:
		if strings.TrimSpace(x.Path) == "" {
			return fmt.Errorf("extract.type=json 时必须提供 path")
		}
	case ExtractRegex:
		if _, err := regexp.Compile(x.Pattern); err != nil {
			return fmt.Errorf("extract.pattern 不是合法正则: %w", err)
		}
	default:
		return fmt.Errorf("extract.type %q 不受支持", x.Type)
	}
	switch x.ValueType {
	case "", ParamString, ParamNumber, ParamBoolean:
		return nil
	default:
		return fmt.Errorf("extract.value_type %q 不受支持", x.ValueType)
	}
}
