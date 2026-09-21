#!/usr/bin/env bash
# 只构建前端：安装依赖并把产物写入 internal/webui/dist

set -eu

. "$(dirname "$0")/_common.sh"

build_web web

log_info web "产物已写入 internal/webui/dist"
