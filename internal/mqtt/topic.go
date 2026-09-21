package mqtt

import "strings"

// TopicMatches 判断主题过滤器是否命中实际主题，规则与 MQTT 3.1.1 一致：
// + 匹配单层，# 匹配多层且必须位于末级，首层通配符不匹配 $ 开头的系统主题。
func TopicMatches(filter, topic string) bool {
	filter = strings.TrimSpace(filter)
	if filter == "" || topic == "" {
		return false
	}
	f := strings.Split(filter, "/")
	t := strings.Split(topic, "/")

	if (f[0] == "#" || f[0] == "+") && strings.HasPrefix(t[0], "$") {
		return false
	}

	for i, seg := range f {
		if seg == "#" {
			return i == len(f)-1
		}
		if i >= len(t) {
			return false
		}
		if seg == "+" {
			continue
		}
		if seg != t[i] {
			return false
		}
	}
	return len(f) == len(t)
}

// ValidTopicFilter 校验主题过滤器是否合法，用于在建立订阅前提前拦下明显错误
func ValidTopicFilter(filter string) bool {
	filter = strings.TrimSpace(filter)
	if filter == "" || len(filter) > 65535 {
		return false
	}
	levels := strings.Split(filter, "/")
	for i, level := range levels {
		if strings.ContainsAny(level, "\x00") {
			return false
		}
		if strings.Contains(level, "#") && (level != "#" || i != len(levels)-1) {
			return false
		}
		if strings.Contains(level, "+") && level != "+" {
			return false
		}
	}
	return true
}
