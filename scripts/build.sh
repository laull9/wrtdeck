#!/usr/bin/env bash
# 构建前端并把产物嵌入后端二进制，输出到 dist/wrtdeck

set -eu

. "$(dirname "$0")/_common.sh"

root="$(project_root)"
go_bin="$(find_go)"
version="${VERSION:-dev}"

build_web build

cd "$root"
mkdir -p dist
log_info build "构建后端（本机平台）"
"$go_bin" build \
  -trimpath \
  -ldflags="-s -w -X main.version=${version}" \
  -o dist/wrtdeck \
  ./cmd/wrtdeck

report_artifact build dist/wrtdeck
