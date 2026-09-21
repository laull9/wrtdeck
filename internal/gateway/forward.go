package gateway

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// dial_timeout 是连接面板本体的上限。本体就在回环上，连不上只可能是没在跑。
const dial_timeout = 3 * time.Second

// new_client 构造转发用的 HTTP 客户端。
//
// 上游在设备回环上，因此允许自签证书；响应头超时用调用方给的时限，
// 响应体不限时，实时事件流这种长连接才不会被半路掐断。
func new_client(timeout time.Duration) *http.Client {
	dial := dial_timeout
	if timeout > 0 && timeout < dial {
		dial = timeout
	}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   dial,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
		TLSHandshakeTimeout:   dial,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: time.Second,
		DisableCompression:    true,
		MaxIdleConns:          2,
	}
	return &http.Client{Transport: transport}
}

// forward 把请求发给面板本体。
// 到这一步请求头已经整理干净，函数只负责发出去并把错误原样带回。
func forward(client *http.Client, req *http.Request, opts Options) (*http.Response, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	// 正常请求不记日志：CGI 的标准错误最终会进系统日志，
	// 每次刷新都写一行会把设备上真正有用的记录淹掉。
	if resp.StatusCode >= http.StatusInternalServerError {
		opts.log("面板本体返回 %d：%s %s", resp.StatusCode, req.Method, req.URL.Path)
	}
	return resp, nil
}
