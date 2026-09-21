#!/usr/bin/env bash
# 公共函数：定位 go / pnpm，并统一日志前缀
# 注意：macOS 自带 bash 3.2，这里只使用 3.2 就支持的语法

set -eu

# 项目根目录。source 时算一次并缓存：project_root 早期实现每次都 cd，
# 调用方一旦先切到子目录就会推导出错误路径，因此改成幂等的纯查询。
PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

project_root() {
  printf '%s\n' "$PROJECT_ROOT"
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

# 打印产物路径与大小。
# 不要用 ls 解析列：BSD ls 的日期是两列（2026-09-21 15:31），
# GNU ls 是三列（Sep 21 15:31），写死 $9 在 macOS 上会取到空字符串。
report_artifact() {
  printf '[%s] 产物 %s %s\n' "$1" "$2" "$(du -h "$2" | awk '{ print $1 }')"
}

# 构建前端并写回 embed 占位文件。
# Vite 会清空输出目录，而 //go:embed all:dist 要求该目录非空，
# 因此每次构建后都要放回 .gitkeep，否则空仓库克隆后无法编译。
build_web() {
  prefix="${1:-build}"
  target="$(project_root)"
  cd "$target/web"
  log_info "$prefix" "构建前端"
  run_pnpm install --frozen-lockfile
  run_pnpm build
  touch "$target/internal/webui/dist/.gitkeep"
}

# 定位可用的 python3。apk 打包只用标准库，因此不要求 Pillow。
find_python3() {
  for candidate in python3 /opt/homebrew/bin/python3 /usr/local/bin/python3 /usr/bin/python3; do
    if command -v "$candidate" >/dev/null 2>&1; then
      command -v "$candidate"
      return 0
    fi
  done
  echo "找不到 python3，apk 打包需要它（仅用标准库）" >&2
  return 1
}

# 准备 apk 签名密钥。首次打包时生成一对 4096 位 RSA 密钥，
# 私钥留在 packaging/keys/ 供后续打包复用，公钥导到 dist/ 便于拷进设备的 /etc/apk/keys/。
# 设置 APK_SIGN=0 可跳过签名，此时安装需要 apk add --allow-untrusted。
# 结果写入 APK_SIGN_KEY / APK_SIGN_NAME / APK_SIGN_PUB 三个全局变量。
ensure_apk_key() {
  APK_SIGN_KEY=""
  APK_SIGN_NAME="owdash-local.rsa.pub"
  APK_SIGN_PUB="$(project_root)/dist/$APK_SIGN_NAME"
  if [ "${APK_SIGN:-1}" = "0" ]; then
    return 0
  fi

  APK_SIGN_KEY="$(project_root)/packaging/keys/owdash-local.rsa"
  if [ ! -f "$APK_SIGN_KEY" ]; then
    mkdir -p "$(dirname "$APK_SIGN_KEY")"
    log_info apk "首次打包，生成本地 apk 签名密钥 packaging/keys/owdash-local.rsa"
    if ! openssl genrsa -out "$APK_SIGN_KEY" 4096 2>/dev/null; then
      echo "生成签名密钥失败，请检查 openssl；或设置 APK_SIGN=0 跳过签名" >&2
      exit 1
    fi
    chmod 600 "$APK_SIGN_KEY"
  fi
}
