#!/bin/sh
# 校验 apk 包的内部结构。分两层：
#   1. 用系统自带的 gzip 与 tar 把整包当一条连续 tar 流读一遍——
#      这是 apk 自己的读法，能完整列出条目就说明段边界与结束块位置都对；
#   2. 交给 check_apk.py 做纯 shell 拿不到的断言：gzip 成员切分、签名、逐文件校验和。
#
# 用法：sh scripts/check-apk.sh [--preset=wrtdeck|luci] [apk 路径]
#       不传路径时自动取 dist/ 下对应预设的最新包

set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
preset=wrtdeck
apk=""

for arg in "$@"; do
  case "$arg" in
    --preset=*) preset="${arg#--preset=}" ;;
    *) apk="$arg" ;;
  esac
done

case "$preset" in
  wrtdeck) glob='dist/wrtdeck-*.apk' ;;
  luci)   glob='dist/luci-app-wrtdeck-*.apk' ;;
  *) echo "未知的预设：$preset" >&2; exit 1 ;;
esac

if [ -z "$apk" ]; then
  apk="$(ls -1 "$root"/$glob 2>/dev/null | head -1)"
fi
if [ -z "$apk" ] || [ ! -f "$apk" ]; then
  echo "找不到 apk，请先执行：make apk" >&2
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

echo "被测文件: $apk"

# ── 第一层：真实解析器交叉验证 ─────────────────────────────────────────────
printf '\n== 0. 用系统 gzip + tar 复读整包 ==\n'
if gzip -dc "$apk" > "$work/stream.tar" 2>/dev/null; then
  ok "gzip 能解出全部成员（多成员拼接流完整）"
else
  ng "gzip 无法解出全部成员"
fi

if tar -tvf "$work/stream.tar" > "$work/listing.txt" 2>"$work/tar.err"; then
  ok "tar 能把整包读成一条连续归档"
else
  ng "tar 读取失败：$(head -3 "$work/tar.err")"
fi

entries="$(wc -l < "$work/listing.txt" | tr -d ' ')"
if [ "$entries" -gt 0 ]; then
  ok "列出 $entries 个条目"
else
  ng "没有列出任何条目"
fi

if grep -q '\.PKGINFO' "$work/listing.txt"; then
  ok "整包中存在 .PKGINFO"
else
  ng "整包中缺少 .PKGINFO"
fi

if grep -q '^[^ ]* *[0-9]* *root *root' "$work/listing.txt"; then
  ok "tar 视角下条目属主为 root"
else
  ng "tar 视角下存在非 root 属主"
fi

# 真实解析器认可之后，再让 python 做结构断言
printf '\n== 结构断言（scripts/check_apk.py） ==\n'
counts="$work/counts"
set +e
APK_CHECK_WORK="$work/py" "$(command -v python3 || echo /usr/bin/python3)" \
  "$root/scripts/check_apk.py" "$apk" --preset "$preset" \
  --pubkey "$root/dist/wrtdeck-local.rsa.pub" --counts-out "$counts"
py_status=$?
set -e

if [ -f "$counts" ]; then
  py_pass="$(awk '{ print $1 }' "$counts")"
  py_fail="$(awk '{ print $2 }' "$counts")"
  pass=$((pass + py_pass))
  fail=$((fail + py_fail))
elif [ "$py_status" -ne 0 ]; then
  ng "结构断言脚本异常退出（退出码 $py_status）"
fi

# ── 薄壳：解出文件树，做与包格式无关的源码级断言 ────────────────────────────
# 结构校验只说明这个包装得上，说明不了它装上去能不能用。薄壳与面板前端
# 分属两个包、两份代码，靠菜单路径、消息名与 Web 根目录约定对接，
# 因此这一层与 check-ipk.sh 共用同一份断言（scripts/check_luci_src.sh）。
if [ "$preset" = luci ]; then
  printf '\n== 源码级断言（scripts/check_luci_src.sh） ==\n'
  tree="$work/apk"
  if "$(command -v python3 || echo /usr/bin/python3)" "$root/scripts/apk_unpack.py" \
      --in "$apk" --out "$tree" --quiet >/dev/null 2>&1; then
    # 权限从重放出来的整包 tar 流里读，apk 数据段的成员名没有 ipk 那样的 ./ 前缀
    assert_file() {
      if [ -f "$tree/rootfs$1" ]; then ok "安装 $1"; else ng "缺少 $1"; fi
      _mode="$(tar tvf "$work/stream.tar" \
        | awk -v p="${1#/}" '{ n = $NF; sub(/^\.\//, "", n); if (n == p) { print $1; exit } }')"
      assert_eq "$_mode" "$2" "$1 权限为 $2"
    }
    data_dir="$tree/rootfs"
    # apk_unpack.py 把名字里的前导点去掉了，控制文件落地为 PKGINFO / post-install
    meta_file="$tree/control/PKGINFO"
    . "$root/scripts/check_luci_src.sh"
    check_luci_src
  else
    ng "无法解出包内文件树，跳过源码级断言"
  fi
fi

printf '\n== 结果 ==\n  通过 %s 项，失败 %s 项\n' "$pass" "$fail"
if [ "$fail" -gt 0 ]; then
  exit 1
fi

case "$preset" in
  wrtdeck) echo "  apk 结构校验通过，可执行 apk add --allow-untrusted 安装" ;;
  *)      echo "  apk 结构校验通过" ;;
esac
