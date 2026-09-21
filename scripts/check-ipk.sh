#!/bin/sh
# 校验 ipk 的内部结构与关键内容，逐条对应 opkg 安装时的检查点。
# 在没有 OpenWrt 设备与 SDK 的开发机上也能跑，是打包测试的核心断言。
#
# 两种预设：
#   wrtdeck 面板本体：二进制、init 脚本、默认配置
#   luci   薄壳：menu.d / acl.d / rpcd 后端 / LuCI 视图，以及与面板前端的交接契约
#
# 用法：sh scripts/check-ipk.sh [ipk 路径]
#       sh scripts/check-ipk.sh --preset=luci [ipk 路径]
#       不传路径时自动取 dist/ 下对应预设的最新包

set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
preset=wrtdeck
ipk=""

for arg in "$@"; do
  case "$arg" in
    --preset=*) preset="${arg#--preset=}" ;;
    *) ipk="$arg" ;;
  esac
done

case "$preset" in
  wrtdeck)
    glob='dist/wrtdeck_*.ipk'
    package_name=wrtdeck
    ;;
  luci)
    glob='dist/luci-app-wrtdeck_*.ipk'
    package_name=luci-app-wrtdeck
    ;;
  *)
    echo "未知的预设：$preset" >&2
    exit 1
    ;;
esac

if [ -z "$ipk" ]; then
  ipk="$(ls -1 "$root"/$glob 2>/dev/null | head -1)"
fi
if [ -z "$ipk" ] || [ ! -f "$ipk" ]; then
  echo "找不到 ipk，请先执行：make ipk / make luci" >&2
  exit 1
fi

pass=0
fail=0

ok() { printf '  [ok]   %s\n' "$1"; pass=$((pass + 1)); }
ng() { printf '  [FAIL] %s\n' "$1"; fail=$((fail + 1)); }

# 断言：描述 + 命令，命令成功即通过
assert() {
  _desc="$1"
  shift
  if "$@" >/dev/null 2>&1; then ok "$_desc"; else ng "$_desc"; fi
}

# 断言：描述 + 实际值 + 期望值
assert_eq() {
  if [ "$1" = "$2" ]; then ok "$3"; else ng "$3（实际 '$1'，期望 '$2'）"; fi
}

# 断言：文件里出现某个字符串
assert_has() {
  if grep -q "$2" "$1"; then ok "$3"; else ng "$3"; fi
}

step() { printf '\n== %s ==\n' "$1"; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT INT TERM

echo "被测文件: $ipk  （预设 $preset）"

# ── 1. ar 归档结构 ─────────────────────────────────────────────────────────
step "1. 归档结构"
assert_eq "$(head -c 7 "$ipk")" '!<arch>' "文件是 ar 归档（ipk 容器格式）"

members="$(ar t "$ipk" | tr '\n' ' ' | sed 's/ *$//')"
assert_eq "$members" 'debian-binary control.tar.gz data.tar.gz' "成员与顺序为 debian-binary / control.tar.gz / data.tar.gz"
assert_eq "$(ar p "$ipk" debian-binary)" '2.0' "debian-binary 内容为 2.0"

( cd "$work" && ar x "$ipk" )
assert "control.tar.gz 与 data.tar.gz 可解压" tar tzf "$work/control.tar.gz"
assert "data.tar.gz 可解压" tar tzf "$work/data.tar.gz"

# ── 2. control 元数据 ──────────────────────────────────────────────────────
step "2. control 元数据"
mkdir -p "$work/control"
( cd "$work/control" && tar xzf "$work/control.tar.gz" )
ctl="$work/control/control"
assert "存在 control 文件" test -f "$ctl"

field() { sed -n "s/^$1: *//p" "$ctl" | head -1; }
arch_field="$(field Architecture)"

# 架构名里本身含下划线（aarch64_cortex-a53），不能按最后一个下划线反推，
# 因此改为校验「文件名确实以 _架构.ipk 结尾」。
case "$(basename "$ipk")" in
  *"_${arch_field}.ipk") ok "文件名以 _${arch_field}.ipk 结尾" ;;
  *) ng "文件名与 control 中的架构 $arch_field 不一致" ;;
esac

for f in Package Version Architecture Description Maintainer; do
  if [ -n "$(field "$f")" ]; then ok "control 含字段 $f = $(field "$f")"; else ng "control 缺少字段 $f"; fi
done

assert_eq "$(field Package)" "$package_name" "包名为 $package_name"

# ── 3. 数据部分内容 ────────────────────────────────────────────────────────
step "3. 数据部分"
mkdir -p "$work/data"
( cd "$work/data" && tar xzf "$work/data.tar.gz" )

# 断言：文件在包内且权限符合预期
assert_file() {
  if [ -f "$work/data$1" ]; then ok "安装 $1"; else ng "缺少 $1"; fi
  # ipk 数据段的成员名带 OpenWrt 惯例的 "./" 前缀（opkg-build 产出的包同样如此），
  # 因此比较前先剥掉它，否则永远匹配不到，断言会静默读到空串。
  # 用 awk 的 exit 提前结束而不是管道接 head，避免 SIGPIPE 在 set -e 下误伤脚本。
  _mode="$(tar tvzf "$work/data.tar.gz" \
    | awk -v p="${1#/}" '{ n = $NF; sub(/^\.\//, "", n); if (n == p) { print $1; exit } }')"
  assert_eq "$_mode" "$2" "$1 权限为 $2"
}

bad_owner="$(tar tvzf "$work/data.tar.gz" | awk '$3 != "root" || $4 != "root" { print $3 ":" $4 " " $NF }')"
if [ -z "$bad_owner" ]; then
  ok "数据部分属主统一为 root:root"
else
  ng "存在非 root:root 属主：$bad_owner"
fi

case "$preset" in
wrtdeck)
  assert_file /usr/bin/wrtdeck -rwxr-xr-x
  assert_file /etc/init.d/wrtdeck -rwxr-xr-x
  assert_file /etc/wrtdeck/config.json -rw-r--r--

  # ── 4. 安装钩子与配置文件声明 ────────────────────────────────────────────
  step "4. 安装钩子与 conffiles"
  assert "postinst 存在" test -f "$work/control/postinst"
  assert "prerm 存在" test -f "$work/control/prerm"
  assert "postinst 语法合法" sh -n "$work/control/postinst"
  assert "prerm 语法合法" sh -n "$work/control/prerm"
  if grep -q '^/etc/wrtdeck/config\.json$' "$work/control/conffiles"; then
    ok "conffiles 声明 /etc/wrtdeck/config.json（升级不覆盖用户配置）"
  else
    ng "conffiles 未声明 /etc/wrtdeck/config.json"
  fi
  if field Depends | grep -q 'ca-bundle'; then ok "依赖声明含 ca-bundle（TLS 需要）"; else ng "依赖未声明 ca-bundle"; fi

  # ── 5. 二进制可运行性 ────────────────────────────────────────────────────
  step "5. 二进制属性"
  elf="$work/data/usr/bin/wrtdeck"
  bin_info="$(file -b "$elf")"
  echo "  file: $bin_info"
  case "$bin_info" in
    *ELF*) ok "是 ELF 可执行文件" ;;
    *) ng "不是 ELF 可执行文件" ;;
  esac
  case "$bin_info" in
    *'statically linked'*) ok "静态链接（精简版 OpenWrt 无 libc 依赖）" ;;
    *) ng "非静态链接，设备上可能无法启动" ;;
  esac

  case "$arch_field" in
    aarch64*) exp_elf='ARM aarch64' ;;
    arm_*)    exp_elf='ARM' ;;
    x86_64)   exp_elf='x86-64' ;;
    mipsel*)  exp_elf='MIPS' ;;
    mips*)    exp_elf='MIPS' ;;
    *)        exp_elf='' ;;
  esac
  if [ -n "$exp_elf" ]; then
    case "$bin_info" in
      *"$exp_elf"*) ok "ELF 目标架构匹配 $arch_field" ;;
      *) ng "ELF 目标架构与 $arch_field 不匹配" ;;
    esac
  fi

  # ── 6. init 脚本契约 ─────────────────────────────────────────────────────
  step "6. procd init 脚本"
  init="$work/data/etc/init.d/wrtdeck"
  assert "语法合法" sh -n "$init"
  for token in 'USE_PROCD=1' 'procd_open_instance' 'procd_set_param command' \
               'procd_set_param respawn' 'procd_close_instance' 'start_service' 'stop_service'; do
    assert_has "$init" "$token" "含 $token"
  done
  # init 脚本用变量拼装路径，这里把变量取出来核对包内是否真有对应文件
  prog_path="$(sed -n 's/^PROG=//p' "$init" | head -1)"
  conf_dir="$(sed -n 's/^CONF_DIR=//p' "$init" | head -1)"
  if [ -n "$prog_path" ] && [ -x "$work/data$prog_path" ]; then
    ok "启动命令 $prog_path 在包内存在且可执行"
  else
    ng "启动命令路径 $prog_path 与包内容不一致"
  fi
  if [ -n "$conf_dir" ] && [ -f "$work/data$conf_dir/config.json" ]; then
    ok "配置目录 $conf_dir 与包内配置文件一致"
  else
    ng "配置目录 $conf_dir 与包内容不一致"
  fi

  # ── 7. 默认配置 ──────────────────────────────────────────────────────────
  step "7. 默认配置"
  conf="$work/data/etc/wrtdeck/config.json"
  assert "JSON 语法合法" python3 -c "import json,sys; json.load(open(sys.argv[1]))" "$conf"
  assert_has "$conf" '"listen"' "含 listen 字段"
  if grep -q '"data_dir": *"/etc/wrtdeck"' "$conf"; then
    ok "data_dir 指向 /etc/wrtdeck（与 init 一致）"
  else
    ng "data_dir 与 init 脚本不一致"
  fi
  ;;
luci)
  assert_file /usr/share/luci/menu.d/luci-app-wrtdeck.json -rw-r--r--
  assert_file /usr/share/rpcd/acl.d/luci-app-wrtdeck.json -rw-r--r--
  assert_file /usr/libexec/rpcd/wrtdeck -rwxr-xr-x
  assert_file /www/luci-static/resources/view/wrtdeck/panel.js -rw-r--r--

  # ── 4. 依赖与安装钩子 ────────────────────────────────────────────────────
  step "4. 依赖与安装钩子"
  for dep in luci-base wrtdeck; do
    if field Depends | grep -q "$dep"; then ok "依赖声明含 $dep"; else ng "依赖未声明 $dep"; fi
  done
  # rpcd 只在启动时读 ACL 与后端脚本，装完不重启就会出现「能点进去、一调就报权限不足」
  assert "postinst 存在" test -f "$work/control/postinst"
  assert "postinst 语法合法" sh -n "$work/control/postinst"
  assert_has "$work/control/postinst" '/etc/init.d/rpcd restart' "postinst 重启 rpcd 让新 ACL 生效"

  # ── 5. rpcd 后端 ─────────────────────────────────────────────────────────
  step "5. rpcd 后端"
  rpcd="$work/data/usr/libexec/rpcd/wrtdeck"
  assert "语法合法" sh -n "$rpcd"
  assert_has "$rpcd" 'jshn.sh' "引入 jshn，用 json_* 组装 ubus 返回值"
  for method in status handoff control; do
    assert_has "$rpcd" "$method)" "实现 $method 方法"
  done
  # rpcd 的后端与 ACL 是两份文件，方法名漂移的后果是「方法存在但被 ACL 拒掉」，很难查
  acl="$work/data/usr/share/rpcd/acl.d/luci-app-wrtdeck.json"
  for method in status handoff control; do
    assert_has "$acl" "\"$method\"" "ACL 放行 $method"
  done

  # ── 6. LuCI 菜单与视图 ───────────────────────────────────────────────────
  step "6. LuCI 菜单与视图"
  menu="$work/data/usr/share/luci/menu.d/luci-app-wrtdeck.json"
  assert_has "$menu" 'admin/services/wrtdeck' "菜单挂在 admin/services/wrtdeck"
  assert_has "$menu" 'wrtdeck/panel' "菜单指向视图 wrtdeck/panel"
  assert_has "$menu" 'luci-app-wrtdeck' "菜单依赖本包的 ACL"
  panel="$work/data/www/luci-static/resources/view/wrtdeck/panel.js"
  for token in "'require view'" 'rpc.declare' 'postMessage' 'view.extend' 'iframe'; do
    assert_has "$panel" "$token" "视图含 $token"
  done
  if grep -q 'panel' "$menu"; then ok "菜单路径与视图文件同名"; else ng "菜单路径与视图文件对不上"; fi

  # ── 7. 与面板前端的交接契约 ──────────────────────────────────────────────
  # 薄壳与前端是两个仓库内的两份代码，靠消息名与接口路径约定对接。
  # 一旦改名而另一侧没跟着改，现象是「静默地要用户手输口令」，因此在这里卡住。
  step "7. 与面板前端的交接契约"
  session="$root/web/src/lib/handoff.ts"
  server="$root/internal/api/server.go"
  if [ -f "$session" ]; then
    for message in wrtdeck.handoff wrtdeck.ready wrtdeck.accepted; do
      if grep -q "$message" "$panel" && grep -q "$message" "$session"; then
        ok "消息名 $message 在薄壳与前端两侧一致"
      else
        ng "消息名 $message 两侧不一致（薄壳/前端至少一侧缺失）"
      fi
    done
  else
    ng "找不到 $session，无法核对交接契约"
  fi
  if [ -f "$server" ] && grep -q '/api/v1/session/handoff' "$rpcd" && grep -q '/api/v1/session/handoff' "$server"; then
    ok "登录码接口 /api/v1/session/handoff 在 rpcd 与服务端两侧一致"
  else
    ng "登录码接口路径在 rpcd 与服务端两侧不一致"
  fi
  # 交接只传一次性登录码：长期凭据出现在 postMessage 里是设计上要避免的事
  if grep -q 'token' "$panel"; then
    ng "薄壳的交接消息里出现了 token 字段（应当只传一次性登录码 code）"
  else
    ok "交接消息只携带一次性登录码，不含长期凭据"
  fi
  ;;
esac

# ── 汇总 ───────────────────────────────────────────────────────────────────
printf '\n== 结果 ==\n  通过 %s 项，失败 %s 项\n' "$pass" "$fail"
if [ "$fail" -gt 0 ]; then
  exit 1
fi
echo "  ipk 结构校验通过（$package_name，可执行 opkg install 安装）"
