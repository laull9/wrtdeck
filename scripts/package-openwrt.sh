#!/usr/bin/env bash
# 把交叉编译产物与 procd 脚本打包成 OpenWrt 可安装的 ipk
#
# ipk 本体就是 ar 归档（debian-binary + control.tar.gz + data.tar.gz），
# 因此不依赖 OpenWrt SDK，用系统自带的 ar/tar 就能在开发机上产出真机可装的包。
# 需要走 SDK 内构建时用 packaging/openwrt/Makefile。
#
# 用法：
#   sh scripts/package-openwrt.sh                      # 默认 GOARCH=arm64
#   GOARCH=arm64 PKG_ARCH=aarch64_cortex-a53 sh scripts/package-openwrt.sh
#   VERSION=1.2.0 PKG_RELEASE=2 sh scripts/package-openwrt.sh

set -eu

. "$(dirname "$0")/_common.sh"

root="$(project_root)"
go_bin="$(find_go)"
version="${VERSION:-1.0.0}"
release="${PKG_RELEASE:-1}"
goarch="${GOARCH:-arm64}"

# Go 目标平台与 OpenWrt 架构名的对应，PKG_ARCH 显式给出时不走这张表
if [ -z "${PKG_ARCH:-}" ]; then
  case "$goarch" in
    arm64)  PKG_ARCH="aarch64_cortex-a53" ;;
    arm)    PKG_ARCH="arm_cortex-a7" ;;
    amd64)  PKG_ARCH="x86_64" ;;
    mipsle) PKG_ARCH="mipsel_24kc" ;;
    mips)   PKG_ARCH="mips_24kc" ;;
    *)
      echo "未知的 GOARCH=$goarch，请显式设置 PKG_ARCH" >&2
      exit 1
      ;;
  esac
fi

# 打包目录为 tar.gz，属主统一写成 root:root，
# 否则 macOS 上 gid 0 会解析成 wheel，设备端解包后属主错误。
tar_gz() {
  _src="$1"
  _out="$2"
  if tar --version 2>/dev/null | grep -qi 'gnu'; then
    ( cd "$_src" && tar --owner=root:0 --group=root:0 -czf "$_out" . )
  else
    ( cd "$_src" && tar --uid 0 --gid 0 --uname root --gname root -czf "$_out" . )
  fi
}

build_web package

cd "$root"
mkdir -p dist
bin="$root/dist/owdash-linux-${goarch}"
log_info package "交叉编译 linux/${goarch}"
CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" "$go_bin" build \
  -trimpath \
  -ldflags="-s -w -buildid= -X main.version=${version}" \
  -o "$bin" \
  ./cmd/owdash

# 校验产物确实是静态 ELF：动态链接的二进制在精简版 OpenWrt 上没有 libc 可用
if ! file "$bin" | grep -q 'statically linked'; then
  echo "产物不是静态链接，无法在 OpenWrt 上运行：" >&2
  file "$bin" >&2
  exit 1
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# ── 数据部分：/usr/bin + /etc/init.d + /etc/owdash ──────────────────────────
pkgdir="$work/data"
mkdir -p "$pkgdir/usr/bin" "$pkgdir/etc/init.d" "$pkgdir/etc/owdash"
install -m 0755 "$bin" "$pkgdir/usr/bin/owdash"
install -m 0755 "$root/packaging/openwrt/owdash.init" "$pkgdir/etc/init.d/owdash"
install -m 0644 "$root/packaging/openwrt/files/etc/owdash/config.json" \
  "$pkgdir/etc/owdash/config.json"

# ── 控制部分：元数据与安装钩子 ─────────────────────────────────────────────
ctrldir="$work/control"
mkdir -p "$ctrldir"
cp "$root/packaging/openwrt/control/conffiles" "$ctrldir/conffiles"
install -m 0755 "$root/packaging/openwrt/control/postinst" "$ctrldir/postinst"
install -m 0755 "$root/packaging/openwrt/control/prerm" "$ctrldir/prerm"

size_kb="$(du -sk "$pkgdir" | awk '{ print $1 }')"
cat > "$ctrldir/control" <<EOF
Package: owdash
Version: ${version}-${release}
Depends: ca-bundle
Section: utils
Priority: optional
Maintainer: WrtDeck
Architecture: ${PKG_ARCH}
Installed-Size: ${size_kb}
Description: WrtDeck 轻量设备控制面板
 通过注册表把 HTTP/TCP/UDP/MQTT 设备状态与控制命令动态变成面板卡片与操作按钮。
 前端资源已通过 embed.FS 编入二进制，运行时仅依赖设备自身，无额外运行时依赖。
EOF

printf '2.0\n' > "$work/debian-binary"
tar_gz "$ctrldir" "$work/control.tar.gz"
tar_gz "$pkgdir" "$work/data.tar.gz"

ipk="$root/dist/owdash_${version}-${release}_${PKG_ARCH}.ipk"
rm -f "$ipk"
# BSD ar 默认会往归档里塞 __.SYMDEF 符号表，-S 可关掉；
# 个别平台不认 -S，退回默认行为后再手动摘掉符号表。
if ! ( cd "$work" && ar rcS "$ipk" debian-binary control.tar.gz data.tar.gz ) 2>/dev/null; then
  ( cd "$work" && ar rc "$ipk" debian-binary control.tar.gz data.tar.gz )
  ( cd "$work" && ar d "$ipk" __.SYMDEF ) 2>/dev/null || true
fi

members="$(ar t "$ipk" | tr '\n' ' ' | sed 's/ *$//')"
if [ "$members" != 'debian-binary control.tar.gz data.tar.gz' ]; then
  echo "ipk 成员异常：$members" >&2
  exit 1
fi

log_info package "ipk 已生成 $ipk"
log_info package "结构 $members"
report_artifact package "$ipk"
