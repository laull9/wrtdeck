#!/usr/bin/env bash
# 启动前端开发服务器：Vite 监听 127.0.0.1:5173，并把 /api 代理到后端

set -eu

. "$(dirname "$0")/_common.sh"

root="$(project_root)/web"

cd "$root"
if [ ! -d node_modules ]; then
  log_info frontend "未检测到依赖，先执行安装"
  run_pnpm install
fi

log_info frontend "启动 Vite 开发服务器"
if command -v pnpm >/dev/null 2>&1; then
  exec pnpm dev "$@"
fi
exec corepack pnpm dev "$@"
