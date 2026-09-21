package engine

import (
	"wrtdeck/internal/registry"
	"wrtdeck/internal/template"
)

// render_transport 渲染传输配置里所有允许写模板的字段，返回一份新配置
func render_transport(spec registry.TransportSpec, scope template.Scope) (registry.TransportSpec, error) {
	out := spec
	out.HTTP = nil
	out.TCP = nil
	out.UDP = nil
	out.MQTT = nil
	out.Exec = nil

	var err error
	switch spec.Type {
	case registry.TransportHTTP:
		if spec.HTTP != nil {
			out.HTTP, err = render_http(spec.HTTP, scope)
		}
	case registry.TransportTCP:
		if spec.TCP != nil {
			out.TCP, err = render_tcp(spec.TCP, scope)
		}
	case registry.TransportUDP:
		if spec.UDP != nil {
			out.UDP, err = render_udp(spec.UDP, scope)
		}
	case registry.TransportMQTT:
		if spec.MQTT != nil {
			out.MQTT, err = render_mqtt(spec.MQTT, scope)
		}
	case registry.TransportExec:
		if spec.Exec != nil {
			out.Exec, err = render_exec(spec.Exec, scope)
		}
	}
	if err != nil {
		return out, err
	}
	return out, nil
}

// render_http 渲染 HTTP 传输字段
func render_http(cfg *registry.HTTPSpec, scope template.Scope) (*registry.HTTPSpec, error) {
	out := *cfg
	var err error
	if out.URL, err = template.Render(cfg.URL, scope); err != nil {
		return nil, err
	}
	if out.Body, err = template.Render(cfg.Body, scope); err != nil {
		return nil, err
	}
	if len(cfg.Headers) > 0 {
		out.Headers = make(map[string]string, len(cfg.Headers))
		for k, v := range cfg.Headers {
			key, key_err := template.Render(k, scope)
			if key_err != nil {
				return nil, key_err
			}
			if out.Headers[key], err = template.Render(v, scope); err != nil {
				return nil, err
			}
		}
	}
	return &out, nil
}

// render_tcp 渲染 TCP 传输字段
func render_tcp(cfg *registry.TCPSpec, scope template.Scope) (*registry.TCPSpec, error) {
	out := *cfg
	var err error
	if out.Address, err = template.Render(cfg.Address, scope); err != nil {
		return nil, err
	}
	if out.Payload, err = template.Render(cfg.Payload, scope); err != nil {
		return nil, err
	}
	return &out, nil
}

// render_udp 渲染 UDP 传输字段
func render_udp(cfg *registry.UDPSpec, scope template.Scope) (*registry.UDPSpec, error) {
	out := *cfg
	var err error
	if out.Address, err = template.Render(cfg.Address, scope); err != nil {
		return nil, err
	}
	if out.Payload, err = template.Render(cfg.Payload, scope); err != nil {
		return nil, err
	}
	return &out, nil
}

// render_mqtt 渲染 MQTT 传输字段
func render_mqtt(cfg *registry.MQTTSpec, scope template.Scope) (*registry.MQTTSpec, error) {
	out := *cfg
	var err error
	if out.Broker, err = template.Render(cfg.Broker, scope); err != nil {
		return nil, err
	}
	if out.Topic, err = template.Render(cfg.Topic, scope); err != nil {
		return nil, err
	}
	if out.Payload, err = template.Render(cfg.Payload, scope); err != nil {
		return nil, err
	}
	if out.Username, err = template.Render(cfg.Username, scope); err != nil {
		return nil, err
	}
	if out.Password, err = template.Render(cfg.Password, scope); err != nil {
		return nil, err
	}
	return &out, nil
}

// render_exec 渲染 Exec 传输字段
func render_exec(cfg *registry.ExecSpec, scope template.Scope) (*registry.ExecSpec, error) {
	out := *cfg
	var err error
	if out.Executable, err = template.Render(cfg.Executable, scope); err != nil {
		return nil, err
	}
	out.Args = make([]string, 0, len(cfg.Args))
	for _, arg := range cfg.Args {
		rendered, arg_err := template.Render(arg, scope)
		if arg_err != nil {
			return nil, arg_err
		}
		out.Args = append(out.Args, rendered)
	}
	return &out, nil
}
