# WrtDeck 快速安装

面向 ARM64 / MIPS 等 OpenWrt 设备的安装说明。全程只需要一条 `opkg` 命令，二进制为静态链接，不依赖设备上的 libc 版本。

- 架构与设计：[ARCHITECTURE.md](./ARCHITECTURE.md)
- 从源码构建：[从源码构建 ipk](#从源码构建-ipk)

## 1. 前提

| 项目 | 要求 |
| --- | --- |
| 系统 | OpenWrt 21.02 及以上（procd + opkg） |
| 架构 | 见下表，默认包为 `aarch64_cortex-a53` |
| 存储 | 约 8 MB（二进制 7.3 MB + 配置） |
| 内存 | 常驻约 8–12 MB，随注册项数量增长 |
| 依赖 | `ca-bundle`（HTTPS 设备需要），安装时由 opkg 自动处理 |

包的架构字段必须与设备一致，可用 `uname -m` 或 `opkg print-architecture` 确认：

| 设备类型 | `opkg print-architecture` 示例 | 打包时的参数 |
| --- | --- | --- |
| ARMv8 路由器（多数 AX/AC 机型） | `aarch64_cortex-a53` | `GOARCH=arm64` |
| ARMv7 老机型 | `arm_cortex-a7` | `GOARCH=arm` |
| x86 软路由 | `x86_64` | `GOARCH=amd64` |
| MIPS 小闪存机型 | `mipsel_24kc` | `GOARCH=mipsle` |

## 2. 安装

把 ipk 传到设备上（`scp`、U 盘或局域网共享均可），然后：

```sh
opkg update
opkg install ./owdash_1.0.0-1_aarch64_cortex-a53.ipk
```

`postinst` 会自动 `enable` 并 `start` 服务，安装成功后会打印访问地址与 Token 查看方式。

安装动作与设备文件的变化：

```text
/usr/bin/owdash              静态 ELF，7.3 MB
/etc/init.d/owdash           procd 启动脚本
/etc/owdash/config.json      默认配置（conffiles 声明，升级不覆盖）
```

## 3. 首次登录

服务默认监听 `0.0.0.0:8080`，OpenWrt 默认防火墙对 LAN 放行 input，因此同网段浏览器直接访问即可：

```text
http://<设备IP>:8080
```

首次启动会生成 43 字符的随机 Token，写入 `/etc/owdash/secrets.json`（权限 0600）。用下面任一方式取出：

```sh
/etc/init.d/owdash token                                  # 推荐
/usr/bin/owdash -config /etc/owdash/config.json -print-token
cat /etc/owdash/secrets.json
```

在面板右上角「设置 Token」里填入，Token 会保存在浏览器 localStorage。

## 4. 服务管理

```sh
/etc/init.d/owdash start       # 启动
/etc/init.d/owdash stop        # 停止
/etc/init.d/owdash restart     # 重启（配置改动后）
/etc/init.d/owdash reload      # 等价于 restart
/etc/init.d/owdash enable      # 开机自启
/etc/init.d/owdash disable     # 取消自启
/etc/init.d/owdash token       # 打印当前 API Token
```

进程由 procd 托管，异常退出后按 5 秒间隔最多重启 5 次（1 小时后计数重置）；文件描述符上限提到 1024。

日志走 syslog：

```sh
logread -e owdash -f
```

## 5. 配置

`/etc/owdash/config.json`。它被声明为 conffiles，`opkg upgrade` 不会覆盖你改过的内容。

```json
{
  "listen": "0.0.0.0:8080",
  "data_dir": "/etc/owdash",
  "auth": {},
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
| `listen` | 只在本机访问可改成 `127.0.0.1:8080`，再前置 Nginx |
| `data_dir` | 注册表、密钥、配置的落盘目录，改完要同步改 `/etc/init.d/owdash` 里的 `CONF_DIR` |
| `auth.disabled` | 设为 `true` 关闭鉴权，**仅限本机调试** |
| `auth.token_env` | 从环境变量读 Token，不再落盘。见下节 |
| `limits.history_limit` | 每个信息源在内存里保留的采样条数，`0` 表示关闭。**只占内存，不写闪存** |
| `limits.min_interval_ms` | 信息源轮询间隔下限，防止注册项把设备拖垮 |
| `exec.enabled` | Exec 传输总开关，默认关闭 |
| `limits.max_body_bytes` | 单次响应/载荷长度上限，默认 256 KB |

改完配置执行 `/etc/init.d/owdash restart` 生效。**注册项不用重启**，通过 API 或面板改动即时生效。

### 用环境变量提供 Token

不想让 Token 落盘时，在 `/etc/init.d/owdash` 的 `start_service()` 里导出变量，并在配置中声明变量名：

```json
{ "auth": { "token_env": "OWDASH_TOKEN" } }
```

```sh
# /etc/init.d/owdash
export OWDASH_TOKEN='你的Token'
```

注意：**声明了 `token_env` 却没有该环境变量时进程会拒绝启动并给出明确报错**，不会静默退回随机 Token。这是刻意设计——否则运维会以为环境变量已生效，实际鉴权用的是另一个谁也不知道的值。这种模式下 `secrets.json` 不会被创建，Token 也无法在运行时轮换。

### 闪存磨损

设计上避免频繁写闪存：

- 运行状态（各信息源的当前值、运行历史）**只驻内存**，重启即丢；
- `registry.json` 只在注册表真正发生变化时写盘；
- `config.json`、`secrets.json` 只在首次生成或配置变更时写盘。

## 6. 接入第一台设备

服务本身不预置任何设备，全部通过注册表 API 或面板添加。下面三个例子覆盖三类最常见的接法。

### HTTP 信息源：每 5 秒读一次温度

```sh
TOKEN='<你的Token>'
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

## 7. 升级与卸载

```sh
opkg install ./owdash_1.0.1-1_aarch64_cortex-a53.ipk   # 升级
opkg remove owdash                                     # 卸载
```

- 升级时 `/etc/owdash/config.json` 因 conffiles 声明而保留你的改动；`postinst` 会 `restart` 服务而不是重新 `enable`。
- `registry.json` 与 `secrets.json` 不在包内，升级和卸载都不会被清除。
- 卸载后如需彻底清理：`rm -rf /etc/owdash`。

## 从源码构建 ipk

需要 Go 1.23+、Node 18+ 与 pnpm（脚本会自动探测 `go`，或尊重 `GO_BIN`）。

```sh
make ipk                                            # 默认 arm64 / aarch64_cortex-a53
GOARCH=arm     make ipk                             # ARMv7
GOARCH=amd64   make ipk                             # x86_64
GOARCH=mipsle  make ipk                             # MIPS 小端
VERSION=1.2.0 PKG_RELEASE=2 make ipk                # 指定版本
PKG_ARCH=自定义 make ipk                            # 显式指定架构名
```

产物在 `dist/owdash_<版本>-<发布号>_<架构>.ipk`。

打包过程不依赖 OpenWrt SDK：ipk 本身就是 `ar` 归档（`debian-binary` + `control.tar.gz` + `data.tar.gz`），脚本用系统自带的 `ar`/`tar` 直接组装，同时做两件事：

1. 交叉编译 `CGO_ENABLED=0` 的静态 ELF，并断言产物确实是 `statically linked`；
2. 把 tar 内的属主统一写成 `root:root`，避免在 macOS 上打包时 gid 0 被解析成 `wheel`。

## 打包测试

```sh
make check-ipk     # 结构校验：归档格式、control 字段、权限、属主、ELF 属性、init 契约
make test-ipk      # 结构校验 + 安装模拟
```

`make test-ipk` 会解包 ipk 到临时目录，把 procd 的 `procd_*` 函数替换成记录参数的桩，执行 init 脚本的 `start_service()`，然后用它下发的命令真实把服务跑起来，验证：

- 服务能监听就绪，`/api/v1/health` 与面板页面正常返回；
- 首启生成 `secrets.json`，未带 Token 返回 401，带 `/etc/init.d/owdash token` 输出的 Token 返回 200；
- 空注册表启动时不写 `registry.json`，写入注册项后才落盘；运行状态不落盘；
- 启动日志为生产形态，不带开发模式提示。

两处必要的模拟已在该脚本头部注明：开发机上没有 procd，且交叉编译出的 Linux ELF 无法在开发机执行（跑的是同源码的本机产物，包内 ELF 的静态链接与目标架构由 `make check-ipk` 单独断言）。

> 已验证范围：ipk 结构、init 脚本行为、启动与鉴权链路、数据落盘位置、MQTT 与真实 broker 的互操作。**尚未在真实 OpenWrt 设备上安装**（构建环境没有可用的设备、QEMU 或容器），首次上真机时请留意第 2 节的架构匹配与第 3 节的防火墙放行。

## 排障

| 现象 | 排查方向 |
| --- | --- |
| `opkg install` 报架构不匹配 | `opkg print-architecture` 与包名里的架构对比，按第 1 节的表重新打包 |
| 装了但进程起不来 | `logread -e owdash`；常见原因是 `auth.token_env` 声明了但变量为空（会明确报错） |
| 浏览器打不开 | `netstat -lntp \| grep 8080` 确认监听；确认访问来自 LAN 区，必要时检查 `/etc/config/firewall` |
| 面板一直提示未授权 | Token 从 `secrets.json` 或 `token` 命令重新取出，注意不要带多余空格 |
| 信息源一直是 error | 面板卡片上的 detail 会给出具体原因；HTTP 类通常是设备不可达，MQTT 类通常是 broker 地址或账号问题 |
| 想临时绕过鉴权 | 配置里设 `auth.disabled: true` 后 `restart`，**仅限本机调试** |
