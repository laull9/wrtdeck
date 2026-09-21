#!/usr/bin/env bash
# 打 luci-app-wrtdeck 薄壳包（apk 与 ipk 两种格式共用同一份 payload）。
#
# 薄壳本身只有四件事：
#   1. 在 LuCI 的「服务」下加一个 WrtDeck 菜单项；
#   2. 给 rpcd 装一个后端（状态查询、一次性登录码、服务启停）；
#   3. 加一个 CGI 入口（/www/cgi-bin/wrtdeck-api），把面板 API 转交给只监听回环的面板本体，
#      于是浏览器始终只与 LuCI 同一个源打交道，不再需要去猜设备的 IP 与端口；
#   4. 把面板页面用 iframe 嵌进 LuCI 页面，并用 postMessage 交接一次性登录码。
#
# 面板页面本身不放进这个包：它由面板本体在启动时导出到 /www/wrtdeck
# （wrtdeck -export-web），这样页面与二进制永远同版本，两个包可以独立升级。
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

# CGI 入口必须以 0755 安装，其余文件一律 0644
gateway_path=/www/cgi-bin/wrtdeck-api
rpcd_path=/usr/libexec/rpcd/wrtdeck

# payload：把仓库里的文件树搬到临时目录再赋权。
# 不直接打 packaging/luci/root 是因为这两个脚本必须以 0755 安装，
# 而 git 的 exec 位在 Windows 检出、解压分发等场景下会丢，显式赋权才可靠。
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

PKG_ROOT="$work/rootfs"
mkdir -p "$PKG_ROOT"
cp -R "$root/packaging/luci/root/." "$PKG_ROOT/"
find "$PKG_ROOT" -type d -exec chmod 0755 {} +
find "$PKG_ROOT" -type f ! -path "*$rpcd_path" ! -path "*$gateway_path" -exec chmod 0644 {} +
chmod 0755 "$PKG_ROOT$rpcd_path" "$PKG_ROOT$gateway_path"

# 元数据：两种格式共用，改一处即可
PKG_NAME=luci-app-wrtdeck
PKG_VERSION="$version"
PKG_RELEASE="${PKG_RELEASE:-1}"
PKG_DESC="LuCI 里的 WrtDeck 面板入口：把面板内嵌进 LuCI 页面并免密进入"
PKG_DESC_LONG="在 LuCI 的「服务」菜单下增加 WrtDeck 项，把面板原样嵌进页面。
面板页面与接口都走 LuCI 自己的那个源（同域名、同端口、同一套加密方式），
因此被反向代理到公网域名时也不需要浏览器去连设备的 8080 端口。
登录 LuCI 后进入面板无需再填写面板口令。
面板本体与页面资源由 wrtdeck 包提供，本包只做入口、同源网关与凭据交接。"
PKG_DEPS="luci-base wrtdeck"
PKG_LICENSE="MIT"
# apk 升级只跑 post-upgrade、不跑 post-install，两条路径都要接上：
# 从 1.0.0 升上来时，rpcd 重启、cgi_prefix 符号链接与 LuCI 菜单缓存清空全靠这个钩子，
# 漏接的现场是「菜单点得进去，里面是个空白框」
PKG_APK_SCRIPTS="post-install=$root/packaging/luci/hooks/post-install"
PKG_APK_SCRIPTS="$PKG_APK_SCRIPTS post-upgrade=$root/packaging/luci/hooks/post-install"
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
    echo "未知的 PKG_FORMAT=${format}，可选 apk / ipk / both" >&2
    exit 1
    ;;
esac

log_info package "luci 薄壳打包完成（格式 ${format}）"
