#!/usr/bin/env bash
# 打 luci-app-wrtdeck 薄壳包（apk 与 ipk 两种格式共用同一份 payload）。
#
# 薄壳本身只有三件事：
#   1. 在 LuCI 的「服务」下加一个 WrtDeck 菜单项；
#   2. 给 rpcd 装一个后端（状态查询、一次性登录码、服务启停）；
#   3. 把面板嵌进 iframe，并用 postMessage 把登录码交给它。
# 面板本体（wrtdeck）是独立包，见 package-openwrt.sh；两者互不依赖对方的版本。
#
# 用法：
#   sh scripts/package-luci.sh                       # 默认只出 apk
#   PKG_FORMAT=ipk sh scripts/package-luci.sh        # 只出 ipk
#   PKG_FORMAT=both sh scripts/package-luci.sh       # 两种都出
#   VERSION=1.2.0 PKG_RELEASE=2 sh scripts/package-luci.sh

set -eu

. "$(dirname "$0")/_common.sh"
. "$(dirname "$0")/pack.sh"

root="$(project_root)"
version="${VERSION:-1.0.0}"
format="${PKG_FORMAT:-apk}"

# apk 只认 noarch，opkg 只认 all，两者都是「与 CPU 架构无关」的意思
apk_arch=noarch
ipk_arch=all

# payload：把仓库里的文件树搬到临时目录再赋权。
# 不直接打 packaging/luci/root 是因为 rpcd 后端必须以 0755 安装，
# 而 git 的 exec 位在 Windows 检出、解压分发等场景下会丢，显式赋权才可靠。
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

PKG_ROOT="$work/rootfs"
mkdir -p "$PKG_ROOT"
cp -R "$root/packaging/luci/root/." "$PKG_ROOT/"
find "$PKG_ROOT" -type d -exec chmod 0755 {} +
find "$PKG_ROOT" -type f ! -path '*/rpcd/wrtdeck' -exec chmod 0644 {} +
chmod 0755 "$PKG_ROOT/usr/libexec/rpcd/wrtdeck"

# 元数据：两种格式共用，改一处即可
PKG_NAME=luci-app-wrtdeck
PKG_VERSION="$version"
PKG_RELEASE="${PKG_RELEASE:-1}"
PKG_DESC="LuCI 里的 WrtDeck 入口：查看服务状态并免密进入面板"
PKG_DESC_LONG="在 LuCI 的「服务」菜单下增加 WrtDeck 项，把面板嵌进页面；
登录 LuCI 后进入面板无需再手工填写 API Token。
面板本体由 wrtdeck 包提供，本包只做入口与凭据交接。"
PKG_DEPS="luci-base wrtdeck"
PKG_LICENSE="MIT"
PKG_APK_SCRIPTS="post-install=$root/packaging/luci/hooks/post-install"
PKG_IPK_SCRIPTS="postinst=$root/packaging/luci/hooks/post-install"

case "$format" in
  apk)
    PKG_ARCH="$apk_arch"
    pack_apk "$root/dist/${PKG_NAME}-${PKG_VERSION}-r${PKG_RELEASE}.apk"
    ;;
  ipk)
    PKG_ARCH="$ipk_arch"
    pack_ipk "$root/dist/${PKG_NAME}_${PKG_VERSION}-${PKG_RELEASE}_${ipk_arch}.ipk"
    ;;
  both)
    PKG_ARCH="$apk_arch"
    pack_apk "$root/dist/${PKG_NAME}-${PKG_VERSION}-r${PKG_RELEASE}.apk"
    PKG_ARCH="$ipk_arch"
    pack_ipk "$root/dist/${PKG_NAME}_${PKG_VERSION}-${PKG_RELEASE}_${ipk_arch}.ipk"
    ;;
  *)
    echo "未知的 PKG_FORMAT=$format，可选 apk / ipk / both" >&2
    exit 1
    ;;
esac

log_info package "luci 薄壳打包完成（格式 $format）"
