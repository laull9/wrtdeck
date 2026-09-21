#!/bin/sh
# 在开发机上模拟一次完整的 OpenWrt 安装，验证「包内容 + procd 脚本」真的能把服务跑起来。
#
# 流程与设备上的 opkg install 一一对应：
#   解包 ipk → 按 init 脚本的方式启动服务 → 首启生成 Token → 验证 HTTP 与鉴权
#
# 两处必要的模拟，其余都是真实执行：
#   1. 把 procd_* 系列函数替换成记录参数的桩，因为开发机上没有 procd；
#   2. 交叉编译出的 Linux ELF 无法在 macOS 上执行，因此换成同源码的本机产物，
#      包内二进制的 ELF 属性（静态链接、目标架构）由 check-ipk.sh 负责校验。
#
# 用法：sh scripts/simulate-openwrt.sh [ipk 路径]

set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
go_bin="${GO_BIN:-}"
if [ -z "$go_bin" ]; then
  if command -v go >/dev/null 2>&1; then go_bin="$(command -v go)"; else go_bin=/opt/homebrew/bin/go; fi
fi

ipk="${1:-}"
if [ -z "$ipk" ]; then
  ipk="$(ls -1 "$root"/dist/owdash_*.ipk 2>/dev/null | head -1)"
fi
if [ -z "$ipk" ] || [ ! -f "$ipk" ]; then
  echo "找不到 ipk，请先执行：make ipk" >&2
  exit 1
fi

port="${SIM_PORT:-8096}"
stage="$(mktemp -d)"
server_pid=""

cleanup() {
  if [ -n "$server_pid" ]; then kill "$server_pid" 2>/dev/null || true; fi
  rm -rf "$stage"
}
trap cleanup EXIT INT TERM

step() { printf '\n== %s ==\n' "$1"; }
ok()   { printf '  [ok]   %s\n' "$1"; }

# ── 1. 解包 ipk，还原设备上的文件树 ────────────────────────────────────────
step "1. 解包并还原文件树"
( cd "$stage" && ar x "$ipk" )
( cd "$stage" && tar xzf control.tar.gz )
mkdir -p "$stage/data"
( cd "$stage/data" && tar xzf "$stage/data.tar.gz" )

for f in usr/bin/owdash etc/init.d/owdash etc/owdash/config.json; do
  [ -f "$stage/data/$f" ] || { echo "包内缺少 $f" >&2; exit 1; }
done
ok "包内文件齐全，解包到 $stage/data"
printf '  file: %s\n' "$(file -b "$stage/data/usr/bin/owdash")"

# 交叉编译产物无法在本机执行，换成同源码的本机二进制
"$go_bin" build -trimpath -o "$stage/data/usr/bin/owdash" "$root/cmd/owdash"
ok "已替换为本机二进制用于运行时验证（不影响包内 ELF 属性）"

# 改到空闲端口，避免与开发环境的 8080 冲突；
# data_dir 在设备上是 /etc/owdash，模拟时一并重定向到临时目录。
sed -i.bak -e "s/\"0.0.0.0:8080\"/\"127.0.0.1:${port}\"/" \
           -e "s#\"data_dir\": \"/etc/owdash\"#\"data_dir\": \"${stage}/data/etc/owdash\"#" \
    "$stage/data/etc/owdash/config.json"
rm -f "$stage/data/etc/owdash/config.json.bak"
ok "监听地址改为 127.0.0.1:${port}，data_dir 重定向到临时目录"

# ── 2. 桩化 procd，执行 init 脚本 ──────────────────────────────────────────
step "2. 执行 init 脚本的 start_service"
init="$stage/data/etc/init.d/owdash"

# init 脚本里的路径是设备上的绝对路径，模拟时重定向到临时目录；
# 路径与包内容是否一致已由 check-ipk.sh 断言。
sed -i.bak "s#^PROG=/usr/bin/owdash#PROG=$stage/data/usr/bin/owdash#" "$init"
sed -i.bak "s#^CONF_DIR=/etc/owdash#CONF_DIR=$stage/data/etc/owdash#" "$init"
sed -i.bak "s#^CONF=\"\$CONF_DIR/config.json\"#CONF=\"\$CONF_DIR/config.json\"#" "$init"
rm -f "$init.bak"

# 记录 procd 收到的参数，稍后用这些参数真实启动进程
captured_cmd=""
captured_respawn=""
captured_limits=""
procd_open_instance() { :; }
procd_close_instance() { :; }
procd_set_param() {
  _key="$1"
  shift
  case "$_key" in
    command) captured_cmd="$*" ;;
    respawn) captured_respawn="$*" ;;
    limits)  captured_limits="$*" ;;
    stdout|stderr) : ;;
  esac
}

# shellcheck disable=SC1090
. "$init"
start_service

if [ -z "$captured_cmd" ]; then
  echo "init 脚本没有向 procd 下发 command" >&2
  exit 1
fi
ok "procd command = $captured_cmd"
ok "procd respawn = $captured_respawn"
ok "procd limits  = $captured_limits"

# ── 3. 用 init 下发的命令真实启动服务 ──────────────────────────────────────
step "3. 启动服务并验证可用性"
# captured_cmd 是 "程序 参数 参数" 形式的字符串，按空格拆成 argv 再执行
# shellcheck disable=SC2086
set -- $captured_cmd
"$@" > "$stage/server.log" 2>&1 &
server_pid=$!

i=0
while [ "$i" -lt 30 ]; do
  if curl -fsS "http://127.0.0.1:${port}/api/v1/health" > "$stage/health.json" 2>/dev/null; then
    break
  fi
  i=$((i + 1))
  sleep 0.5
done

if [ "$i" -ge 30 ]; then
  echo "服务在 15s 内没有就绪，日志：" >&2
  cat "$stage/server.log" >&2
  exit 1
fi

printf '  health: %s\n' "$(cat "$stage/health.json")"
ok "服务已就绪并响应 /api/v1/health"

curl -fsS "http://127.0.0.1:${port}/" -o "$stage/index.html"
if grep -q 'WrtDeck' "$stage/index.html"; then
  ok "前端资源已随二进制提供（/ 返回面板页面）"
else
  echo "根路径没有返回面板页面" >&2
  exit 1
fi

# ── 4. 首启 Token 与鉴权 ───────────────────────────────────────────────────
step "4. 首启 Token 与鉴权"
# init 脚本额外提供的 token 命令
init_token="$(token)"
if [ -n "$init_token" ]; then
  ok "init 脚本的 token 命令可用（长度 ${#init_token}）"
else
  echo "token 命令没有输出" >&2
  exit 1
fi

if [ -f "$stage/data/etc/owdash/secrets.json" ]; then
  ok "首启已生成 secrets.json"
else
  echo "首启没有生成 secrets.json" >&2
  exit 1
fi

# 该实例关闭了鉴权（auth.disabled 未设置但包内默认 auth 为空对象 = 开启鉴权）
code="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${port}/api/v1/dashboard")"
if [ "$code" = "401" ]; then
  ok "未带 Token 访问返回 401（鉴权生效）"
else
  echo "未知鉴权状态，期望 401，实际 $code" >&2
  exit 1
fi

code="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $init_token" "http://127.0.0.1:${port}/api/v1/dashboard")"
if [ "$code" = "200" ]; then
  ok "带 init 命令输出的 Token 访问返回 200"
else
  echo "Token 鉴权失败，期望 200，实际 $code" >&2
  exit 1
fi

# ── 5. 数据落盘位置 ────────────────────────────────────────────────────────
step "5. 数据落盘位置"
if [ -f "$stage/data/etc/owdash/secrets.json" ]; then
  ok "secrets.json 落在 data_dir（/etc/owdash）内"
else
  echo "secrets.json 未落在 data_dir 内" >&2
  exit 1
fi
if [ -f "$stage/data/etc/owdash/registry.json" ]; then
  echo "空注册表不应在启动时落盘" >&2
  exit 1
fi
ok "空注册表不落盘（无变更不写闪存）"

# 写入一条注册项，验证持久化路径确实可用
curl -fsS -X PUT "http://127.0.0.1:${port}/api/v1/registry/sim-selfcheck" \
  -H "Authorization: Bearer $init_token" -H 'Content-Type: application/json' \
  -d "{\"id\":\"sim-selfcheck\",\"kind\":\"source\",\"name\":\"自检\",\"enabled\":false,\"transport\":{\"type\":\"http\",\"http\":{\"method\":\"GET\",\"url\":\"http://127.0.0.1:${port}/api/v1/health\"}}}" \
  > "$stage/put.json"
ok "注册项写入成功：$(head -c 80 "$stage/put.json")"

if [ -f "$stage/data/etc/owdash/registry.json" ]; then
  ok "registry.json 已落盘到 data_dir"
else
  echo "写入注册项后 registry.json 仍未落盘" >&2
  exit 1
fi

if [ -f "$stage/data/etc/owdash/state.json" ]; then
  echo "运行状态不应落盘（会产生闪存写放大）" >&2
  exit 1
fi
ok "运行状态仅驻内存，未生成 state.json"

if grep -q '127.0.0.1:5173' "$stage/server.log"; then
  echo "生产包不应提示前端开发地址" >&2
  exit 1
fi
ok "启动日志为生产形态（无开发模式提示）"

printf '\n== 结果 ==\n'
printf '  安装模拟全部通过：init 脚本可拉起服务、首启 Token 可用、鉴权生效、数据落盘正确\n'
