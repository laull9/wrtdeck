#!/usr/bin/env bash
# 把交叉编译产物与 procd 脚本打包成 OpenWrt 包。
#
# 默认产出 apk：OpenWrt 25.12 起 apk-tools v3 取代 opkg，官方仓库索引也已改成 packages.adb。
# ipk 仍然保留，供 24.10 及更早版本使用；两种格式共用同一份 payload，
# 因此不会出现「apk 里有的文件 ipk 里没有」这类偏差。
#
# 用法：
#   sh scripts/package-openwrt.sh                      # 默认只出 apk
#   PKG_FORMAT=ipk sh scripts/package-openwrt.sh       # 只出 ipk
#   PKG_FORMAT=both sh scripts/package-openwrt.sh      # 两种都出
#   GOARCH=arm64 PKG_ARCH=aarch64_cortex-a53 sh scripts/package-openwrt.sh
#   VERSION=1.2.0 PKG_RELEASE=2 sh scripts/package-openwrt.sh
#   APK_SIGN=0 sh scripts/package-openwrt.sh           # 出未签名 apk，安装需 --allow-untrusted

set -eu

. "$(dirname "$0")/_common.sh"
. "$(dirname "$0")/pack.sh"

root="$(project_root)"
go_bin="$(find_go)"
version="${VERSION:-1.0.0}"
goarch="${GOARCH:-arm64}"
format="${PKG_FORMAT:-apk}"

# Go 目标平台与 OpenWrt 架构名的对应，PKG_ARCH 显式给出时不走这张表
if [ -z "${PKG_ARCH:-}" ]; then
  case "$goarch" in
    arm64)  PKG_ARCH="aarch64_cortex-a53" ;;
    arm)    PKG_ARCH="arm_cortex-a7" ;;
    amd64)  PKG_ARCH="x86_64" ;;
    mipsle) PKG_ARCH="mipsel_24kc" ;;
    mips)   PKG_ARCH="mips_24kc" ;;
    *)
      echo "未知的 GOARCH=${goarch}，请显式设置 PKG_ARCH" >&2
      exit 1
      ;;
  esac
fi

if [ "${SKIP_WEB:-0}" != "1" ]; then
  build_web package
fi

cd "$root"
mkdir -p dist
bin="$root/dist/wrtdeck-linux-${goarch}"
log_info package "交叉编译 linux/${goarch}"
CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" "$go_bin" build \
  -trimpath \
  -ldflags="-s -w -buildid= -X main.version=${version}" \
  -o "$bin" \
  ./cmd/wrtdeck

# 校验产物确实是静态 ELF：动态链接的二进制在精简版 OpenWrt 上没有 libc 可用
if ! file "$bin" | grep -q 'statically linked'; then
  echo "产物不是静态链接，无法在 OpenWrt 上运行：" >&2
  file "$bin" >&2
  exit 1
fi

# ── payload：apk 与 ipk 共用这一份文件树 ───────────────────────────────────
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

PKG_ROOT="$work/rootfs"
mkdir -p "$PKG_ROOT/usr/bin" "$PKG_ROOT/etc/init.d" "$PKG_ROOT/etc/wrtdeck"
install -m 0755 "$bin" "$PKG_ROOT/usr/bin/wrtdeck"
install -m 0755 "$root/packaging/openwrt/wrtdeck.init" "$PKG_ROOT/etc/init.d/wrtdeck"
# 配置文件在设备上由 apk 的 protected_paths 与 ipk 的 conffiles 保护，升级都不覆盖用户改动
install -m 0644 "$root/packaging/openwrt/files/etc/wrtdeck/config.json" \
  "$PKG_ROOT/etc/wrtdeck/config.json"

# apk 的安装钩子：脚本名对应 abuild 约定的 .post-install 等条目
PKG_APK_SCRIPTS="post-install=$root/packaging/openwrt/apk/post-install"
PKG_APK_SCRIPTS="$PKG_APK_SCRIPTS post-upgrade=$root/packaging/openwrt/apk/post-upgrade"
PKG_APK_SCRIPTS="$PKG_APK_SCRIPTS pre-deinstall=$root/packaging/openwrt/apk/pre-deinstall"

# 元数据：两种格式共用，改一处即可
PKG_NAME=wrtdeck
PKG_VERSION="$version"
PKG_RELEASE="${PKG_RELEASE:-1}"
PKG_DESC="WrtDeck 轻量设备控制面板"
PKG_DESC_LONG="通过注册表把 HTTP/TCP/UDP/MQTT 设备状态与控制命令动态变成面板卡片与操作按钮。
前端资源已通过 embed.FS 编入二进制，运行时仅依赖设备自身，无额外运行时依赖。"
PKG_DEPS="ca-bundle"
PKG_LICENSE="MIT"
PKG_IPK_HOOKS="$root/packaging/openwrt/control"
PKG_IPK_CONFFILES="/etc/wrtdeck/config.json"

case "$format" in
  apk)
    pack_apk "$root/dist/${PKG_NAME}-${PKG_VERSION}-r${PKG_RELEASE}.apk"
    ;;
  ipk)
    pack_ipk "$root/dist/${PKG_NAME}_${PKG_VERSION}-${PKG_RELEASE}_${PKG_ARCH}.ipk"
    ;;
  both)
    pack_apk "$root/dist/${PKG_NAME}-${PKG_VERSION}-r${PKG_RELEASE}.apk"
    pack_ipk "$root/dist/${PKG_NAME}_${PKG_VERSION}-${PKG_RELEASE}_${PKG_ARCH}.ipk"
    ;;
  *)
    echo "未知的 PKG_FORMAT=${format}，可选 apk / ipk / both" >&2
    exit 1
    ;;
esac

log_info package "打包完成（格式 ${format}，架构 ${PKG_ARCH}）"
