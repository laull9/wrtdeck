// Package template 实现最小模板系统：只做变量替换和少量安全过滤器。
// 刻意不支持循环、条件、函数调用，避免注册配置演变成脚本执行系统。
package template

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// 支持的过滤器名称
const (
	FilterRaw  = "raw"
	FilterURL  = "url"
	FilterJSON = "json"
)

// variable_pattern 匹配 ${params.name} 或 ${secret.token|url} 形式
var variable_pattern = regexp.MustCompile(`\$\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\.([a-zA-Z_][a-zA-Z0-9_]*)\s*((?:\|[a-z]+)*)\s*\}`)

// Scope 是渲染时可见的变量集合
type Scope struct {
	Params  map[string]any
	Secrets map[string]string
}

// Ref 表示模板中对某个变量的引用
type Ref struct {
	Namespace string
	Name      string
}

// Render 把模板中的变量替换为实际值，未提供的变量替换为空串
func Render(text string, scope Scope) (string, error) {
	if text == "" || !strings.Contains(text, "${") {
		return text, nil
	}
	var first_err error
	out := variable_pattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := variable_pattern.FindStringSubmatch(match)
		namespace, name, filters := parts[1], parts[2], parts[3]

		value, ok := lookup(namespace, name, scope)
		if !ok {
			if first_err == nil {
				first_err = fmt.Errorf("未提供的变量 ${%s.%s}", namespace, name)
			}
			return ""
		}
		rendered, err := apply_filters(value_text(value), filters)
		if err != nil {
			if first_err == nil {
				first_err = err
			}
			return ""
		}
		return rendered
	})
	return out, first_err
}

// Refs 列出模板引用的全部变量，用于注册期检查引用是否存在
func Refs(text string) []Ref {
	matches := variable_pattern.FindAllStringSubmatch(text, -1)
	seen := make(map[string]bool, len(matches))
	out := make([]Ref, 0, len(matches))
	for _, m := range matches {
		key := m[1] + "." + m[2]
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Ref{Namespace: m[1], Name: m[2]})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// CheckRefs 检查模板引用的变量都已声明，names 是合法名字集合
func CheckRefs(text string, names map[string]bool) error {
	for _, ref := range Refs(text) {
		if names[ref.Namespace+"."+ref.Name] {
			continue
		}
		return fmt.Errorf("模板引用了未声明的变量 ${%s.%s}", ref.Namespace, ref.Name)
	}
	return nil
}

// lookup 在作用域中查找变量的原始值
func lookup(namespace, name string, scope Scope) (any, bool) {
	switch namespace {
	case "params":
		v, ok := scope.Params[name]
		return v, ok
	case "secret":
		v, ok := scope.Secrets[name]
		return v, ok
	default:
		return nil, false
	}
}

// value_text 把参数值转成用于替换的文本
func value_text(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 32)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(data)
	}
}

// apply_filters 依次应用模板过滤器
func apply_filters(value, filters string) (string, error) {
	if filters == "" {
		return value, nil
	}
	for _, raw := range strings.Split(strings.TrimPrefix(filters, "|"), "|") {
		name := strings.TrimSpace(raw)
		switch name {
		case "", FilterRaw:
		case FilterURL:
			value = url.QueryEscape(value)
		case FilterJSON:
			encoded, err := json.Marshal(value)
			if err != nil {
				return "", fmt.Errorf("json 过滤器失败: %w", err)
			}
			value = string(encoded[1 : len(encoded)-1])
		default:
			return "", fmt.Errorf("不支持的过滤器 %q", name)
		}
	}
	return value, nil
}
