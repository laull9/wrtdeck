# WrtDeck 快速安装

面向 ARM64 / MIPS 等 OpenWrt 设备的安装说明。二进制为静态链接，不依赖设备上的 libc 版本。

- 架构与设计：[ARCHITECTURE.md](./ARCHITECTURE.md)
- 从源码构建：[从源码构建安装包](#从源码构建安装包)

## 1. 前提

| 项目 | 要求 |
| --- | --- |
| 系统 | OpenWrt 21.02 及以上（procd；apk 需 25.12+，ipk 需 24.10 及更早） |
| 架构 | 见下表，默认包为 `aarch64_cortex-a53` |
| 存储 | 约 8 MB（二进制 7.9 MB + 配置） |
| 内存 | 常驻约 8–12 MB，随注册项数量增长 |
| 依赖 | `ca-bundle`（HTTPS 设备需要），安装时由包管理器自动处理 |

包的架构字段必须与设备一致，可用 `uname -m` 或 `opkg print-architecture` 确认：

| 设备类型 | `opkg print-architecture` 示例 | 打包时的参数 |
| --- | --- | --- |
| ARMv8 路由器（多数 AX/AC 机型） | `aarch64_cortex-a53` | `GOARCH=arm64` |
| ARMv7 老机型 | `arm_cortex-a7` | `GOARCH=arm` |
| x86 软路由 | `x86_64` | `GOARCH=amd64` |
| MIPS 小闪存机型 | `mipsel_24kc` | `GOARCH=mipsle` |

## 2. 安装

OpenWrt 25.12 起包格式已从 ipk 切换为 **apk**，24.10 及更早仍是 ipk。两种包内容完全一致，按设备版本选一个即可。

把包传到设备上（`scp`、U 盘或局域网共享均可），然后：

```sh
# OpenWrt 25.12 及以上（apk）
apk update                                              # 先取一次仓库索引，ca-bundle 要从仓库装
apk add --allow-untrusted ./wrtdeck-1.0.0-rc2-r1.apk

# OpenWrt 24.10 及更早（ipk）
opkg update
opkg install ./wrtdeck_1.0.0-rc2-1_aarch64_cortex-a53.ipk
```

> **本地包之间不会互相解析依赖**。`apk` / `opkg` 只在**仓库索引**与**已安装集合**里找依赖，
> 不会把同目录下的另一个本地包当作候选。面板包 `wrtdeck` 只以本地文件形式存在，
> 因此**必须先装面板包，再装薄壳**（见下节）；或者把两个文件写在同一条命令里：
>
> ```sh
> apk add --allow-untrusted ./wrtdeck-1.0.0-rc2-r1.apk ./luci-app-wrtdeck-1.0.0-rc2-r1.apk
> ```

`post-install` 会自动 `enable` 并 `start` 服务，安装成功后会打印访问地址、默认口令与 Token 查看方式。

安装动作与设备文件的变化：

```text
/usr/bin/wrtdeck              静态 ELF
/etc/init.d/wrtdeck           procd 启动脚本
/etc/wrtdeck/config.json      默认配置（conffiles 声明，升级不覆盖）
```

### 可选：LuCI 薄壳

如果设备上装了 LuCI，可以再装一个十几 KB 的 `luci-app-wrtdeck`，在 LuCI 菜单的「服务 → WrtDeck」里得到**原样内嵌的面板页面**——不是一个跳转链接，而且登录 LuCI 之后进去不用再输一次面板口令。

先确认面板包已装好（`apk list -I | grep wrtdeck`），再装薄壳：

```sh
apk add --allow-untrusted ./luci-app-wrtdeck-1.0.0-rc2-r1.apk   # apk 设备
opkg install ./luci-app-wrtdeck_1.0.0-rc2-1_all.ipk             # ipk 设备
```

薄壳只有 rpcd 后端、一个 LuCI 视图、一份菜单与一个 CGI 网关入口，不含二进制，因此与 CPU 架构无关。它声明了 `Depends: wrtdeck`，而 `wrtdeck` 不在任何仓库里，面板没装好时这一步必然失败，报 `wrtdeck (no such package)`。

装完薄壳后，面板的对外入口就交给 LuCI 了：面板本体默认只监听 `127.0.0.1:8080`，局域网里直连 `<设备IP>:8080` 是不通的。这是**有意的**，理由见下面「LuCI 内嵌与同源网关」。

## 3. 首次登录

装了薄壳的运行方式：登录 LuCI 后打开「服务 → WrtDeck」，面板会免登录直接进来。

没装薄壳的运行方式：面板默认只监听回环地址，需要先把 `config.json` 的 `listen` 改成 `0.0.0.0:8080` 再重启服务（命令见第 4 节）：

```json
{ "listen": "0.0.0.0:8080" }
```

OpenWrt 默认防火墙对 LAN 放行 input，因此同网段浏览器直接访问 `http://<设备IP>:8080` 即可。

首次启动会写入两样凭据：

| 凭据 | 首次取值 | 用途 |
| --- | --- | --- |
| 登录口令 | `admin` | 浏览器登录面板，**必须立即修改** |
| API Token | 43 字符随机值 | 脚本、设备本地调用、面板「用 Token 登录」 |

用 `admin` 登录后，面板会**强制弹出改口令对话框**，不改完无法进入：

- 新口令至少 8 位，且需包含**至少两类字符**（大小写 / 数字 / 符号）；
- `admin`、`12345678`、`password`、`openwrt` 等常见弱口令会被拒绝；
- 改口令成功后**所有已登录会话立即失效**，当前浏览器自动换发新凭据。

**在改口令之前，只有私网与回环来源能登录**，公网来源会被直接拒绝——这是防止默认口令被公网扫描器捡到的兜底。

API Token 的取出方式：

```sh
/etc/init.d/wrtdeck token                                  # 推荐
/usr/bin/wrtdeck -config /etc/wrtdeck/config.json -print-token
cat /etc/wrtdeck/secrets.json
```

## 4. 服务管理

```sh
/etc/init.d/wrtdeck start       # 启动
/etc/init.d/wrtdeck stop        # 停止
/etc/init.d/wrtdeck restart     # 重启（配置改动后）
/etc/init.d/wrtdeck reload      # 等价于 restart
/etc/init.d/wrtdeck enable      # 开机自启
/etc/init.d/wrtdeck disable     # 取消自启
/etc/init.d/wrtdeck token       # 打印当前 API Token
/etc/init.d/wrtdeck password    # 重置登录口令（忘记口令时用）
/etc/init.d/wrtdeck export_web  # 把面板页面重新导出到 /www/wrtdeck
```

`password` 会从标准输入读一个新口令，重置后标记为「待修改」，下次登录仍需再改一次。它是设备主人忘记口令时**唯一的找回手段**，因此需要有 root shell 才能执行。

`export_web` 在 `start` 时自动执行一次（页面编在二进制里，所以页面与二进制永远同版本）；LuCI 里出现空白框时可以手动重跑一次。子命令名用下划线而不是短横线，因为 `rc.common` 会把子命令名直接当函数名调用，而短横线不是合法的 shell 标识符字符——写成 `export-web()` 会让整个 init 脚本在任何子命令下都报语法错误。

进程由 procd 托管，异常退出后按 5 秒间隔最多重启 5 次（1 小时后计数重置）；文件描述符上限提到 1024。

日志走 syslog：

```sh
logread -e wrtdeck -f
```

认证相关事件带 `[auth]` 前缀，例如登录失败、锁定、改口令成功。**日志只记录来源与结果，绝不写入口令、Token 或交接码。**

## 5. 配置

`/etc/wrtdeck/config.json`。它被声明为 conffiles，升级不会覆盖你改过的内容。

```json
{
  "listen": "127.0.0.1:8080",
  "data_dir": "/etc/wrtdeck",
  "auth": {
    "session_ttl_minutes": 720,
    "max_login_attempts": 5,
    "lockout_minutes": 15
  },
  "tls": {
    "enabled": false
  },
  "gateway": {},
  "exec": { "enabled": true },
  "limits": { "history_limit": 120 },
  "workers": { "source": 4, "action": 4 },
  "mqtt": {
    "keepalive_seconds": 30,
    "connect_timeout_ms": 5000,
    "max_reconnect_delay_ms": 30000
  }
}
```

几个值得注意的字段：

| 字段 | 说明 |
| --- | --- |
| `listen` | 默认 `127.0.0.1:8080`，只监听回环，对外入口由 LuCI 承担。要让局域网直连就改成 `0.0.0.0:8080`，或改成 `127.0.0.1:8080` 后前置 Nginx |
| `data_dir` | 注册表、密钥、配置与自签证书的落盘目录，改完要同步改 `/etc/init.d/wrtdeck` 里的 `CONF_DIR` |
| `gateway.*` | 同源 CGI 网关。留空即按默认工作：上游地址由 `listen` 推导，校验 LuCI 会话后注入凭据 |
| `gateway.upstream` | 面板本体地址，留空时按 `listen` 推出回环地址；面板本体与网关分处两个进程时才需要显式指定 |
| `gateway.verify_luci_session` | 默认 `true`。设为 `false` 时**不会注入凭据**（不校验却注入等于把凭据送给所有能访问设备 Web 端口的人），面板退化成需要自己登录 |
| `auth.disabled` | 设为 `true` 关闭鉴权，**仅限本机调试** |
| `auth.token_env` | 从环境变量读 Token，不再落盘。见下节 |
| `auth.session_ttl_minutes` | 登录会话有效期，默认 720 分钟（12 小时） |
| `auth.max_login_attempts` | 单个来源在窗口内允许的登录失败次数，默认 5，超出即锁定 |
| `auth.lockout_minutes` | 首次锁定分钟数，默认 15；连续失败按指数退避翻倍，上限 1 小时 |
| `tls.*` | 启用 HTTPS，见第 6 节 |
| `limits.history_limit` | 每个信息源在内存里保留的采样条数，`0` 表示关闭。**只占内存，不写闪存** |
| `limits.min_interval_ms` | 信息源轮询间隔下限，防止注册项把设备拖垮 |
| `exec.enabled` | Exec 传输总开关，默认关闭 |
| `limits.max_body_bytes` | 单次响应/载荷长度上限，默认 256 KB |

改完配置执行 `/etc/init.d/wrtdeck restart` 生效。**注册项不用重启**，通过 API 或面板改动即时生效。

### 用环境变量提供 Token

不想让 Token 落盘时，在 `/etc/init.d/wrtdeck` 的 `start_service()` 里导出变量，并在配置中声明变量名：

```json
{ "auth": { "token_env": "WRTDECK_TOKEN" } }
```

```sh
# /etc/init.d/wrtdeck
export WRTDECK_TOKEN='你的Token'
```

注意：**声明了 `token_env` 却没有该环境变量时进程会拒绝启动并给出明确报错**，不会静默退回随机 Token。这是刻意设计——否则运维会以为环境变量已生效，实际鉴权用的是另一个谁也不知道的值。这种模式下 `secrets.json` 不会写入 Token，Token 也无法在运行时轮换（不影响口令登录）。

### 闪存磨损

设计上避免频繁写闪存：

- 运行状态（各信息源的当前值、运行历史）**只驻内存**，重启即丢；
- 登录会话**只驻内存**，重启后浏览器需重新登录；
- `registry.json` 只在注册表真正发生变化时写盘；
- `config.json`、`secrets.json` 只在首次生成或配置变更（含改口令）时写盘。

## 6. 公网暴露与 TLS 加固

面板可能控制真实设备，因此按「可能暴露在公网」做了默认加固，**不需要额外配置就已生效**：

| 措施 | 说明 |
| --- | --- |
| 强制首改口令 | 未改口令前仅私网/回环可登录，会话凭据只能调 `me` / `password` / `logout` |
| 登录节流 | 按来源统计失败次数，超限锁定并按指数退避延长；另有 50 次/分钟全局兜底 |
| 严格 CSP | `default-src 'self'`、`script-src 'self'`（无内联脚本）、`frame-ancestors` 放行同源与 LuCI |
| 安全响应头 | `X-Content-Type-Options: nosniff`、`Referrer-Policy: no-referrer`、`Permissions-Policy` 收窄 |
| 禁缓存 | `/api/` 一律 `Cache-Control: no-store` |
| 反节流绕过 | 来源判定**刻意忽略 `X-Forwarded-For`**，避免伪造请求头绕过锁定 |
| 代理感知 | 只认 `X-Forwarded-For` / `X-Forwarded-Host` / `X-Real-Ip` **出现与否**，不采信其值；经代理进来时不再当作设备本机，同源网关绕不过这两条 |

### 启用 HTTPS

**强烈建议**：只要面板能从 WAN 访问，就应启用 TLS，否则口令与会话凭据会明文过网。

```json
{
  "tls": {
    "enabled": true,
    "cert_file": "/etc/wrtdeck/tls.crt",
    "key_file": "/etc/wrtdeck/tls.key",
    "redirect_listen": "0.0.0.0:8080"
  },
  "listen": "0.0.0.0:8443"
}
```

| 字段 | 说明 |
| --- | --- |
| `enabled` | 打开 HTTPS |
| `cert_file` / `key_file` | 自带证书。**留空则自动生成** |
| `auto_self_signed` | 未显式配置时按 `true` 处理；设为 `false` 且未给证书会启动失败 |
| `redirect_listen` | 非空时额外监听该地址，把明文请求 **308 永久重定向**到 HTTPS |

不提供证书时会自动生成一张 **ECDSA P-256 自签证书**（有效期 10 年），落到 `data_dir` 下，重启复用不重复生成。自签证书浏览器会提示不受信任，首次访问需手动确认——能接受就直接用，介意就换成自己的证书（或用 `mkcert` 等工具签一张局域网内受信的证书）。

启用 TLS 后，启动日志会打印证书指纹（SHA-256），方便在浏览器里核对：

```sh
/etc/init.d/wrtdeck restart
logread -e wrtdeck | grep -i fingerprint
```

HSTS 只在**确认走的是 TLS** 时才下发，因此不会误伤明文部署。

### LuCI 内嵌与同源网关

这是 rc2 的核心变化。之前 LuCI 里只有一个跳转链接，而面板地址要靠浏览器去拼「设备地址 + 8080」——一旦设备被反向代理到公网域名，这个地址必然连不通，而且浏览器的加密方式与面板自己的对不上。现在面板被**原样复刻到 LuCI 所在的源上**：

| 部分 | 挂在哪 | 谁在服务 |
| --- | --- | --- |
| 面板页面与资源 | `/www/wrtdeck/` | 设备自带的 Web 服务器（uhttpd），当静态文件发 |
| 面板接口 | `<cgi_prefix>/wrtdeck-api` | 同源下的 CGI，转交给面板本体 |
| 面板本体 | `127.0.0.1:8080` | 只监听回环，不对局域网另开端口 |

面板本体启动时把内嵌的前端资源导出到 `/www/wrtdeck`（`wrtdeck -export-web`，init 里 `start_service` 会调用）。导出的页面用的是**相对路径**，因此既能挂在 `/wrtdeck/` 下，也能被别的前缀布置，页面自己不知道也不需要知道设备地址。

`cgi_prefix` 是可配置的（`uci get uhttpd.main.cgi_prefix`）。包内固定把网关放在 `/www/cgi-bin/`，装完由安装脚本按设备实际的前缀补一个符号链接：

```sh
/etc/init.d/wrtdeck export_web      # 页面没了？手动重新导出一次
ls -l /www/wrtdeck/index.html       # 页面是否就位
ls -l /www/cgi-bin/wrtdeck-api      # 网关入口是否就位
uci get uhttpd.main.cgi_prefix      # 设备实际的 CGI 前缀
```

凭据交接有两条路，按顺序尝试，都不成才退回收口令登录页：

1. **同源网关代签**：网关先向 rpcd 问一句「这个浏览器带的 LuCI 会话登录了吗」，证实通过才替它补上面板凭据。浏览器手里始终没有面板凭据，界面上会显示一个「LuCI 免登录」小标记。
2. **一次性登录码**：rpcd 用设备本机的 Token 换一张 60 秒有效、只能兑一次的登录码，父窗口用 `postMessage` 递给面板。地址栏与浏览器历史里不放任何长期凭据。

两条路都**不会因为认不出人就把门打开**：网关校验不通过就只是原样转发，面板自己那道鉴权照常生效。

排障对照：

| 现象 | 原因与处理 |
| --- | --- |
| LuCI 里是一个空白框 | 面板页面没导出。重启服务（`/etc/init.d/wrtdeck restart`）或用手动导出命令；确认 `/www` 可写 |
| 面板能打开但仍要求输口令 | 网关没就绪或 LuCI 会话没被认出来。看 `uci get uhttpd.main.cgi_prefix` 是否为空；确认 `/www/<前缀>/wrtdeck-api` 存在且可执行 |
| 界面显示「同源接口网关不在 Web 服务器的 CGI 前缀下」 | 同上，重装或手动补一次链接：`ln -sf /www/cgi-bin/wrtdeck-api /www$(uci get uhttpd.main.cgi_prefix)/wrtdeck-api` |
| 点「重新拉取」才有数据，界面写「定时刷新」 | 正常。内嵌时不走 SSE 长连接（一条长连接会在路由器上常驻一个 CGI 进程），改走定时拉取 |
| 想从局域网直连面板 | 把 `config.json` 的 `listen` 改成 `0.0.0.0:8080`。但要清楚这会让面板绕开 LuCI 的加密方式与登录状态 |

### 与 LuCI 共存

LuCI 自身的 `/cgi-bin/luci` 路径不在本服务的响应头作用域内，无需特殊处理。

面板页面的 CSP `frame-ancestors` 需要放行 LuCI 所在的页面来源，网关转发时会把浏览器的 `Origin` 与 `Host` 原样带给面板，因此由面板自己判断，不需要在设备上额外配置。

面板导出的页面与二进制**同版本**（页面编在二进制里），所以升级面板包时页面会自动跟着更新，两个包也可以独立升级。

### 与 LuCI 使用同一套加密

面板不另开端口、也不另起证书，它的加密方式完全跟随浏览器访问 LuCI 的那一段：公网配好 HTTPS 就是 HTTPS，局域网里是明文就是明文。

想给整个 LuCI（连带面板）加上 HTTPS，请配置 uhttpd 自己的证书：

```sh
uci set uhttpd.main.redirect_https=1
uci add_list uhttpd.main.listen_https=0.0.0.0:443
uci set uhttpd.main.cert=/etc/uhttpd.crt
uci set uhttpd.main.key=/etc/uhttpd.key
uci commit uhttpd && /etc/init.d/uhttpd restart
```

或把整个 LuCI 放在一个 HTTPS 反向代理之后。无论哪种，面板都跟着走，不需要单独配置。若当前是**公网域名 + 明文 HTTP**，LuCI 里的入口页会明确提示凭据会明文过网。

## 7. 接入第一台设备

服务本身不预置任何设备，全部通过注册表 API 或面板添加。下面三个例子覆盖三类最常见的接法。脚本调用统一使用 **API Token**：

```sh
TOKEN="$(/etc/init.d/wrtdeck token)"
```

### HTTP 信息源：每 5 秒读一次温度

```sh
curl -X PUT "http://127.0.0.1:8080/api/v1/registry/bedroom-temp" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{
  "id": "bedroom-temp",
  "kind": "source",
  "name": "卧室温度",
  "enabled": true,
  "transport": {
    "type": "http",
    "http": { "method": "GET", "url": "http://192.168.1.20/api/temp" }
  },
  "schedule": { "interval_ms": 5000 },
  "extract": { "type": "json", "path": "sensor.temperature", "value_type": "number" },
  "ui": { "type": "metric", "unit": "°C", "precision": 1 }
}'
```

### UDP 动作：向设备发一条开机指令

```sh
curl -X PUT "http://127.0.0.1:8080/api/v1/registry/nas-power" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{
  "id": "nas-power",
  "kind": "action",
  "name": "NAS 开机",
  "enabled": true,
  "transport": {
    "type": "udp",
    "udp": { "address": "192.168.1.20:9000", "payload": "POWER_ON", "expect_reply": false }
  },
  "ui": { "type": "button", "confirm": { "enabled": true, "title": "确认开机？" } }
}'
```

### MQTT 信息源：订阅 broker 推送

MQTT 支持两种模式：信息源用 `subscribe`（由 broker 推送，不需要轮询），动作用 `publish`。

```sh
curl -X PUT "http://127.0.0.1:8080/api/v1/registry/livingroom-temp" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{
  "id": "livingroom-temp",
  "kind": "source",
  "name": "客厅温度",
  "enabled": true,
  "transport": {
    "type": "mqtt",
    "mqtt": {
      "broker": "mqtt://192.168.1.30:1883",
      "mode": "subscribe",
      "topic": "home/livingroom/temp",
      "qos": 1
    }
  },
  "extract": { "type": "json", "path": "value", "value_type": "number" },
  "ui": { "type": "metric", "unit": "°C", "precision": 1 }
}'
```

MQTT 使用 **3.1.1** 协议，ESP32、Tasmota、Zigbee2MQTT、mosquitto 等设备与 broker 均可直连。同一 broker 上的多个注册项会复用一条长连接，断线后自动重连并恢复订阅，无需手工干预。

订阅型信息源的当前值由推送驱动，因此面板上不提供「刷新」按钮，手动调用刷新接口会返回 `409 push_source`。

查看某个信息源最近的采样：

```sh
curl -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:8080/api/v1/sources/livingroom-temp/history?limit=60"
```

## 8. 升级与卸载

```sh
apk add --allow-untrusted ./wrtdeck-1.0.1-r1.apk          # apk 升级
opkg install ./wrtdeck_1.0.1-1_aarch64_cortex-a53.ipk      # ipk 升级

apk del wrtdeck              # apk 卸载
opkg remove wrtdeck          # ipk 卸载
```

- 升级时 `/etc/wrtdeck/config.json` 因 conffiles 声明而保留你的改动；控制脚本会 `restart` 服务而不是重新 `enable`。
- `registry.json` 与 `secrets.json` 不在包内，升级和卸载都不会被清除，**口令与 Token 因此得以保留**。
- 卸载后如需彻底清理：`rm -rf /etc/wrtdeck`。

### 从旧包名 owdash 升级

1.0.0 之前的包名、二进制、init 脚本与配置目录都叫 `owdash`（`WrtDeck` 只是产品名）。
改名后这些路径全部变成 `wrtdeck`，因此**不是一次原地升级**：

```sh
/etc/init.d/owdash stop
apk del owdash luci-app-wrtdeck            # 或 opkg remove owdash luci-app-wrtdeck
mv /etc/owdash /etc/wrtdeck                # 想保留口令、Token 与注册项时必须做这一步
apk add --allow-untrusted ./wrtdeck-1.0.0-rc2-r1.apk ./luci-app-wrtdeck-1.0.0-rc2-r1.apk
```

- 不迁移 `/etc/owdash` 也能启动，但会当成全新安装：初始口令回到 `admin`，API Token 重新生成。
- 薄壳与面板之间的 postMessage 消息名同步改成了 `wrtdeck.*`，**两边必须一起升级**，否则 LuCI 里点进面板会退化成口令登录页。
- 旧的 `/usr/bin/owdash` 与 `/etc/init.d/owdash` 会随旧包一起卸载清除，不需要手工处理。

## 从源码构建安装包

需要 Go 1.24+、Node 18+ 与 pnpm（脚本会自动探测 `go`，或尊重 `GO_BIN`）。

```sh
make packages                                       # 面板 apk + ipk，薄壳 apk + ipk，四种一次出全
make apk                                            # 只要面板 apk
make ipk                                            # 只要面板 ipk
make luci                                           # 只要薄壳（与架构无关，两种格式一起出）

GOARCH=arm     make apk                             # ARMv7
GOARCH=amd64   make apk                             # x86_64
GOARCH=mipsle  make ipk                             # MIPS 小端
VERSION=1.2.0 PKG_RELEASE=2 make packages            # 指定版本
```

产物在 `dist/`：

```text
dist/wrtdeck-<版本>-<发布号>.apk                    面板，按架构区分
dist/wrtdeck_<版本>-<发布号>_<架构>.ipk             面板，按架构区分
dist/luci-app-wrtdeck-<版本>-<发布号>.apk          薄壳，架构无关
dist/luci-app-wrtdeck_<版本>-<发布号>_all.ipk      薄壳，架构无关
```

打包过程不依赖 OpenWrt SDK：

- **ipk** 本身就是 `ar` 归档（`debian-binary` + `control.tar.gz` + `data.tar.gz`），脚本用系统自带的 `ar`/`tar` 直接组装；
- **apk** 是新版分段格式（多个 gzip 成员拼成的 tar 流 + 尾部签名），由 `scripts/apk_build.py` 生成并用内置 RSA 私钥签名，公钥同时输出到 `dist/wrtdeck-local.rsa.pub`。

两者都把 tar 内的属主统一写成 `root:root`，避免在 macOS 上打包时 gid 0 被解析成 `wheel`；同时断言交叉编译产物确实是 `statically linked`。

> 安装 apk 时需要 `--allow-untrusted`，因为签名用的是本项目自带的本地密钥，不在设备的信任库里。要免掉这个参数，请用自己的密钥重新签名并预置公钥。

## 打包测试

```sh
make check-apk     # apk 结构：gzip 成员切分、段边界、签名、逐文件校验和
make check-ipk     # ipk 结构：归档格式、control 字段、权限、属主、ELF 属性、init 契约
make check-luci       # 薄壳（apk）：结构 + 源码级交接契约
make check-ipk-luci   # 薄壳（ipk）：同一批断言，两种格式一份代码
make test-apk      # apk：结构校验 + 安装模拟
make test-ipk      # ipk：结构校验 + 安装模拟
make test-pkg      # 上面几项一次跑全（推荐提交前执行）
```

薄壳的源码级断言放在 `scripts/check_luci_src.sh`，apk 与 ipk 两个校验脚本共用同一份——两边的文件树是一样的，分开写只会慢慢漂移。它卡住的是「装得上」之外的「装上去能用」：菜单路径与视图文件是否同名、rpcd 的字段与 ACL 是否对得上、视图里有没有出现 `location.host` / 固定端口 / 硬编码协议前缀（这三样一旦出现，页面被反向代理到公网域名就必然连不通）、交接消息名与接口路径在前端与薄壳两侧是否一致、`panel.js` 里有没有混进长期凭据。

`simulate-openwrt.sh` 会解包到临时目录，把 procd 的 `procd_*` 函数替换成记录参数的桩，执行 init 脚本的 `start_service()`，然后用它下发的命令真实把服务跑起来，验证：

1. 服务能监听就绪，`/api/v1/health` 与面板页面正常返回；
2. init 把面板页面导出到 `/www/wrtdeck`，页面与它引用的资源都在，且引用是相对路径；
3. 首启生成 `secrets.json`，其中含口令散列、盐与「待改口令」标记，且**文件里没有明文口令**；
4. 未带凭据访问 API 返回 401；
5. 默认口令 `admin` 登录成功、强制改密生效、改密后旧会话失效；
6. 连续错误口令触发登录节流，返回 429 并进入锁定；
7. 一次性交接码能换到会话凭据；
8. 空注册表启动时不写 `registry.json`，写入注册项后才落盘；运行状态不落盘；
9. 启动日志为生产形态，不带开发模式提示；
10. **同源网关**：按 uhttpd 的约定调起 `/www/cgi-bin/wrtdeck-api`，验证公开接口照常转发、没有可证实的 LuCI 会话时**不补凭据**（401）、浏览器自带凭据原样转发、非面板 API 的路径一律拒绝（400）、登录码绝不代传（403）。

两处必要的模拟已在该脚本头部注明：开发机上没有 procd，且交叉编译出的 Linux ELF 无法在开发机执行（跑的是同源码的本机产物，包内 ELF 的静态链接与目标架构由 `check-ipk.sh` 单独断言）。

> 已验证范围：apk / ipk 结构、init 脚本行为、启动与两种凭据的鉴权链路、登录节流、一次性交接、数据落盘位置、MQTT 与真实 broker 的互操作。**尚未在真实 OpenWrt 设备上安装**（构建环境没有可用的设备、QEMU 或容器），首次上真机时请留意第 1 节的架构匹配与第 3 节的防火墙放行。

## 排障

| 现象 | 排查方向 |
| --- | --- |
| `apk add` 报签名不受信 | 属预期，加 `--allow-untrusted`；要免掉得用自己的密钥重签 |
| `opkg install` 报架构不匹配 | `opkg print-architecture` 与包名里的架构对比，按第 1 节的表重新打包 |
| 装了但进程起不来 | `logread -e wrtdeck`；常见原因是 `auth.token_env` 声明了但变量为空（会明确报错） |
| 局域网直连 `<设备IP>:8080` 打不开 | 默认只监听回环，属预期。走 LuCI 的「服务 → WrtDeck」，或按第 3 节把 `listen` 改成 `0.0.0.0:8080` |
| LuCI 里是一个空白框 | 面板页面没导出：`/etc/init.d/wrtdeck restart`，或手动 `/etc/init.d/wrtdeck export_web`；确认 `/www` 可写 |
| LuCI 里能打开但要求输口令 | 网关没就绪或 LuCI 会话没被认出来，见第 6 节「LuCI 内嵌与同源网关」的排障表 |
| 界面写「定时刷新」而不是「实时推送」 | 内嵌时的正常表现：不走 SSE 长连接，改走定时拉取 |
| 登录页提示「口令需先修改」，但改不动 | 默认口令尚未修改时**只有私网来源能登录**；确认浏览器与面板在同一网段或走回环 |
| 登录一直提示失败 | 可能已触发节流锁定，看 `logread -e wrtdeck \| grep '\[auth\]'`；锁定按指数退避，最长 1 小时 |
| 忘了登录口令 | 在设备上执行 `/etc/init.d/wrtdeck password` 重置 |
| 面板提示未授权但 Token 没错 | Token 从 `token` 命令重新取出，注意不要带多余空格；确认没有多余换行 |
| 浏览器提示证书不受信任 | 用的是自动生成的自签证书，手动确认即可；或按第 6 节换成自己的证书 |
| 信息源一直是 error | 面板卡片上的 detail 会给出具体原因；HTTP 类通常是设备不可达，MQTT 类通常是 broker 地址或账号问题 |
| 想临时绕过鉴权 | 配置里设 `auth.disabled: true` 后 `restart`，**仅限本机调试** |
