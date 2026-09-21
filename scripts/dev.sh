#!/usr/bin/env bash
# 同时启动前后端开发服务，任意一方退出时一并收尾

set -eu

. "$(dirname "$0")/_common.sh"

root="$(project_root)"
backend_pid=""
frontend_pid=""

# 退出时清理两个子进程
cleanup() {
  if [ -n "$backend_pid" ]; then
    kill "$backend_pid" 2>/dev/null || true
  fi
  if [ -n "$frontend_pid" ]; then
    kill "$frontend_pid" 2>/dev/null || true
  fi
}

trap cleanup INT TERM EXIT

log_info dev "后端 http://127.0.0.1:8080  前端 http://127.0.0.1:5173"
sh "$root/scripts/dev-backend.sh" &
backend_pid=$!

sh "$root/scripts/dev-frontend.sh" &
frontend_pid=$!

log_info dev "按 Ctrl+C 结束"

# 等待任一方退出后由 trap 收尾
wait "$backend_pid" "$frontend_pid" || true
