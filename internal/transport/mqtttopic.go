package transport

import (
	"fmt"
	"hash/fnv"
	"strings"
)

// derive_client_id 由连接标识推导出稳定的客户端 ID，
// 同一 broker 上的多个注册项因此共用同一个会话而不是互相顶号。
func derive_client_id(key string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return fmt.Sprintf("owdash-%08x", h.Sum32())
}

// conn_key 计算连接池的键：broker 相同但身份或客户端 ID 不同的注册项必须分开连接
func conn_key(broker, username string, password string, client_id string) string {
	return strings.Join([]string{broker, username, password, client_id}, "\x00")
}
