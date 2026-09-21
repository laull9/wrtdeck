#!/usr/bin/env bash
# 启动后端开发服务：关闭鉴权、写入自检示例注册项、监听 127.0.0.1:8080

set -eu

. "$(dirname "$0")/_common.sh"

root="$(project_root)"
go_bin="$(find_go)"

cd "$root"
log_info backend "启动 Go 服务（dev 模式）"
exec "$go_bin" run ./cmd/wrtdeck -dev "$@"
