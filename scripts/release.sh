#!/usr/bin/env bash
# 交叉编译 OpenWrt ARM64 版本：CGO_ENABLED=0，产出无 libc 依赖的静态 ELF

set -eu

. "$(dirname "$0")/_common.sh"

root="$(project_root)"
go_bin="$(find_go)"
version="${VERSION:-dev}"
target_arch="${GOARCH:-arm64}"

build_web release

cd "$root"
mkdir -p dist
log_info release "交叉编译 linux/${target_arch}"
CGO_ENABLED=0 GOOS=linux GOARCH="$target_arch" "$go_bin" build \
  -trimpath \
  -ldflags="-s -w -buildid= -X main.version=${version}" \
  -o "dist/wrtdeck-linux-${target_arch}" \
  ./cmd/wrtdeck

report_artifact release "dist/wrtdeck-linux-${target_arch}"
