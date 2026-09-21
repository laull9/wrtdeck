#!/usr/bin/env bash
# 公共函数：定位 go / pnpm，并统一日志前缀
# 注意：macOS 自带 bash 3.2，这里只使用 3.2 就支持的语法

set -eu

# 项目根目录，基于脚本自身位置推导
project_root() {
  cd "$(dirname "$0")/.." && pwd
}

# 定位 go 可执行文件，优先使用 PATH，其次探测常见安装位置
find_go() {
  if [ -n "${GO_BIN:-}" ] && [ -x "${GO_BIN}" ]; then
    printf '%s' "${GO_BIN}"
    return 0
  fi
  if command -v go >/dev/null 2>&1; then
    command -v go
    return 0
  fi
  for candidate in /opt/homebrew/bin/go /usr/local/go/bin/go "$HOME/go/bin/go"; do
    if [ -x "$candidate" ]; then
      printf '%s' "$candidate"
      return 0
    fi
  done
  echo "找不到 go，请安装 Go 或设置 GO_BIN 环境变量" >&2
  return 1
}

# 执行 pnpm 命令，没有全局 pnpm 时退回 corepack
run_pnpm() {
  if command -v pnpm >/dev/null 2>&1; then
    pnpm "$@"
    return $?
  fi
  if command -v corepack >/dev/null 2>&1; then
    corepack pnpm "$@"
    return $?
  fi
  echo "找不到 pnpm，请安装 pnpm 或启用 corepack" >&2
  return 1
}

# 带前缀的信息输出
log_info() {
  printf '[%s] %s\n' "$1" "$2"
}
