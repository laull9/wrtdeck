package registry

import "fmt"

// 示例注册项使用的分组名
const demo_group = "系统自检"

// SeedDemo 生成一组开箱自检用的注册项，全部指向本服务自身，无需外部设备
// base_url 形如 http://127.0.0.1:8080，interval_ms 是默认轮询周期
func SeedDemo(base_url string, interval_ms int) []*Entry {
	if interval_ms <= 0 {
		interval_ms = 5000
	}
	auth_header := map[string]string{"Authorization": "Bearer ${secret.api_token}"}
	health_url := fmt.Sprintf("%s/api/v1/health", base_url)

	entries := []*Entry{
		{
			ID:      "demo-health",
			Kind:    KindSource,
			Name:    "服务健康",
			Group:   demo_group,
			Enabled: true,
			Transport: TransportSpec{
				Type: TransportHTTP,
				HTTP: &HTTPSpec{Method: "GET", URL: health_url, Headers: auth_header, TimeoutMS: 2000},
			},
			Schedule: &ScheduleSpec{IntervalMS: interval_ms},
			Extract:  &ExtractSpec{Type: ExtractJSON, Path: "status", ValueType: ParamString},
			UI:       UISpec{Type: "status", Order: 10},
		},
		{
			ID:      "demo-uptime",
			Kind:    KindSource,
			Name:    "服务运行时长",
			Group:   demo_group,
			Enabled: true,
			Transport: TransportSpec{
				Type: TransportHTTP,
				HTTP: &HTTPSpec{Method: "GET", URL: health_url, Headers: auth_header, TimeoutMS: 2000},
			},
			Schedule: &ScheduleSpec{IntervalMS: 3000},
			Extract:  &ExtractSpec{Type: ExtractJSON, Path: "uptime_s", ValueType: ParamNumber},
			UI:       UISpec{Type: "metric", Unit: "s", Precision: 1, Order: 20},
		},
		{
			ID:      "demo-subscribers",
			Kind:    KindSource,
			Name:    "SSE 订阅数",
			Group:   demo_group,
			Enabled: true,
			Transport: TransportSpec{
				Type: TransportHTTP,
				HTTP: &HTTPSpec{Method: "GET", URL: health_url, Headers: auth_header, TimeoutMS: 2000},
			},
			Schedule: &ScheduleSpec{IntervalMS: 5000},
			Extract:  &ExtractSpec{Type: ExtractJSON, Path: "server.subscribers", ValueType: ParamNumber},
			UI:       UISpec{Type: "metric", Unit: "个", Precision: 0, Order: 30},
		},
		{
			ID:      "demo-refresh-uptime",
			Kind:    KindAction,
			Name:    "刷新运行时长",
			Group:   demo_group,
			Enabled: true,
			Transport: TransportSpec{
				Type: TransportHTTP,
				HTTP: &HTTPSpec{
					Method:    "POST",
					URL:       fmt.Sprintf("%s/api/v1/sources/demo-uptime/refresh", base_url),
					Headers:   auth_header,
					TimeoutMS: 2000,
				},
			},
			UI: UISpec{
				Type: "button", Variant: "primary", Order: 10,
				Confirm: &ConfirmSpec{Enabled: true, Title: "确认刷新？", Message: "将立即触发一次运行时长采集。"},
			},
		},
		{
			ID:      "demo-servo",
			Kind:    KindAction,
			Name:    "舵机角度",
			Group:   "示例设备",
			Enabled: false,
			Params: map[string]ParamSpec{
				"angle": {
					Type: ParamNumber, Label: "舵机角度", Required: true,
					Default: float64(90), Min: float_ptr(0), Max: float_ptr(180),
				},
			},
			Transport: TransportSpec{
				Type: TransportUDP,
				UDP: &UDPSpec{
					Address:   "192.168.1.20:9000",
					Payload:   "SERVO:${params.angle}",
					Encoding:  "text",
					TimeoutMS: 1000,
				},
			},
			UI: UISpec{
				Type: "button", Variant: "ghost", Order: 10,
				Confirm: &ConfirmSpec{Enabled: true, Title: "确认下发角度？", Message: "将向舵机控制器发送角度指令。"},
			},
		},
	}
	for _, entry := range entries {
		if err := Validate(entry); err != nil {
			panic(fmt.Sprintf("示例注册项 %s 非法: %v", entry.ID, err))
		}
	}
	return entries
}

// float_ptr 返回浮点数指针，用于可选的最小值/最大值字段
func float_ptr(v float64) *float64 { return &v }
