// Package extract 从响应体中取出目标值，支持 raw、json 路径和正则三种方式。
package extract

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"wrtdeck/internal/registry"
)

// Result 是一次提取的产物
type Result struct {
	Value any
	Text  string
}

// Extract 按配置从响应体中提取并转换类型
func Extract(spec *registry.ExtractSpec, body []byte) (*Result, error) {
	if spec == nil {
		spec = &registry.ExtractSpec{Type: registry.ExtractRaw}
	}
	text := strings.TrimSpace(string(body))

	var raw string
	switch spec.Type {
	case registry.ExtractJSON:
		v, err := pick_json(body, spec.Path)
		if err != nil {
			return nil, err
		}
		raw = scalar_text(v)
	case registry.ExtractRegex:
		v, err := pick_regex(text, spec.Pattern, spec.Group)
		if err != nil {
			return nil, err
		}
		raw = v
	default:
		raw = text
	}
	return convert(raw, spec.ValueType)
}

// pick_json 用简单的点分路径从 JSON 中取值，数组下标写成数字段
func pick_json(body []byte, path string) (any, error) {
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("响应体不是合法 JSON: %w", err)
	}
	segments := strings.Split(strings.TrimSpace(path), ".")
	cur := doc
	for _, seg := range segments {
		if seg == "" {
			continue
		}
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[seg]
			if !ok {
				return nil, fmt.Errorf("JSON 路径 %s 不存在字段 %s", path, seg)
			}
			cur = v
		case []any:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil, fmt.Errorf("JSON 路径 %s 的下标 %s 越界", path, seg)
			}
			cur = node[idx]
		default:
			return nil, fmt.Errorf("JSON 路径 %s 在字段 %s 处无法继续下钻", path, seg)
		}
	}
	return cur, nil
}

// pick_regex 用正则捕获组从文本中取值
func pick_regex(text, pattern string, group int) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("正则不合法: %w", err)
	}
	m := re.FindStringSubmatch(text)
	if m == nil {
		return "", fmt.Errorf("正则 %s 未匹配到内容", pattern)
	}
	if group <= 0 {
		return m[0], nil
	}
	if group >= len(m) {
		return "", fmt.Errorf("正则捕获组 %d 不存在", group)
	}
	return m[group], nil
}

// scalar_text 把 JSON 标量转成字符串，非标量序列化后再返回
func scalar_text(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		data, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(data)
	}
}

// convert 按 value_type 把文本转换成目标类型
func convert(text, value_type string) (*Result, error) {
	res := &Result{Text: text, Value: text}
	switch value_type {
	case registry.ParamNumber:
		n, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, fmt.Errorf("%q 无法转换为数字", text)
		}
		res.Value = n
	case registry.ParamBoolean:
		b, err := strconv.ParseBool(strings.ToLower(text))
		if err != nil {
			return nil, fmt.Errorf("%q 无法转换为布尔值", text)
		}
		res.Value = b
	case registry.ParamString, "":
		res.Value = text
	}
	return res, nil
}
