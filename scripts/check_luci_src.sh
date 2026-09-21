#!/bin/sh
# luci-app-wrtdeck 的源码级断言。
#
# 包格式校验（段边界、签名、逐文件校验和）只说明这个包「装得上」，
# 说明不了它「装上去之后能不能用」：薄壳与面板前端分属两个包、两份代码，
# 靠菜单路径、消息名、接口路径与 Web 根目录约定对接，任何一处单独改动都不会报错，
# 现象只是「静默地要用户手输口令」或者「面板框一片空白」。
#
# 这一层专门卡住那些契约，而且不关心包格式：apk 与 ipk 解出来的文件树是一样的，
# 因此 check-apk.sh 与 check-ipk.sh 都 source 它，避免同一批断言写两份然后慢慢漂移。
#
# 调用方需要先准备好：
#   ok / ng / step / assert / assert_eq   断言与分节函数
#   assert_file <包内路径> <期望权限>      由调用方按自己的包格式实现
#   data_dir                             包内文件树解出来的目录
#   meta_file                            包元数据文件（ipk 的 control、apk 的 .PKGINFO）；
#                                        安装脚本按「与它同目录」推断名字
#   root                                 仓库根目录

# 断言：文件里出现某个字符串。用 -e 传模式，带前导短横线的模式才不会被当成选项。
luci_has() {
  if grep -q -e "$2" "$1"; then ok "$3"; else ng "$3"; fi
}

# 断言：文件里不出现某个字符串
luci_lacks() {
  if grep -q -e "$2" "$1"; then ng "$3"; else ok "$3"; fi
}

check_luci_src() {
  # ── 1. 数据部分 ──────────────────────────────────────────────────────────
  step "数据部分"
  assert_file /usr/share/luci/menu.d/luci-app-wrtdeck.json -rw-r--r--
  assert_file /usr/share/rpcd/acl.d/luci-app-wrtdeck.json -rw-r--r--
  assert_file /usr/libexec/rpcd/wrtdeck -rwxr-xr-x
  assert_file /www/luci-static/resources/view/wrtdeck/panel.js -rw-r--r--
  # 同源网关必须是可执行文件：Web 服务器只把可执行的那一个当 CGI 调起
  assert_file /www/cgi-bin/wrtdeck-api -rwxr-xr-x

  # ── 2. 依赖与安装钩子 ────────────────────────────────────────────────────
  # 面板本体由 wrtdeck 包提供；两个包的 Web 目录约定必须对得上，
  # 因此这里同时卡住依赖声明与导出目录。
  step "依赖与安装钩子"
  for dep in luci-base wrtdeck; do
    if grep -q "$dep" "$meta_file"; then ok "依赖声明含 $dep"; else ng "依赖未声明 $dep"; fi
  done
  # 安装脚本的文件名由包格式决定：ipk 叫 postinst，apk 叫 .post-install
  # （解包后前导点可能已被去掉）。它与元数据同在一个目录里，
  # 因此按元数据所在目录去找，几种写法都试一遍，不假设包格式。
  hook_dir="$(dirname "$meta_file")"
  hook="$(ls "$hook_dir/postinst" "$hook_dir/post-install" \
             "$hook_dir/.postinst" "$hook_dir/.post-install" 2>/dev/null | head -1)"
  if [ -n "$hook" ]; then
    ok "安装钩子存在（$(basename "$hook")）"
    assert "安装钩子语法合法" sh -n "$hook"
    # rpcd 只在启动时读 ACL 与后端脚本，装完不重启会出现「能点进去、一调就报权限不足」
    luci_has "$hook" '/etc/init.d/rpcd restart' "安装钩子重启 rpcd 让新 ACL 生效"
    # cgi_prefix 在设备上可配置，包内固定放 /cgi-bin，改过的设备要在安装时补一次
    luci_has "$hook" 'cgi_prefix' "安装钩子按设备实际的 cgi_prefix 补网关入口"
  else
    ng "包内没有安装钩子"
  fi

  # ── 3. rpcd 后端 ─────────────────────────────────────────────────────────
  step "rpcd 后端"
  rpcd="$data_dir/usr/libexec/rpcd/wrtdeck"
  assert "语法合法" sh -n "$rpcd"
  luci_has "$rpcd" 'jshn.sh' "引入 jshn，用 json_* 组装 ubus 返回值"
  for method in status handoff control; do
    luci_has "$rpcd" "$method)" "实现 $method 方法"
  done
  # 视图要靠这几个字段决定「页面与网关就绪了没有、面板入口挂在哪个前缀下」
  for field in cgi_prefix web_ready gateway_ready; do
    luci_has "$rpcd" "\"$field\"" "status 汇报 $field"
  done
  # rpcd 的后端与 ACL 是两份文件，方法名漂移的后果是「方法存在但被 ACL 拒掉」，很难查
  acl="$data_dir/usr/share/rpcd/acl.d/luci-app-wrtdeck.json"
  for method in status handoff control; do
    luci_has "$acl" "\"$method\"" "ACL 放行 $method"
  done
  # jshn 的 json_add_boolean 只认 0/1：喂它 shell 字符串 "true" 会被静默序列化成
  # JSON 的 false，不报任何错。真机上的表现是 status.running 恒假——LuCI 里永远
  # 显示「服务未运行」，而且薄壳会因为 running 为假提前 return，连面板框都不渲染。
  # 包格式校验、源码断言、本地桩化统统看不见这一类错，只能靠这条纪律卡住。
  bool_strings="$(grep -n -E '^[[:space:]]*[A-Za-z_][A-Za-z0-9_]*=(true|false)[[:space:]]*$' "$rpcd" | tr '\n' ';')"
  if [ -n "$bool_strings" ]; then
    ng "rpcd 用字符串 true/false 表示布尔量，jshn 会静默转成 JSON false：$bool_strings"
  else
    ok "rpcd 的布尔量用 0/1（jshn 的 json_add_boolean 只认数字）"
  fi
  # pgrep 由 procps-ng 提供，最小固件的 busybox 没编入它；拿它当唯一判据会静默判成未运行
  luci_has "$rpcd" '/etc/init.d/wrtdeck status' "service_running 以 procd 为主判据，不单靠可选的 pgrep"

  # ── 4. 同源网关入口 ──────────────────────────────────────────────────────
  step "同源网关入口"
  gw="$data_dir/www/cgi-bin/wrtdeck-api"
  assert "语法合法" sh -n "$gw"
  luci_has "$gw" '\-gateway' "调用面板的 -gateway 模式完成转发"
  luci_has "$gw" '/etc/wrtdeck/config.json' "使用设备上的面板配置"
  # 面板本体没装时不能一声不响地退出，否则浏览器只看到一个空洞的 502
  luci_has "$gw" 'Status: 503' "面板缺席时返回可读的 503"

  # ── 5. LuCI 菜单与视图 ───────────────────────────────────────────────────
  step "LuCI 菜单与视图"
  menu="$data_dir/usr/share/luci/menu.d/luci-app-wrtdeck.json"
  luci_has "$menu" 'admin/services/wrtdeck' "菜单挂在 admin/services/wrtdeck"
  luci_has "$menu" 'wrtdeck/panel' "菜单指向视图 wrtdeck/panel"
  luci_has "$menu" 'luci-app-wrtdeck' "菜单依赖本包的 ACL"
  # depends.acl 必须写成字符串数组。ucode 版的 dispatcher 用 ...spread 展开它
  # （dispatcher.uc: push(ctx.acls, ...(node?.depends?.acl || []))），
  # 写成对象会让整条菜单 500：({"luci-app-wrtdeck":["read"]}) is not iterable。
  # 更毒的是这个错只在真机点进菜单时出现，包格式校验与本地桩化都看不见。
  # 去掉空白后再比对，免得断言被缩进风格绑死。
  menu_norm="$(tr -d ' \t\n' < "$menu")"
  case "$menu_norm" in
    *'"acl":['*) ok "菜单 depends.acl 用数组形式（ucode dispatcher 要求）" ;;
    *) ng "菜单 depends.acl 不是数组：ucode 会因 'is not iterable' 让菜单 500" ;;
  esac
  panel="$data_dir/www/luci-static/resources/view/wrtdeck/panel.js"
  for token in "'require view'" 'rpc.declare' 'postMessage' 'view.extend' 'iframe'; do
    luci_has "$panel" "$token" "视图含 $token"
  done

  # 面板页面与接口都必须挂在同源路径上，而不是由浏览器去拼「设备地址 + 端口」：
  # 页面被反向代理到公网域名之后，那种拼法必然指向一个连不通的地址。
  # 断言只针对代码行——注释里提到固定端口正是在说明为什么不能这么做。
  panel_code="$(grep -v -e '^[[:space:]]*//' -e '^[[:space:]]*\*' -e '^[[:space:]]*/\*' "$panel")"
  for bad in location.host location.port 8080; do
    if printf '%s\n' "$panel_code" | grep -q -F "$bad"; then
      ng "视图里出现 $bad：面板地址不该由浏览器端拼出来"
    else
      ok "视图不出现 ${bad}（面板地址不由浏览器端拼）"
    fi
  done
  for scheme in 'http://' 'https://'; do
    if printf '%s\n' "$panel_code" | grep -q -F "$scheme"; then
      ng "视图硬编码了 $scheme 前缀"
    else
      ok "视图不硬编码 $scheme 前缀（加密方式跟随 LuCI）"
    fi
  done
  for token in 'index.html' 'cgi_prefix' 'embed=1' 'wrtdeck-api'; do
    luci_has "$panel" "$token" "视图用同源 $token 定位面板与接口"
  done
  # 面板页面在 Web 根目录下（/www/<panel_dir>），**不**挂在 CGI 前缀下：
  # 只有 API 网关那一份脚本才走 cgi_prefix。两者混起来会拼出一个必然 404 的
  # 地址（/cgi-bin/wrtdeck/index.html），现象是 LuCI 里的面板框一片空白。
  # 只截取 panel_page 函数体，免得把 panel_api 里正当的 normalize_prefix 算进来。
  panel_page_body="$(sed -n '/function panel_page/,/^}/p' "$panel")"
  case "$panel_page_body" in
    *normalize_prefix*)
      ng "panel_page 掺进了 CGI 前缀：面板在 Web 根目录下，不在 cgi_prefix 下" ;;
    *)
      ok "panel_page 不掺 CGI 前缀（面板在 Web 根目录下）" ;;
  esac

  # ── 6. 与面板前端的交接契约 ──────────────────────────────────────────────
  step "与面板前端的交接契约"
  session="$root/web/src/lib/handoff.ts"
  server="$root/internal/api/server.go"
  if [ -f "$session" ]; then
    for message in wrtdeck.handoff wrtdeck.ready wrtdeck.accepted; do
      if grep -q "$message" "$panel" && grep -q "$message" "$session"; then
        ok "消息名 $message 在薄壳与前端两侧一致"
      else
        ng "消息名 $message 两侧不一致（薄壳/前端至少一侧缺失）"
      fi
    done
  else
    ng "找不到 $session，无法核对交接契约"
  fi
  luci_has "$rpcd" '/api/v1/session/handoff' "rpcd 用 /api/v1/session/handoff 申请登录码"
  if [ -f "$server" ]; then
    luci_has "$server" '/api/v1/session/handoff' "服务端注册同名接口"
  else
    ng "找不到 $server，无法核对登录码接口"
  fi
  # 交接只传一次性登录码：长期凭据出现在 postMessage 里是设计上要避免的事
  luci_lacks "$panel" 'token' "交接消息只携带一次性登录码，不含长期凭据"

  # 面板页面由本体导出到 Web 根目录，导出目录是两边共用的约定。
  # 两处对不上时页面在 LuCI 里会 404，而两边各自看都没问题，很难定位。
  init="$root/packaging/openwrt/wrtdeck.init"
  if [ -f "$init" ]; then
    web_dir="$(sed -n 's/^WEB_DIR=//p' "$init" | head -1)"
    assert_eq "$web_dir" '/www/wrtdeck' "面板导出目录与薄壳入口约定一致"
  else
    ng "找不到 $init，无法核对导出目录"
  fi

  # 前端必须能认出被内嵌、并接受来自薄壳的 API 前缀
  for file in "$root/web/src/lib/embed.ts" "$root/web/src/lib/live.ts"; do
    if [ -f "$file" ]; then ok "存在 $(basename "$file")"; else ng "缺少 $(basename "$file")"; fi
  done
}
