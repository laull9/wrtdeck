#!/usr/bin/env bash
# 只构建前端：安装依赖并把产物写入 internal/webui/dist

set -eu

. "$(dirname "$0")/_common.sh"

root="$(project_root)"

cd "$root/web"
log_info web "安装依赖"
run_pnpm install --frozen-lockfile
log_info web "构建前端"
run_pnpm build

# Vite 会清空输出目录，重新放回占位文件以免 git 显示删除
touch "$root/internal/webui/dist/.gitkeep"
log_info web "产物已写入 internal/webui/dist"
