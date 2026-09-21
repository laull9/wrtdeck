#!/usr/bin/env bash
# apk 与 ipk 两种格式的通用打包函数。
#
# 调用方先用环境变量描述这个包，再选格式打包：
#   PKG_NAME        包名
#   PKG_VERSION     版本号，如 1.0.0
#   PKG_RELEASE     发布号，默认 1
#   PKG_ARCH        架构名：apk 脚本包用 noarch，ipk 脚本包用 all
#   PKG_DESC        一行式描述（ipk 的 Summary 行）
#   PKG_DESC_LONG   可选的补充描述，按 ipk 规范缩进续行
#   PKG_DEPS        依赖，空格分隔；同一条串同时喂给 apk 的 depend 与 ipk 的 Depends
#   PKG_LICENSE     许可证标识，仅 apk 使用
#   PKG_ROOT        安装到设备上的文件树根目录
#   PKG_APK_SCRIPTS apk 安装脚本，形如 "post-install=路径 post-upgrade=路径"
#   PKG_IPK_SCRIPTS ipk 安装脚本，形如 "postinst=路径 prerm=路径"（优先于 PKG_IPK_HOOKS）
#   PKG_IPK_HOOKS   ipk 钩子目录，其中的 postinst / prerm / conffiles 会被复制进包
#   PKG_IPK_CONFFILES 需要声明为配置文件的路径，空格分隔（由本文件生成 conffiles）
#
# 用法：PKG_NAME=wrtdeck ... pack_apk dist/wrtdeck-1.0.0-r1.apk
#                     PKG_NAME=wrtdeck ... pack_ipk dist/wrtdeck_1.0.0-1_arm64.ipk
#
# 所有函数内部的临时变量一律用 local 声明。这些函数是顺序调用的，
# 曾经踩过的坑正是「助手函数覆盖了调用方记着输出路径的变量」，
# 结果是刚写好的 data.tar.gz 被下一步的 rm 删掉，报错却指向 ar。
# local 在 bash 3.2（macOS 自带）上可用。

set -eu

. "$(dirname "$0")/_common.sh"

# 打包目录为 tar.gz，属主统一写成 root:root，
# 否则 macOS 上 gid 0 会解析成 wheel，设备端解包后属主错误。
tar_gz() {
  local src="$1"
  local out="$2"
  if tar --version 2>/dev/null | grep -qi 'gnu'; then
    ( cd "$src" && tar --owner=root:0 --group=root:0 -czf "$out" . )
  else
    ( cd "$src" && tar --uid 0 --gid 0 --uname root --gname root -czf "$out" . )
  fi
}

# 打包 apk（OpenWrt 25.12 及更新版本的默认包管理器）
pack_apk() {
  local out="$1"
  local py dep script conf
  py="$(find_python3)"
  ensure_apk_key

  # 用 set -- 累积参数再一次性传给打包脚本；set 在函数内不会影响调用方的位置参数
  set -- --root "$PKG_ROOT" \
    --out "$out" \
    --name "$PKG_NAME" \
    --version "$PKG_VERSION" \
    --release "${PKG_RELEASE:-1}" \
    --arch "$PKG_ARCH" \
    --desc "$PKG_DESC" \
    --license "${PKG_LICENSE:-MIT}" \
    --maintainer "${PKG_MAINTAINER:-WrtDeck}"
  for dep in ${PKG_DEPS:-}; do
    set -- "$@" --depend "$dep"
  done
  for script in ${PKG_APK_SCRIPTS:-}; do
    set -- "$@" --script "$script"
  done
  for conf in ${PKG_IPK_CONFFILES:-}; do
    set -- "$@" --conffiles "$conf"
  done
  if [ -n "${APK_SIGN_KEY:-}" ]; then
    set -- "$@" --sign-key "$APK_SIGN_KEY" --sign-name "$APK_SIGN_NAME" \
      --sign-pub-out "$APK_SIGN_PUB"
  fi
  "$py" "$(project_root)/scripts/apk_build.py" "$@"
  report_artifact package "$out"
}

# 打包 ipk（OpenWrt 24.10 及更早版本）
pack_ipk() {
  local out="$1"
  local work ctrldir size_kb member script hook conf archive
  work="$(mktemp -d)"
  ctrldir="$work/control"
  archive="$out"
  case "$archive" in
    /*) ;;
    *) archive="$(pwd)/$archive" ;;
  esac
  mkdir -p "$ctrldir"

  # ipk 安装脚本：显式映射优先，格式为 "postinst=路径 prerm=路径"，
  # 好处是 apk 与 ipk 可以共用同一份脚本文件，不必为了换个文件名复制一遍
  for script in ${PKG_IPK_SCRIPTS:-}; do
    local name="${script%%=*}"
    local path="${script#*=}"
    if [ ! -f "$path" ]; then
      echo "ipk 安装脚本不存在：$path" >&2
      exit 1
    fi
    install -m 0755 "$path" "$ctrldir/$name"
  done

  # 没有显式给出的钩子，再从目录里按 opkg 约定的名字补齐
  if [ -n "${PKG_IPK_HOOKS:-}" ] && [ -d "$PKG_IPK_HOOKS" ]; then
    for hook in postinst prerm postrm preinst; do
      if [ -f "$PKG_IPK_HOOKS/$hook" ] && [ ! -f "$ctrldir/$hook" ]; then
        install -m 0755 "$PKG_IPK_HOOKS/$hook" "$ctrldir/$hook"
      fi
    done
  fi

  if [ -n "${PKG_IPK_CONFFILES:-}" ]; then
    : > "$ctrldir/conffiles"
    for conf in $PKG_IPK_CONFFILES; do
      printf '%s\n' "$conf" >> "$ctrldir/conffiles"
    done
  fi

  size_kb="$(du -sk "$PKG_ROOT" | awk '{ print $1 }')"
  {
    printf 'Package: %s\n' "$PKG_NAME"
    printf 'Version: %s-%s\n' "$PKG_VERSION" "${PKG_RELEASE:-1}"
    if [ -n "${PKG_DEPS:-}" ]; then
      printf 'Depends: %s\n' "$(printf '%s' "$PKG_DEPS" | tr ' ' ',' | sed 's/,,*/,/g')"
    fi
    printf 'Section: %s\n' "${PKG_SECTION:-utils}"
    printf 'Priority: optional\n'
    printf 'Maintainer: %s\n' "${PKG_MAINTAINER:-WrtDeck}"
    printf 'Architecture: %s\n' "$PKG_ARCH"
    printf 'Installed-Size: %s\n' "$size_kb"
    printf 'Description: %s\n' "$PKG_DESC"
    if [ -n "${PKG_DESC_LONG:-}" ]; then
      printf '%s\n' "$PKG_DESC_LONG" | sed 's/^/ /'
    fi
  } > "$ctrldir/control"

  printf '2.0\n' > "$work/debian-binary"
  tar_gz "$ctrldir" "$work/control.tar.gz"
  tar_gz "$PKG_ROOT" "$work/data.tar.gz"

  rm -f "$archive"
  # BSD ar 默认会往归档里塞 __.SYMDEF 符号表，-S 可关掉；
  # 个别平台不认 -S，退回默认行为后再手动摘掉符号表。
  if ! ( cd "$work" && ar rcS "$archive" debian-binary control.tar.gz data.tar.gz ) 2>/dev/null; then
    ( cd "$work" && ar rc "$archive" debian-binary control.tar.gz data.tar.gz )
    ( cd "$work" && ar d "$archive" __.SYMDEF ) 2>/dev/null || true
  fi
  rm -rf "$work"

  member="$(ar t "$archive" | tr '\n' ' ' | sed 's/ *$//')"
  if [ "$member" != 'debian-binary control.tar.gz data.tar.gz' ]; then
    echo "ipk 成员异常：$member" >&2
    exit 1
  fi
  report_artifact package "$archive"
}
