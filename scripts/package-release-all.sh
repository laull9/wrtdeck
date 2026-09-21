#!/usr/bin/env bash
# 一次性打出所有目标架构的发布资产：二进制、apk、ipk、luci 薄壳与 sha256 校验清单。
#
# 用法：
#   VERSION=1.0.3 sh scripts/package-release-all.sh
#   不传 VERSION 时自动从 Makefile 中提取。

set -eu

# 切换到脚本所在目录的上一级根目录
root="$(cd "$(dirname "$0")/.." && pwd)"
. "$root/scripts/_common.sh"

version="${VERSION:-}"
if [ -z "$version" ]; then
  version="$(sed -n 's/^VERSION[[:space:]]*?=[[:space:]]*//p' "$root/Makefile" | tr -d ' \r\n')"
fi

if [ -z "$version" ]; then
  echo "无法推导版本号，请通过 VERSION=1.0.3 传入" >&2
  exit 1
fi

release="${PKG_RELEASE:-1}"

log_info release "开始打包 WrtDeck v${version} 全部架构资产"

# 先构建一次前端，后续各个后端编译复用此产物
build_web release

# 架构对应关系：goarch:pkg_arch
architectures=(
  "arm64:aarch64_cortex-a53"
  "amd64:x86_64"
  "arm:arm_cortex-a7"
  "mipsle:mipsel_24kc"
  "mips:mips_24kc"
)

mkdir -p "$root/dist"

# 打包单个架构的独立二进制、ipk 和 apk
pack_single_arch() {
  local arch_pair="$1"
  local go_arch="${arch_pair%%:*}"
  local openwrt_arch="${arch_pair##*:}"

  log_info release "处理架构: ${go_arch} (${openwrt_arch})"

  # 1. 编译独立静态二进制
  VERSION="$version" GOARCH="$go_arch" SKIP_WEB=1 sh "$root/scripts/release.sh"

  # 2. 打包 ipk
  VERSION="$version" PKG_RELEASE="$release" GOARCH="$go_arch" PKG_ARCH="$openwrt_arch" PKG_FORMAT=ipk SKIP_WEB=1 sh "$root/scripts/package-openwrt.sh"

  # 3. 打包 apk
  VERSION="$version" PKG_RELEASE="$release" GOARCH="$go_arch" PKG_ARCH="$openwrt_arch" PKG_FORMAT=apk SKIP_WEB=1 sh "$root/scripts/package-openwrt.sh"

  # 为非默认架构的 apk 附加架构后缀，避免多架构发布时文件名冲突
  if [ "$go_arch" != "arm64" ]; then
    mv -f "$root/dist/wrtdeck-${version}-r${release}.apk" "$root/dist/wrtdeck-${version}-r${release}.${openwrt_arch}.apk"
  else
    # 默认 arm64 保留标准命名的同时，增加带架构后缀的副本供识别
    cp -f "$root/dist/wrtdeck-${version}-r${release}.apk" "$root/dist/wrtdeck-${version}-r${release}.${openwrt_arch}.apk"
  fi
}

for item in "${architectures[@]}"; do
  pack_single_arch "$item"
done

# 默认 arm64 包同时保留标准无架构后缀名称，兼容文档默认安装指令
cp -f "$root/dist/wrtdeck-${version}-r${release}.aarch64_cortex-a53.apk" "$root/dist/wrtdeck-${version}-r${release}.apk"

# 打包 LuCI 薄壳（架构无关，apk + ipk）
log_info release "打包 LuCI 薄壳"
VERSION="$version" PKG_RELEASE="$release" PKG_FORMAT=both sh "$root/scripts/package-luci.sh"

# 生成发布资产校验清单
log_info release "生成 sha256sums.txt 校验清单"
(
  cd "$root/dist"
  rm -f sha256sums.txt
  # 收集发布包、二进制与公钥
  files=()
  for pattern in "wrtdeck-*.apk" "wrtdeck_*.ipk" "wrtdeck-linux-*" "luci-app-wrtdeck-*.apk" "luci-app-wrtdeck_*.ipk" "wrtdeck-local.rsa.pub"; do
    for f in $pattern; do
      if [ -f "$f" ]; then
        files+=("$f")
      fi
    done
  done

  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${files[@]}" > sha256sums.txt
  else
    shasum -a 256 "${files[@]}" > sha256sums.txt
  fi
)

log_info release "全部资产打包完成，产物位于 dist/"
