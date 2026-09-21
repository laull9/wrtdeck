#!/bin/sh
# 校验 ipk 的内部结构与关键内容，逐条对应 opkg 安装时的检查点。
# 在没有 OpenWrt 设备与 SDK 的开发机上也能跑，是打包测试的核心断言。
#
# 用法：sh scripts/check-ipk.sh [ipk 路径]
#       不传参数时自动取 dist/ 下最新的 owdash_*.ipk

set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
ipk="${1:-}"
if [ -z "$ipk" ]; then
  ipk="$(ls -1 "$root"/dist/owdash_*.ipk 2>/dev/null | head -1)"
fi
if [ -z "$ipk" ] || [ ! -f "$ipk" ]; then
  echo "找不到 ipk，请先执行：make ipk" >&2
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

step() { printf '\n== %s ==\n' "$1"; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT INT TERM

echo "被测文件: $ipk"

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

assert_eq "$(field Package)" 'owdash' "包名为 owdash"
if field Depends | grep -q 'ca-bundle'; then ok "依赖声明含 ca-bundle（TLS 需要）"; else ng "依赖未声明 ca-bundle"; fi

# ── 3. 安装钩子与配置文件声明 ──────────────────────────────────────────────
step "3. 安装钩子与 conffiles"
assert "postinst 存在" test -f "$work/control/postinst"
assert "prerm 存在" test -f "$work/control/prerm"
assert "postinst 语法合法" sh -n "$work/control/postinst"
assert "prerm 语法合法" sh -n "$work/control/prerm"
if grep -q '^/etc/owdash/config\.json$' "$work/control/conffiles"; then
  ok "conffiles 声明 /etc/owdash/config.json（升级不覆盖用户配置）"
else
  ng "conffiles 未声明 /etc/owdash/config.json"
fi

# ── 4. 数据部分内容 ────────────────────────────────────────────────────────
step "4. 数据部分"
mkdir -p "$work/data"
( cd "$work/data" && tar xzf "$work/data.tar.gz" )

assert "安装 /usr/bin/owdash" test -f "$work/data/usr/bin/owdash"
assert "安装 /etc/init.d/owdash" test -f "$work/data/etc/init.d/owdash"
assert "安装 /etc/owdash/config.json" test -f "$work/data/etc/owdash/config.json"

modes="$(tar tvzf "$work/data.tar.gz" | awk '{ print $1, $NF }')"
if printf '%s\n' "$modes" | grep -q '^-rwxr-xr-x .*usr/bin/owdash$'; then
  ok "owdash 权限为 0755"
else
  ng "owdash 权限不是 0755"
fi
if printf '%s\n' "$modes" | grep -q '^-rwxr-xr-x .*etc/init.d/owdash$'; then
  ok "init 脚本权限为 0755"
else
  ng "init 脚本权限不是 0755"
fi
if printf '%s\n' "$modes" | grep -q '^-rw-r--r-- .*etc/owdash/config.json$'; then
  ok "config.json 权限为 0644"
else
  ng "config.json 权限不是 0644"
fi
bad_owner="$(tar tvzf "$work/data.tar.gz" | awk '$3 != "root" || $4 != "root" { print $3 ":" $4 " " $NF }')"
if [ -z "$bad_owner" ]; then
  ok "数据部分属主统一为 root:root"
else
  ng "存在非 root:root 属主：$bad_owner"
fi

# ── 5. 二进制可运行性 ──────────────────────────────────────────────────────
step "5. 二进制属性"
elf="$work/data/usr/bin/owdash"
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

arch_field="$(field Architecture)"
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

# ── 6. init 脚本契约 ───────────────────────────────────────────────────────
step "6. procd init 脚本"
init="$work/data/etc/init.d/owdash"
assert "语法合法" sh -n "$init"
for token in 'USE_PROCD=1' 'procd_open_instance' 'procd_set_param command' \
             'procd_set_param respawn' 'procd_close_instance' 'start_service' 'stop_service'; do
  if grep -q "$token" "$init"; then ok "含 $token"; else ng "缺少 $token"; fi
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

# ── 7. 默认配置 ────────────────────────────────────────────────────────────
step "7. 默认配置"
conf="$work/data/etc/owdash/config.json"
assert "JSON 语法合法" python3 -c "import json,sys; json.load(open(sys.argv[1]))" "$conf"
if grep -q '"listen"' "$conf"; then ok "含 listen 字段"; else ng "缺少 listen 字段"; fi
if grep -q '"data_dir": *"/etc/owdash"' "$conf"; then
  ok "data_dir 指向 /etc/owdash（与 init 一致）"
else
  ng "data_dir 与 init 脚本不一致"
fi

# ── 汇总 ───────────────────────────────────────────────────────────────────
printf '\n== 结果 ==\n  通过 %s 项，失败 %s 项\n' "$pass" "$fail"
if [ "$fail" -gt 0 ]; then
  exit 1
fi
echo "  ipk 结构校验通过，可执行 opkg install 安装"
