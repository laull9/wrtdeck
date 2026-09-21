#!/bin/sh
# 从 assets/WrtDeck.png 重新生成站点图标与界面 logo。
# 换图标只需要替换 assets/WrtDeck.png，然后执行 make icons。
set -eu

. "$(dirname "$0")/_common.sh"

# 定位带 Pillow 的 python。优先 python3，其次常见安装位置，
# 最后退回系统自带的 /usr/bin/python3（macOS 上通常已带 Pillow）。
find_python() {
  for candidate in python3 /opt/homebrew/bin/python3 /usr/local/bin/python3 /usr/bin/python3; do
    if command -v "$candidate" >/dev/null 2>&1; then
      if "$candidate" -c "import PIL" >/dev/null 2>&1; then
        command -v "$candidate"
        return 0
      fi
    fi
  done
  echo "找不到带 Pillow 的 python3，请先执行：python3 -m pip install Pillow" >&2
  return 1
}

python_bin="$(find_python)"
root="$(project_root)"

log_info icons "生成站点图标与界面 logo"
"$python_bin" "$root/scripts/make-icons.py"

for artifact in \
  "$root/web/public/favicon.ico" \
  "$root/web/src/assets/logo.png"; do
  report_artifact icons "$artifact"
done
