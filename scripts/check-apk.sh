#!/bin/sh
# 校验 apk 包的内部结构。分两层：
#   1. 用系统自带的 gzip 与 tar 把整包当一条连续 tar 流读一遍——
#      这是 apk 自己的读法，能完整列出条目就说明段边界与结束块位置都对；
#   2. 交给 check_apk.py 做纯 shell 拿不到的断言：gzip 成员切分、签名、逐文件校验和。
#
# 用法：sh scripts/check-apk.sh [--preset=owdash|luci] [apk 路径]
#       不传路径时自动取 dist/ 下对应预设的最新包

set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
preset=owdash
apk=""

for arg in "$@"; do
  case "$arg" in
    --preset=*) preset="${arg#--preset=}" ;;
    *) apk="$arg" ;;
  esac
done

case "$preset" in
  owdash) glob='dist/owdash-*.apk' ;;
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
  --pubkey "$root/dist/owdash-local.rsa.pub" --counts-out "$counts"
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

printf '\n== 结果 ==\n  通过 %s 项，失败 %s 项\n' "$pass" "$fail"
if [ "$fail" -gt 0 ]; then
  exit 1
fi

case "$preset" in
  owdash) echo "  apk 结构校验通过，可执行 apk add --allow-untrusted 安装" ;;
  *)      echo "  apk 结构校验通过" ;;
esac
