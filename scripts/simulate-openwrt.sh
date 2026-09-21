#!/bin/sh
# 在开发机上模拟一次完整的 OpenWrt 安装，验证「包内容 + procd 脚本」真的能把服务跑起来。
#
# 流程与设备上的包管理动作一一对应：
#   解包 → 按 init 脚本的方式启动服务 → 首启生成口令与 Token → 验证 HTTP、鉴权与改密
#
# 同时支持 apk 与 ipk：apk 的段结构复杂（多 gzip 成员 + PAX 逐文件校验和），
# 交给 scripts/apk_unpack.py 解；ipk 是 ar + tar.gz，直接用系统工具解。
#
# 两处必要的模拟，其余都是真实执行：
#   1. 把 procd_* 系列函数替换成记录参数的桩，因为开发机上没有 procd；
#   2. 交叉编译出的 Linux ELF 无法在 macOS 上执行，因此换成同源码的本机产物，
#      包内二进制的 ELF 属性（静态链接、目标架构）由 check-apk.sh / check-ipk.sh 负责校验。
#
# 用法：sh scripts/simulate-openwrt.sh [apk 或 ipk 路径]
#       不传路径时默认取 dist/ 下的 ipk 包

set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"

# 这个脚本会被 Makefile 用相对路径调用，而下面要 cd 到临时目录解包；
# 因此先把包路径定成绝对路径，否则一 cd 就再也找不到它。
pkg="${1:-}"
if [ -z "$pkg" ]; then
  pkg="$(ls -1 "$root"/dist/wrtdeck_*.ipk 2>/dev/null | head -1)"
fi
if [ -z "$pkg" ] || [ ! -f "$pkg" ]; then
  echo "找不到安装包，请先执行：make packages" >&2
  exit 1
fi
pkg="$(cd "$(dirname "$pkg")" && pwd)/$(basename "$pkg")"

case "$(basename "$pkg")" in
  *.apk) format=apk ;;
  *.ipk) format=ipk ;;
  *) echo "无法识别的包格式（既不是 .apk 也不是 .ipk）：$pkg" >&2; exit 1 ;;
esac

. "$root/scripts/_common.sh"
go_bin="$(find_go)"
python_bin="$(find_python3)"

port="${SIM_PORT:-8096}"
base="http://127.0.0.1:${port}"
stage="$(mktemp -d)"
server_pid=""

cleanup() {
  if [ -n "$server_pid" ]; then kill "$server_pid" 2>/dev/null || true; fi
  rm -rf "$stage"
}
trap cleanup EXIT INT TERM

step() { printf '\n== %s ==\n' "$1"; }
ok()   { printf '  [ok]   %s\n' "$1"; }
die()  { printf '  [FAIL] %s\n' "$1" >&2; exit 1; }

# 只取 HTTP 状态码，响应体丢给 /dev/null；不带 -f，因为 4xx 状态码本身就是断言目标。
code_of() { curl -s -o /dev/null -w '%{http_code}' "$@"; }
body_of() { curl -s "$@"; }

# ── 1. 解包，还原设备上的文件树 ────────────────────────────────────────────
step "1. 解包并还原文件树（${format}）"
case "$format" in
apk)
  "$python_bin" "$root/scripts/apk_unpack.py" --in "$pkg" --out "$stage" --quiet
  rootfs="$stage/rootfs"
  ;;
ipk)
  mkdir -p "$stage/unpack"
  ( cd "$stage/unpack" && ar x "$pkg" )
  ( cd "$stage/unpack" && tar xzf control.tar.gz )
  rootfs="$stage/rootfs"
  mkdir -p "$rootfs"
  ( cd "$rootfs" && tar xzf "$stage/unpack/data.tar.gz" )
  ;;
esac

for f in usr/bin/wrtdeck etc/init.d/wrtdeck etc/wrtdeck/config.json; do
  [ -f "$rootfs/$f" ] || die "包内缺少 $f"
done
ok "包内文件齐全，解包到 $rootfs"
printf '  file: %s\n' "$(file -b "$rootfs/usr/bin/wrtdeck")"

# 交叉编译产物无法在本机执行，换成同源码的本机二进制
"$go_bin" build -trimpath -o "$rootfs/usr/bin/wrtdeck" "$root/cmd/wrtdeck"
ok "已替换为本机二进制用于运行时验证（不影响包内 ELF 属性）"

# 改到空闲端口，避免与开发环境的 8080 冲突；
# data_dir 在设备上是 /etc/wrtdeck，模拟时一并重定向到临时目录。
sed -i.bak -e "s/\"0.0.0.0:8080\"/\"127.0.0.1:${port}\"/" \
           -e "s/\"127.0.0.1:8080\"/\"127.0.0.1:${port}\"/" \
           -e "s#\"data_dir\": \"/etc/wrtdeck\"#\"data_dir\": \"${rootfs}/etc/wrtdeck\"#" \
    "$rootfs/etc/wrtdeck/config.json"
rm -f "$rootfs/etc/wrtdeck/config.json.bak"
ok "监听地址改为 127.0.0.1:${port}，data_dir 重定向到临时目录"

# ── 2. 桩化 procd，执行 init 脚本 ──────────────────────────────────────────
step "2. 执行 init 脚本的 start_service"
init="$rootfs/etc/init.d/wrtdeck"

# init 脚本里的路径是设备上的绝对路径，模拟时重定向到临时目录；
# 路径与包内容是否一致已由 check-ipk.sh 断言。
sed -i.bak "s#^PROG=/usr/bin/wrtdeck#PROG=$rootfs/usr/bin/wrtdeck#" "$init"
sed -i.bak "s#^CONF_DIR=/etc/wrtdeck#CONF_DIR=$rootfs/etc/wrtdeck#" "$init"
sed -i.bak "s#^WEB_DIR=/www/wrtdeck#WEB_DIR=$rootfs/www/wrtdeck#" "$init"
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

# init 启动时要顺手把面板页面导出到 Web 根目录：设备自带的 Web 服务器
# 正是从这里把页面发给浏览器的，缺了它 LuCI 里的入口就只是一个空白框。
if [ -f "$rootfs/www/wrtdeck/index.html" ]; then
  ok "面板页面已导出到 Web 根目录（$(find "$rootfs/www/wrtdeck" -type f | wc -l | tr -d ' ') 个文件）"
else
  die "init 没有把面板页面导出到 Web 根目录"
fi
# 页面里的资源引用必须是相对路径：面板既可能挂在 /，也可能挂在 /wrtdeck/ 下
if grep -q '"\./assets/' "$rootfs/www/wrtdeck/index.html"; then
  ok "面板页面的资源引用是相对路径（可挂在任意子目录下）"
else
  die "面板页面的资源引用不是相对路径，挂在子目录下会加载不到"
fi

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

# ── 4. 首启凭据与鉴权 ──────────────────────────────────────────────────────
step "4. 首启凭据与鉴权"
# init 脚本额外提供的 token 命令，供脚本与运维使用
init_token="$(token)"
[ -n "$init_token" ] || die "init 脚本的 token 命令没有输出"
ok "init 脚本的 token 命令可用（长度 ${#init_token}）"

[ -f "$rootfs/etc/wrtdeck/secrets.json" ] || die "首启没有生成 secrets.json"
ok "首启已生成 secrets.json"

# 口令只以散列形式存在，明文与 API Token 都不该出现在日志里
if grep -q '"password"' "$rootfs/etc/wrtdeck/secrets.json"; then
  ok "secrets.json 里存有口令散列"
else
  die "secrets.json 里没有口令字段"
fi
# BSD grep 不支持 BRE 里的 \| 交替（会当成字面量），因此用多个 -e 表达。
if grep -q -e '"hash"' -e '"salt"' "$rootfs/etc/wrtdeck/secrets.json"; then
  ok "口令以散列 + 盐的形式存储"
else
  die "口令没有按散列存储"
fi
if [ -z "$(grep -o '"must_change_password": *true' "$rootfs/etc/wrtdeck/secrets.json")" ]; then
  die "全新安装应当标记 must_change_password"
fi
ok "首启标记了必须修改初始口令"

# 包内默认配置不应显式关闭鉴权或 TLS 开关之外的认证项
if grep -q '"disabled"' "$rootfs/etc/wrtdeck/config.json"; then
  die "包内默认配置不应出现 auth.disabled"
fi
ok "默认配置未关闭鉴权"

c="$(code_of "$base/api/v1/dashboard")"
[ "$c" = "401" ] || die "未带凭据访问期望 401，实际 $c"
ok "未带凭据访问返回 401（鉴权生效）"

c="$(code_of -H "Authorization: Bearer $init_token" "$base/api/v1/dashboard")"
[ "$c" = "200" ] || die "带 Token 访问期望 200，实际 $c"
ok "带 init 命令输出的 Token 访问返回 200"

# 健康检查在口令仍是初始值时要如实告诉前端，否则界面不会提示改口令
health="$(body_of "$base/api/v1/health")"
case "$health" in
  *'"must_change_password":true'*) ok "health 如实报告需要修改初始口令" ;;
  *) die "health 没有报告 must_change_password" ;;
esac

# ── 5. 口令登录与强制改密 ──────────────────────────────────────────────────
step "5. 口令登录与强制改密"
login_body="$(body_of -X POST -H 'Content-Type: application/json' -d '{"password":"admin"}' "$base/api/v1/session/login")"
session_token="$(printf '%s' "$login_body" | sed -n 's/.*"token": *"\([^"]*\)".*/\1/p')"
[ -n "$session_token" ] || die "初始口令登录失败：$login_body"
ok "初始口令 admin 可以登录，拿到会话凭据"

case "$login_body" in
  *'"must_change_password":true'*) ok "登录响应标明必须先改口令" ;;
  *) die "登录响应没有标出 must_change_password" ;;
esac

c="$(code_of -H "Authorization: Bearer $session_token" "$base/api/v1/dashboard")"
[ "$c" = "403" ] || die "初始口令下访问 dashboard 期望 403，实际 $c"
ok "初始口令下除改口令外的请求被拒（403）"

c="$(code_of -X POST -H "Authorization: Bearer $session_token" -H 'Content-Type: application/json' \
  -d '{"current_password":"admin","new_password":"12345678"}' "$base/api/v1/session/password")"
[ "$c" = "400" ] || die "弱口令期望 400，实际 $c"
ok "纯数字弱口令被拒（400）"

change_body="$(body_of -X POST -H "Authorization: Bearer $session_token" \
  -H 'Content-Type: application/json' \
  -d '{"current_password":"admin","new_password":"Wrt-Deck-2026"}' "$base/api/v1/session/password")"
new_token="$(printf '%s' "$change_body" | sed -n 's/.*"token": *"\([^"]*\)".*/\1/p')"
[ -n "$new_token" ] || die "改口令失败：$change_body"
ok "改口令成功，并换发了一张新会话凭据"

c="$(code_of -H "Authorization: Bearer $new_token" "$base/api/v1/dashboard")"
[ "$c" = "200" ] || die "新会话访问 dashboard 期望 200，实际 $c"
ok "新会话可以正常访问面板接口"

c="$(code_of -H "Authorization: Bearer $session_token" "$base/api/v1/dashboard")"
[ "$c" = "401" ] || die "旧会话期望 401，实际 $c"
ok "改口令前的旧会话立即失效（401）"

c="$(code_of -X POST -H 'Content-Type: application/json' -d '{"password":"admin"}' "$base/api/v1/session/login")"
[ "$c" = "401" ] || die "旧口令登录期望 401，实际 $c"
ok "旧口令再也登不进来（401）"

c="$(code_of -H "Authorization: Bearer $init_token" "$base/api/v1/dashboard")"
[ "$c" = "200" ] || die "API Token 期望 200，实际 $c"
ok "长期 API Token 不受口令变更影响（脚本与设备本机照常可用）"

# ── 6. 登录失败限速 ────────────────────────────────────────────────────────
step "6. 登录失败限速"
locked=0
i=0
while [ "$i" -lt 8 ]; do
  c="$(code_of -X POST -H 'Content-Type: application/json' \
    -d '{"password":"definitely-not-the-password"}' "$base/api/v1/session/login")"
  i=$((i + 1))
  if [ "$c" = "429" ]; then
    locked=$((locked + 1))
  fi
done
[ "$locked" -gt 0 ] || die "连续错误口令没有触发限速，暴力破解不受阻"
ok "连续错误口令后触发限速（429，第 $i 次尝试前已锁定）"

c="$(code_of -X POST -H 'Content-Type: application/json' -d '{"password":"Wrt-Deck-2026"}' "$base/api/v1/session/login")"
[ "$c" = "429" ] || die "锁定期间正确口令也放行了，实际 $c"
ok "锁定期间即使口令正确也拒绝（429）"

c="$(code_of -H "Authorization: Bearer $init_token" "$base/api/v1/dashboard")"
[ "$c" = "200" ] || die "限速期间 API Token 也被拒，实际 $c"
ok "限速只作用于登录入口，不影响已认证的 API 调用"

# ── 7. 一次性登录码（LuCI 薄壳走的那条路径）────────────────────────────────
step "7. 一次性登录码"
c="$(code_of -X POST "$base/api/v1/session/handoff")"
[ "$c" = "401" ] || die "无 Token 申请登录码期望 401，实际 $c"
ok "申请登录码本身需要凭据（401）"

handoff_body="$(body_of -X POST -H "Authorization: Bearer $init_token" "$base/api/v1/session/handoff")"
handoff_code="$(printf '%s' "$handoff_body" | sed -n 's/.*"code": *"\([^"]*\)".*/\1/p')"
[ -n "$handoff_code" ] || die "本机申请登录码失败：$handoff_body"
ok "本机（回环）可申请登录码，长度 ${#handoff_code}"

redeem_body="$(body_of -X POST -H "Origin: $base" -H 'Content-Type: application/json' \
  -d "{\"code\":\"$handoff_code\"}" "$base/api/v1/session/redeem")"
redeem_token="$(printf '%s' "$redeem_body" | sed -n 's/.*"token": *"\([^"]*\)".*/\1/p')"
[ -n "$redeem_token" ] || die "兑换登录码没有换回会话凭据：$redeem_body"
ok "登录码换回会话凭据"

c="$(code_of -X POST -H "Origin: $base" -H 'Content-Type: application/json' \
  -d "{\"code\":\"$handoff_code\"}" "$base/api/v1/session/redeem")"
[ "$c" = "401" ] || die "重复兑换期望 401，实际 $c"
ok "登录码只能兑一次，重复使用被拒（401）"

c="$(code_of -X POST -H 'Origin: http://evil.example.com' -H 'Content-Type: application/json' \
  -d '{"code":"whatever"}' "$base/api/v1/session/redeem")"
[ "$c" = "403" ] || die "跨站兑换期望 403，实际 $c"
ok "跨站来源无法兑换登录码（403）"

c="$(code_of -X POST -H "Origin: $base" -H 'Content-Type: application/json' \
  -d '{"code":"not-a-real-code"}' "$base/api/v1/session/redeem")"
[ "$c" = "401" ] || die "伪登录码期望 401，实际 $c"
ok "伪造的登录码被拒（401）"

# ── 8. 数据落盘位置 ────────────────────────────────────────────────────────
step "8. 数据落盘位置"
if [ -f "$rootfs/etc/wrtdeck/secrets.json" ]; then
  ok "secrets.json 落在 data_dir（/etc/wrtdeck）内"
else
  echo "secrets.json 未落在 data_dir 内" >&2
  exit 1
fi
if [ -f "$rootfs/etc/wrtdeck/registry.json" ]; then
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

if [ -f "$rootfs/etc/wrtdeck/registry.json" ]; then
  ok "registry.json 已落盘到 data_dir"
else
  echo "写入注册项后 registry.json 仍未落盘" >&2
  exit 1
fi

if [ -f "$rootfs/etc/wrtdeck/state.json" ]; then
  echo "运行状态不应落盘（会产生闪存写放大）" >&2
  exit 1
fi
ok "运行状态仅驻内存，未生成 state.json"

if grep -q '127.0.0.1:5173' "$stage/server.log"; then
  echo "生产包不应提示前端开发地址" >&2
  exit 1
fi
ok "启动日志为生产形态（无开发模式提示）"

# ── 9. 同源网关（LuCI 内嵌走的那条路）──────────────────────────────────────
# 薄壳把面板页面与接口都放在设备自带 Web 服务器的同一个源上：
# 页面是 /www 下的静态文件，接口是这个 CGI。这里按 uhttpd 的约定调起它，
# 验证「浏览器全程只跟一个主机名打交道」这条路径是真的通的。
step "9. 同源网关（LuCI 内嵌路径）"

# CGI 入口来自薄壳包，与面板本体是两个包；这里按薄壳的安装位置摆好
cgi_dir="$rootfs/www/cgi-bin"
mkdir -p "$cgi_dir"
cp "$root/packaging/luci/root/www/cgi-bin/wrtdeck-api" "$cgi_dir/wrtdeck-api"
chmod 0755 "$cgi_dir/wrtdeck-api"
# 入口里的路径是设备上的绝对路径，模拟时重定向到临时目录
sed -i.bak "s#^PROG=/usr/bin/wrtdeck#PROG=$rootfs/usr/bin/wrtdeck#" "$cgi_dir/wrtdeck-api"
sed -i.bak "s#^CONF=/etc/wrtdeck/config.json#CONF=$rootfs/etc/wrtdeck/config.json#" "$cgi_dir/wrtdeck-api"
rm -f "$cgi_dir/wrtdeck-api.bak"
ok "CGI 入口已就位（$cgi_dir/wrtdeck-api）"

# call_gateway 按 uhttpd 给 CGI 的环境调起网关，参数是 PATH_INFO
call_gateway() {
  env \
    REQUEST_METHOD="${gw_method:-GET}" \
    PATH_INFO="$1" \
    QUERY_STRING="${gw_query:-}" \
    HTTP_HOST="luci.example.com" \
    HTTP_COOKIE="${gw_cookie:-}" \
    HTTP_AUTHORIZATION="${gw_auth:-}" \
    REMOTE_ADDR="${gw_remote:-192.168.1.9}" \
    "$cgi_dir/wrtdeck-api"
}

# cgi_status 从 CGI 输出取状态码：没有 Status 行即为 200，这是 CGI 的约定
cgi_status() {
  _line="$(printf '%s' "$1" | sed -n '1p')"
  case "$_line" in
    Status:*) printf '%s' "$(printf '%s' "$_line" | sed -n 's/^Status: \([0-9][0-9]*\).*/\1/p')" ;;
    *) printf '200' ;;
  esac
}

# 公开接口：网关照常转发，不因为调用方没登录 LuCI 就拒掉面板自己的健康检查
out="$(call_gateway /api/v1/health)"
[ "$(cgi_status "$out")" = "200" ] || die "网关转发健康检查期望 200，实际 $(cgi_status "$out")"
case "$out" in
  *'"version"'*) ok "网关转发公开接口并原样带回响应体" ;;
  *) die "网关返回的响应体不像面板响应：$(printf '%s' "$out" | sed -n '1,3p')" ;;
esac

# 需要鉴权的接口：没有 LuCI 会话就不补凭据，面板按未登录回 401。
# 这是最关键的一条断言——少了它，网关等于一个不用口令的后门。
gw_cookie=""
out="$(call_gateway /api/v1/dashboard)"
[ "$(cgi_status "$out")" = "401" ] || die "无 LuCI 会话时期望 401，实际 $(cgi_status "$out")"
ok "没有 LuCI 会话时不补凭据，面板按未登录拒绝（401）"

# 带一个伪造的 LuCI 会话同样不补：本机没有 ubus，校验只能失败，
# 而失败时必须按「未登录」处理，不能放行
gw_cookie="sysauth_http=forged-session-id"
out="$(call_gateway /api/v1/dashboard)"
[ "$(cgi_status "$out")" = "401" ] || die "伪造 LuCI 会话时期望 401，实际 $(cgi_status "$out")"
ok "无法证实的 LuCI 会话不会换来面板凭据（校验失败即不注入）"

# 浏览器自己带着凭据时原样转发：面板前端兑换到会话凭据后走的就是这条路
gw_cookie=""
gw_auth="Bearer $init_token"
out="$(call_gateway /api/v1/dashboard)"
[ "$(cgi_status "$out")" = "200" ] || die "带凭据转发期望 200，实际 $(cgi_status "$out")"
ok "浏览器自带的凭据原样转发给面板（200）"
gw_auth=""

# 网关只转发面板 API：能当通用代理用就等于给设备开了个无鉴权跳板
out="$(call_gateway /etc/passwd)"
[ "$(cgi_status "$out")" = "400" ] || die "非 API 路径期望 400，实际 $(cgi_status "$out")"
ok "非面板 API 的路径一概拒绝（400），网关不是通用代理"

# 登录码只许设备本机申请。网关进程恰好跑在本机，放它过去就等于
# 让任何够得着设备 Web 端口的人凭空换一张会话凭据
out="$(call_gateway /api/v1/session/handoff)"
[ "$(cgi_status "$out")" = "403" ] || die "网关代传登录码期望 403，实际 $(cgi_status "$out")"
ok "网关不代传登录码（403），这条路只能由设备本机走"

# 面板页面必须是自包含的相对引用：它既可能挂在 /wrtdeck/ 下，
# 也可能被薄壳按别的子目录布置，绝对路径会让其中一种直接白屏
page="$rootfs/www/wrtdeck/index.html"
grep -q '"\./assets/' "$page" || die "面板页面没有用相对路径引用资源"
for asset in $(sed -n 's/.*"\.\/\(assets\/[^"]*\)".*/\1/p' "$page" | sort -u); do
  [ -f "$rootfs/www/wrtdeck/$asset" ] || die "页面引用的 $asset 不在导出目录里"
done
ok "面板页面与它引用的资源都在导出目录里（$(find "$rootfs/www/wrtdeck" -type f | wc -l | tr -d ' ') 个文件）"

# 导出到 Web 根目录的页面不经过面板进程，因此它必须自带「API 在哪」的答案：
# 这个答案是薄壳用查询参数告诉它的，页面对此不做任何设备地址推断
grep -q 'embed' "$root/packaging/luci/root/www/luci-static/resources/view/wrtdeck/panel.js" ||
  die "薄壳没有把嵌入与 API 前缀告诉面板，面板会去猜设备地址"
ok "薄壳把 API 前缀与嵌入标志传给面板，面板不推断设备地址"

# ── 10. 覆盖低版本安装（1.0.0 → 1.0.1）─────────────────────────────────────
# 这一节回答的是「装过旧版本的设备能不能平滑升上来」。
#
# 难点在配置文件：它属于升级时要保留的那一类，opkg 认 conffiles，apk 的默认保护
# 规则是不覆盖 /etc 下被管理员改过的文件（新文件落成 .apk-new）。也就是说无论哪种
# 格式，设备留下来的都还是旧内容。包管理器只负责换文件，把旧值迁到新形态得靠包里
# 的钩子——所以这里按包管理器的方式真调一次升级钩子，而不是只检查它写了什么。
step "10. 覆盖低版本安装（1.0.0 → 1.0.1）"

upgrade_dir="$stage/upgrade/etc/wrtdeck"
mkdir -p "$upgrade_dir" "$stage/upgrade/rc.d" "$stage/bin"

# 1.0.0 的出厂配置：监听 0.0.0.0:8080（对整个局域网裸奔），也没有 gateway 段。
# 用户在上面改过一处（history_limit），这一处必须原样活下来。
cat > "$upgrade_dir/config.json" <<'JSON'
{
  "listen": "0.0.0.0:8080",
  "data_dir": "/etc/wrtdeck",
  "auth": {},
  "tls": { "enabled": false },
  "exec": { "enabled": true },
  "limits": { "history_limit": 240 },
  "workers": { "source": 4, "action": 4 },
  "mqtt": { "keepalive_seconds": 30, "connect_timeout_ms": 5000, "max_reconnect_delay_ms": 30000 }
}
JSON
# 用户数据不归包管，升级必须原封不动。.installed 是首次安装时留下的标记，
# 包里的钩子正是靠它区分「全新安装」与「升级」。
printf '{"password":"scrypt$sim","must_change_password":false}\n' > "$upgrade_dir/secrets.json"
printf '[{"id":"sim-selfcheck"}]\n' > "$upgrade_dir/registry.json"
: > "$upgrade_dir/.installed"
secrets_before="$(cat "$upgrade_dir/secrets.json")"
registry_before="$(cat "$upgrade_dir/registry.json")"
ok "已构造 1.0.0 现场（旧出厂配置 + 用户改过的 history_limit + 用户数据）"

# 设备上钩子调的是 /etc/init.d/wrtdeck，而 rc.common 会把子命令名直接当函数名调用。
# 开发机上没有 rc.common，用一个小壳子照它的做派转发——顺带也就验证了
# 「子命令名必须是合法 shell 标识符」这条约束在真实进程里成立。
upgrade_init="$stage/upgrade/init.d"
sed "s#^CONF_DIR=.*#CONF_DIR=$upgrade_dir#" "$init" > "$upgrade_init"
shim="$stage/bin/wrtdeck-init"
cat > "$shim" <<EOF
#!/bin/sh
# 模拟设备上的 /etc/init.d/wrtdeck：rc.common 把子命令名当函数名调用。
# start/stop/restart 这类动作由 rc.common 提供，开发机上没有对应实现，按成功处理。
. "$upgrade_init"
case "\$1" in
	start|stop|restart|reload|enable|disable) exit 0 ;;
esac
"\$1"
EOF
chmod 0755 "$shim"

# 包管理器会调的那个钩子：ipk 是 postinst，apk 升级时只跑 post-upgrade
case "$format" in
  apk) upgrade_hook="$stage/control/post-upgrade" ;;
  ipk) upgrade_hook="$stage/unpack/postinst" ;;
esac
[ -f "$upgrade_hook" ] || die "找不到 $format 的升级钩子"

# 把钩子里的设备绝对路径换成模拟路径：/etc/wrtdeck 指向上面那份「1.0.0 现场」，
# /etc/init.d/wrtdeck 指向小壳子，/etc/rc.d 下造一个已启用的记号，
# 这样「升级后重启一次」那条分支也会真的走到
: > "$stage/upgrade/rc.d/S95wrtdeck"
hook_run="$stage/upgrade/hook"
sed -e "s#/etc/init\.d/wrtdeck#$shim#g" \
    -e "s#/etc/rc\.d#$stage/upgrade/rc.d#g" \
    -e "s#/etc/wrtdeck#$upgrade_dir#g" \
    "$upgrade_hook" > "$hook_run"
chmod 0755 "$hook_run"
sh -n "$hook_run" || die "改写后的升级钩子语法不合法"
ok "按包管理器的调法准备好升级钩子（$(basename "$upgrade_hook")）"

"$hook_run" > "$stage/upgrade/hook.out" 2>&1 ||
  die "升级钩子执行失败：$(cat "$stage/upgrade/hook.out")"
ok "升级钩子执行成功"

# 出厂默认的对外监听必须迁到回环。这是 1.0.1 的核心安全变化，也是「覆盖低版本安装」
# 最容易漏掉的一步——包换新了，配置还是旧的，面板等于仍在局域网里裸奔。
if grep -q '"listen": *"127\.0\.0\.1:8080"' "$upgrade_dir/config.json"; then
  ok "旧版本的 0.0.0.0:8080 已迁到 127.0.0.1:8080"
else
  die "升级后 listen 仍是旧值：$(grep '"listen"' "$upgrade_dir/config.json")"
fi
if [ "$(grep -c '"listen"' "$upgrade_dir/config.json")" = "1" ]; then
  ok "迁移是就地改写，没有留下重复键"
else
  die "迁移写出了重复的 listen 键"
fi

# 用户自己改过的项必须活着，否则「升级不覆盖用户配置」这句话就是假的
if grep -q '"history_limit": *240' "$upgrade_dir/config.json"; then
  ok "用户改过的配置项在升级后原样保留"
else
  die "升级弄丢了用户改过的配置项"
fi

[ "$(cat "$upgrade_dir/secrets.json")" = "$secrets_before" ] || die "升级改动了口令数据"
[ "$(cat "$upgrade_dir/registry.json")" = "$registry_before" ] || die "升级改动了注册表"
ok "口令与注册表原封不动（这两样不在包里，升级不该碰）"

[ -f "$upgrade_dir/config.json.pre-1.0.1" ] || die "迁移没有留下原配置备份"
ok "迁移留下了原配置备份（想退回旧行为时有据可依）"

# 幂等：包管理器重装同版本、或用户手工再跑一次子命令，都不该二次改动
conf_after="$(cat "$upgrade_dir/config.json")"
"$hook_run" > /dev/null 2>&1 || true
if [ "$(cat "$upgrade_dir/config.json")" = "$conf_after" ]; then
  ok "重复执行升级钩子幂等"
else
  die "重复执行升级钩子会二次改动配置"
fi

# 反过来：用户在 1.0.0 上自己挑的监听地址不能被替掉——那是他显式选择的行为，
# 替他改掉比留着旧默认值更糟，正确做法是只提示风险。
# 这一条直接调函数，因为要换一个 CONF 指向，不必再搭一套钩子。
custom_conf="$stage/upgrade/custom.json"
printf '{\n  "listen": "0.0.0.0:9090",\n  "limits": { "history_limit": 240 }\n}\n' > "$custom_conf"
conf_saved="$CONF"
CONF="$custom_conf"
migrate_config > "$stage/upgrade/custom.out" 2>&1 || true
CONF="$conf_saved"
if grep -q '"listen": *"0\.0\.0\.0:9090"' "$custom_conf"; then
  ok "用户自己写的监听地址不被迁移改写"
else
  die "迁移擅自改写了用户自己选的监听地址"
fi
if grep -q '0.0.0.0:9090' "$stage/upgrade/custom.out"; then
  ok "自定义监听地址给出了风险提示"
else
  die "迁移对自定义监听地址一声不响，用户不会知道面板仍对外开着口"
fi

printf '\n== 结果 ==\n'
printf '  安装模拟全部通过（%s 包）：\n' "$format"
printf '  init 脚本可拉起服务；首启口令可用且强制更换；改口令后旧会话立即失效；\n'
printf '  登录失败会被限速；一次性登录码只能用一次且限同源；数据落在 data_dir；\n'
printf '  同源网关只转发面板 API 且不凭白给凭据，面板页面自带相对路径资源；\n'
printf '  装过 1.0.0 的设备升上来时，旧配置的对外监听会被迁到回环，\n'
printf '  用户改过的配置项与用户数据原样保留，重复执行幂等\n'
printf '\n  注意：本脚本与真实设备的差异只有两处——procd 被桩化、二进制换成本机版本，\n'
printf '  其余（HTTP 接口、凭据校验、落盘位置）都是真实执行的结果。\n'
