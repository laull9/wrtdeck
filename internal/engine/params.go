package engine

import (
	"fmt"
	"strconv"
	"strings"

	"owdash/internal/registry"
)

// ValidateParams 校验运行时参数，并补齐默认值，返回归一化后的参数表
func ValidateParams(entry *registry.Entry, params map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(entry.Params))
	for name, spec := range entry.Params {
		raw, provided := params[name]
		if !provided || raw == nil || raw == "" {
			if spec.Default != nil {
				out[name] = spec.Default
				continue
			}
			if spec.Required {
				return nil, fmt.Errorf("缺少必填参数 %s", name)
			}
			continue
		}
		value, err := coerce(name, spec, raw)
		if err != nil {
			return nil, err
		}
		out[name] = value
	}
	return out, nil
}

// coerce 按参数声明把输入转换并校验到目标类型
func coerce(name string, spec registry.ParamSpec, raw any) (any, error) {
	switch spec.Type {
	case registry.ParamNumber:
		n, err := to_number(raw)
		if err != nil {
			return nil, fmt.Errorf("参数 %s 需要数字", name)
		}
		if spec.Min != nil && n < *spec.Min {
			return nil, fmt.Errorf("参数 %s 小于最小值 %v", name, *spec.Min)
		}
		if spec.Max != nil && n > *spec.Max {
			return nil, fmt.Errorf("参数 %s 大于最大值 %v", name, *spec.Max)
		}
		return n, nil
	case registry.ParamBoolean:
		b, err := to_bool(raw)
		if err != nil {
			return nil, fmt.Errorf("参数 %s 需要布尔值", name)
		}
		return b, nil
	case registry.ParamSelect:
		text := fmt.Sprint(raw)
		for _, opt := range spec.Options {
			if opt == text {
				return text, nil
			}
		}
		return nil, fmt.Errorf("参数 %s 的取值 %q 不在候选项中", name, text)
	default:
		return fmt.Sprint(raw), nil
	}
}

// to_number 把输入解析为浮点数
func to_number(raw any) (float64, error) {
	switch t := raw.(type) {
	case float64:
		return t, nil
	case float32:
		return float64(t), nil
	case int:
		return float64(t), nil
	case int64:
		return float64(t), nil
	default:
		return strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(raw)), 64)
	}
}

// to_bool 把输入解析为布尔值
func to_bool(raw any) (bool, error) {
	if b, ok := raw.(bool); ok {
		return b, nil
	}
	return strconv.ParseBool(strings.ToLower(strings.TrimSpace(fmt.Sprint(raw))))
}
