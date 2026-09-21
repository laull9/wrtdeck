package registry

import (
	"fmt"
	"regexp"

	"wrtdeck/internal/template"
)

// secret_ref_pattern 匹配 ${secret.xxx} 形式的引用，供静态检查时剔除
var secret_ref_pattern = regexp.MustCompile(`\$\{\s*secret\.[a-zA-Z0-9_]+\s*(?:\|[a-z]+)*\}`)

// TemplateTexts 汇总注册项里所有允许写模板的字段
func TemplateTexts(e *Entry) []string {
	out := make([]string, 0, 8)
	t := e.Transport
	if t.HTTP != nil {
		out = append(out, t.HTTP.URL, t.HTTP.Body)
		for k, v := range t.HTTP.Headers {
			out = append(out, k, v)
		}
	}
	if t.TCP != nil {
		out = append(out, t.TCP.Address, t.TCP.Payload)
	}
	if t.UDP != nil {
		out = append(out, t.UDP.Address, t.UDP.Payload)
	}
	if t.MQTT != nil {
		out = append(out, t.MQTT.Broker, t.MQTT.Topic, t.MQTT.Payload,
			t.MQTT.Username, t.MQTT.Password)
	}
	if t.Exec != nil {
		out = append(out, t.Exec.Executable)
		out = append(out, t.Exec.Args...)
	}
	return out
}

// CheckTemplateRefs 在注册阶段检查模板引用是否都已声明，避免运行期才发现问题
func CheckTemplateRefs(e *Entry) error {
	names := make(map[string]bool, len(e.Params)+1)
	for name := range e.Params {
		names["params."+name] = true
	}
	// secret 命名空间不在这里静态校验，因为密钥由运行环境注入
	for _, text := range TemplateTexts(e) {
		wrapped := strip_secret_refs(text)
		if err := template.CheckRefs(wrapped, names); err != nil {
			return fmt.Errorf("注册项 %s: %w", e.ID, err)
		}
	}
	return nil
}

// strip_secret_refs 把 secret 引用替换成占位符，让静态检查只关心 params
func strip_secret_refs(text string) string {
	return secret_ref_pattern.ReplaceAllString(text, "")
}
