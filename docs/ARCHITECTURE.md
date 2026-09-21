# OpenWrt Lightweight Dashboard 结构设计

## 1. 项目定位

目标是在 ARM64 OpenWrt 上运行一个单进程、低内存、低磁盘占用的设备控制与信息 Dashboard。系统本身不预定义 ESP32、NAS、WOL 或具体 IoT 设备，而是通过统一的 **Registry 注册模型**动态增加“信息源”和“动作”。

信息源负责持续产生状态，例如注册：

```text
HTTP GET http://192.168.1.20/api/temp
每 5 秒执行
JSON 路径 temp
单位 °C
```

Dashboard 自动出现温度卡片。

动作负责执行操作，例如注册：

```text
UDP 192.168.1.20:9000
Payload = POWER_ON
需要确认
```

Dashboard 自动出现“开机”按钮。

最终结构：

```text
                    Browser
                       │
             Vue 3 + Tailwind CSS
                       │
              REST API + SSE
                       │
┌──────────────────────▼──────────────────────┐
│                 Go Service                  │
│                                             │
│ Registry ── Scheduler ── Executor           │
│    │                       │                │
│    │                Transport Layer         │
│    │          HTTP / TCP / UDP / MQTT       │
│    │                       │                │
│    └──────── State Cache ◄─┘                │
│                                             │
│            embed.FS Web Assets              │
└─────────────────────────────────────────────┘
                       │
          ESP32 / NAS / MQTT / LAN Device
```

系统不使用 Node 运行时、不使用数据库、不依赖 nginx；Vue 只在开发机上构建，生产环境最终只有一个 Go ELF 和少量配置文件。Go 官方 `embed.FS` 可以直接把整个静态资源目录编入二进制。

---

## 2. 技术栈

| 层         | 选择                     | 原因                                           |
| --------- | ---------------------- | -------------------------------------------- |
| Frontend  | Vue 3 + TypeScript     | Dashboard/动态表单开发效率高                          |
| Build     | Vite + pnpm            | 开发快，最终输出纯静态文件                                |
| CSS       | Tailwind CSS 4         | 构建期生成 CSS，无浏览器运行时                            |
| Component | Reka UI                | 只使用 Dialog / Select / Switch 等无样式 primitives |
| State     | Vue `ref/reactive`     | V1 不引入 Pinia                                 |
| Routing   | 不使用 Vue Router         | Dashboard / Registry 用轻量视图切换即可               |
| HTTP      | 浏览器原生 `fetch`          | 不引入 Axios                                    |
| Push      | 原生 `EventSource` / SSE | 比 WebSocket 更适合单向状态推送                        |
| Backend   | Go `net/http`          | 不引入 Gin/Echo/Fiber                           |
| Static UI | `embed.FS`             | Vue 构建产物直接进入 ELF                             |
| Auth      | 自研双凭据 + 内存会话      | 不引入 JWT / OAuth / bcrypt 等体系，零第三方依赖             |
| MQTT      | 自研 MQTT 3.1.1 客户端     | 零第三方依赖；只需 3.1.1 子集，连接池与重连自己掌控                |
| Storage   | JSON + RAM             | 不使用 SQLite                                   |
| Service   | OpenWrt procd          | 开机启动、respawn、日志                              |

MQTT 采用自研的 3.1.1 客户端（`internal/mqtt/`），自己维护连接池与重连恢复订阅：同一 broker 只保留一条长连接供多个注册项复用，而不是每次点击按钮重新连接。之所以不引入官方 SDK，是因为本项目只需要 3.1.1 的一个子集，而「零第三方依赖」能让 `go.mod` 只有 `module` 与 `go` 两行，交叉编译与包体积都更可控。

鉴权同理：只需要「口令 + 长期 Token」两种凭据与一个内存会话表，引入完整 Web 框架的 session/CSRF 体系反而更重。详见第 13 节。

前端原则是 **Tailwind 负责 90% UI，Reka UI 只负责难以正确手写的交互组件**。例如 Card、Button、Badge、Input 都自己写 Tailwind class；只有确认框、Select、Dropdown、Tooltip 等使用 Reka。这样不会把完整 UI 框架塞进 bundle。

---

## 3. 核心数据模型

整个系统只保留三个核心概念：

```text
Registry Entry
      │
      ├── Source       信息源
      │
      └── Action       动作
                │
                ▼
             Transport
      HTTP / TCP / UDP / MQTT / Exec
```

`Source` 与 `Action` 不各自实现协议，而是共同调用 Transport Layer。

建议统一注册结构：

```json
{
  "id": "room-temperature",
  "kind": "source",
  "name": "室内温度",
  "group": "ESP32",
  "enabled": true,

  "params": {},

  "transport": {
    "type": "http",
    "http": {
      "method": "GET",
      "url": "http://192.168.1.20/api/temp",
      "timeout_ms": 3000
    }
  },

  "schedule": {
    "interval_ms": 5000
  },

  "extract": {
    "type": "json",
    "path": "temp",
    "value_type": "number"
  },

  "ui": {
    "type": "metric",
    "unit": "°C",
    "precision": 1,
    "order": 10
  }
}
```

动作使用同一结构：

```json
{
  "id": "pc-power-on",
  "kind": "action",
  "name": "电脑开机",
  "group": "Computer",

  "transport": {
    "type": "udp",
    "udp": {
      "address": "192.168.1.50:9527",
      "payload": "POWER_ON",
      "encoding": "text"
    }
  },

  "ui": {
    "type": "button",
    "variant": "primary",
    "confirm": {
      "enabled": true,
      "title": "确认开机？",
      "message": "将向下位机发送开机指令。"
    }
  }
}
```

因此 Dashboard 本质只是：

```text
GET Registry
     ↓
Source → Metric / Status / Text Card
Action → Button / Parameter Button
```

前端不需要知道 UDP、MQTT 或 HTTP 怎么执行。

---

## 4. 参数与模板系统

所有 Transport 字段允许引用参数：

```text
${params.device_ip}
${params.port}
${params.channel}
${secret.mqtt_password}
```

例如：

```json
{
  "params": {
    "angle": {
      "type": "number",
      "required": true,
      "min": 0,
      "max": 180,
      "label": "舵机角度"
    }
  },

  "transport": {
    "type": "udp",
    "udp": {
      "address": "192.168.1.20:9000",
      "payload": "SERVO:${params.angle}"
    }
  }
}
```

Dashboard 自动根据 `params` 生成输入框：

```text
舵机角度
[       90       ]

[ 执行 ]
```

点击后：

```http
POST /api/v1/actions/servo/run

{
  "params": {
    "angle": 90
  }
}
```

服务端完成校验和模板渲染，然后才交给 UDP Transport。

模板系统刻意保持简单，不支持循环、条件表达式、JavaScript 或 Go Template 函数，只允许变量替换以及少数安全过滤器，例如：

```text
${params.name}
${params.query|url}
${params.text|json}
```

这样既减少实现复杂度，也避免注册配置演变成脚本执行系统。

参数建议支持：

```text
string | number | boolean | select | secret
```

参数同时具有 `required/default/min/max/options` 等约束。注册时就检查模板引用是否存在，执行时再次校验用户参数。

---

## 5. Transport Layer

所有协议通过统一 Executor 接口进入：

```text
                 Executor
                    │
      ┌─────────────┼─────────────┐
      ▼             ▼             ▼
    HTTP           UDP           TCP
      │
      └─────────────┬─────────────┐
                    ▼             ▼
                   MQTT          Exec
```

HTTP 支持 method、URL、headers、body、timeout；TCP 支持 address、payload、连接/读取超时以及可选单次响应；UDP 支持 address、payload 和可选等待响应；MQTT 支持 broker、topic、QoS、retain、publish 和 subscribe；Payload 统一允许 `text / json / hex / base64`。

`Exec` 建议存在，但作为 **默认关闭的高级 Transport**：

```json
{
  "type": "exec",
  "exec": {
    "executable": "/usr/bin/ubus",
    "args": [
      "call",
      "system",
      "board"
    ]
  }
}
```

禁止：

```text
/bin/sh -c "..."
```

也不把整个命令字符串交给 shell。只执行 `executable + argv[]`，并通过 daemon 配置设置 executable allowlist。这样以后可以安全一点地注册 `ubus`、`uci` 或自定义程序，同时避免模板参数直接形成 shell injection。

---

## 6. Source 调度与状态模型

Source 不由浏览器自己定时请求设备，而由 Go Scheduler 执行：

```text
Scheduler
   │
   │ every 5s
   ▼
HTTP GET ESP32
   │
   ▼
Extract / Convert
   │
   ▼
State Cache
   │
   ├── latest value
   ├── last success
   ├── latency
   ├── status
   └── last error
   │
   ▼
SSE → Browser
```

每个 Source 的运行状态统一表示为：

```json
{
  "id": "room-temperature",
  "status": "ok",
  "value": 25.6,
  "updated_at": "2026-09-21T13:40:25+08:00",
  "latency_ms": 31,
  "error": null
}
```

状态只有：

```text
unknown → running → ok
                  ↘ error
                  ↘ stale
```

调度器使用一个全局 scheduler + 小型 worker pool，而不是无限创建后台任务。默认可以设：

```text
source workers = 4
action workers = 4
minimum interval = 1s
maximum response body = 256 KiB
default timeout = 5s
```

同一个 Source 上一次还未完成时，不重复启动下一次执行。

所有实时结果只存在 RAM；重启后重新采集。因此高频数据不会写 flash。

---

## 7. 数据提取

信息源执行完成后经过 Extractor：

```text
Response
   │
   ├── raw
   ├── JSON path
   └── regex
       │
       ▼
string / number / bool
       │
       ▼
Dashboard State
```

例如 HTTP 返回：

```json
{
  "status": "ok",
  "sensor": {
    "temperature": 26.37
  }
}
```

注册：

```json
{
  "extract": {
    "type": "json",
    "path": "sensor.temperature",
    "value_type": "number"
  }
}
```

即可得到：

```text
26.37
```

V1 不引入 JSONPath/JMESPath 库，而实现简单的：

```text
sensor.temperature
network.clients.0.name
```

路径解析即可。

这样可以显著减少依赖和二进制大小。

---

## 8. MQTT 特殊处理

MQTT 与 HTTP/UDP 最大区别是它可以主动推送，因此支持两种形式：

```text
Action
MQTT Publish
     │
     └── button → publish(topic, payload)

Source
MQTT Subscribe
     │
     └── message → extract → State Cache → SSE
```

Broker 单独维护连接池：

```text
mqtt://192.168.1.2:1883
             │
        one connection
             │
      ┌──────┼──────┐
      ▼      ▼      ▼
   source1 action1 action2
```

同一 broker 不重复创建连接。没有 MQTT Registry 时则完全不建立 MQTT 连接。

---

## 9. Backend API

API 保持很小：

| API                                 | 用途                           |
| ----------------------------------- | ---------------------------- |
| `GET /api/v1/health`                | 服务健康检查，唯一免鉴权接口               |
| `POST /api/v1/session/login`        | 口令换会话凭据                      |
| `GET /api/v1/session/me`            | 当前凭据身份与待改口令标记                |
| `POST /api/v1/session/password`     | 修改口令（清空全部会话并换发新凭据）           |
| `POST /api/v1/session/logout`       | 注销当前会话凭据                     |
| `POST /api/v1/session/handoff`      | 仅回环可调，签发一次性交接码           |
| `POST /api/v1/session/redeem`       | 交接码换会话凭据，要求同源                |
| `GET /api/v1/dashboard`             | 当前所有 Dashboard Entry + State |
| `GET /api/v1/registry`              | 获取注册信息                       |
| `PUT /api/v1/registry/{id}`         | 注册或更新 Source/Action          |
| `DELETE /api/v1/registry/{id}`      | 删除注册项                        |
| `POST /api/v1/actions/{id}/run`     | 执行动作                         |
| `POST /api/v1/sources/{id}/refresh` | 手动刷新信息源                      |
| `GET /api/v1/events`                | SSE 实时更新                     |

除 `health` 与 `login` / `redeem` 外，所有接口都需要凭据：外部程序用 `Authorization: Bearer <API Token>`，浏览器用会话凭据。SSE 无法自定义请求头，因此事件流允许用 `?token=` 查询参数传递凭据。

外部程序注册时推荐使用：

```http
PUT /api/v1/registry/room-temperature
```

而不是只能 POST 自动生成 ID。

这样 ESP32 管理程序或部署脚本可以重复执行注册操作，天然具备幂等性。

---

## 10. Frontend

前端只有两个主视图：

```text
┌────────────────────────────────────────────┐
│ OpenWrt Dashboard                          │
├────────────────────────────────────────────┤
│ Dashboard                     Registry     │
├────────────────────────────────────────────┤
│ 25.6 °C       ESP32 Online    [电脑开机]   │
│                                             │
│ Router CPU     NAS Status      [舵机控制]   │
└────────────────────────────────────────────┘
```

Dashboard 完全根据 Registry 动态生成，不为某一种设备写专用页面。

建议组件结构：

```text
App
├── DashboardView
│   ├── MetricCard
│   ├── StatusCard
│   ├── ActionButton
│   └── ParameterDialog
│
└── RegistryView
    ├── EntryTable
    └── EntryEditor
```

这里不需要 Pinia；`App.vue` 持有 Registry 和 State，SSE 更新状态即可。

也暂时不需要 Vue Router；使用简单的当前视图状态：

```ts
const page = ref<'dashboard' | 'registry'>('dashboard')
```

确认窗口和参数窗口使用 Reka Dialog，其余组件直接 Vue + Tailwind。

---

## 11. SSE 而不是 WebSocket

浏览器首次：

```text
GET /api/v1/dashboard
```

获取完整状态，之后建立：

```text
GET /api/v1/events
```

服务器发送：

```text
event: source.updated
data: {...}

event: action.finished
data: {...}

event: registry.changed
data: {...}
```

这里浏览器主要是接收状态更新，双向实时通信需求很弱，因此 SSE 比 WebSocket 更简单，不需要额外库，同时浏览器原生支持自动重连。

---

## 12. 配置与持久化

不使用 SQLite。

```text
/etc/wrtdeck/
├── config.json
├── registry.json
├── secrets.json          口令散列 + API Token（mode 0600）
└── tls-self-signed.crt   TLS 启用且未自带证书时自动生成

/tmp/wrtdeck/
└── runtime
```

`registry.json` 只有注册、修改、删除时才写入，因此不会因为每 5 秒读取一次温度而磨损 flash。

登录会话、运行状态全部放内存，重启即丢。

配置更新采用：

```text
write temp
    ↓
fsync
    ↓
rename
```

避免断电把 registry 写坏。

Secrets 和 Registry 分离，API 返回 Registry 时绝不返回 secret 原值。

---

## 13. 安全模型

面板可能控制真实设备甚至执行系统命令，而 OpenWrt 设备常被挂到公网做端口映射，因此安全模型按 **“可能暴露在公网”** 设计，而不是默认“只在内网”。服务仍然不主动开 WAN firewall rule，暴露与否由使用者决定；但一旦暴露，凭据与响应头都已有兜底。

### 双凭据

使用两套互不干扰的凭据，各自服务一类调用方：

| 凭据 | 形态 | 生命周期 | 调用方 |
| --- | --- | --- | --- |
| **登录口令** | 用户记忆的字符串 | 长期，可随时改 | 人在浏览器里登录 |
| **API Token** | 43 字符随机串 | 长期，落盘或取自环境变量 | 脚本、设备本地、自动化 |

登录口令经 **PBKDF2-HMAC-SHA256（210000 轮 + 16 字节随机盐）** 派生后存储，`secrets.json` 里只有算法、轮数、盐与散列，**没有明文**：

```json
{
  "password": {
    "algo": "pbkdf2-sha256",
    "iterations": 210000,
    "salt": "…",
    "hash": "…"
  },
  "must_change_password": true,
  "api_token": "…"
}
```

首次启动写入默认口令 `admin` 并置 `must_change_password`。**在口令被改掉之前**：

- 只有私网（RFC1918）与回环来源能发起登录，公网来源一律拒绝；
- 换发的会话凭据只能调用 `me` / `password` / `logout`，其余 API 全部返回受限错误。

这样默认口令即使被公网扫描器扫到也无法利用，而设备主人在 LAN 内可以正常完成首改。

### 会话

口令校验通过后换发**短期会话凭据**：

```text
POST /api/v1/session/login   { password }  →  { token, must_change_password, … }
```

- 43 字符随机串，默认 **TTL 12 小时**（`auth.session_ttl_minutes`）；
- **只驻内存**，进程重启即全部失效，因此不写闪存、也无从落盘窃取；
- 并发上限 64，超出后淘汰最旧的一条；
- 改口令成功时**清空全部会话**，并给发起改密的浏览器换发新凭据。

浏览器端凭据默认写 `sessionStorage`，只有用户勾选「记住」才落到 `localStorage`。这让「关掉标签页即登出」成为默认行为。

### 防爆破

单靠口令长度不足以对抗公网扫描，因此登录接口叠加两层节流：

```text
每来源：失败 N 次（默认 5）→ 锁定 15 分钟
                              ↓ 继续失败
                        翻倍 → 30 → 60 分钟（上限 1 小时）
                        成功登录即清零

全局：50 次失败 / 分钟 → 所有来源一起拒绝
```

关键设计：**来源判定只取 TCP 对端地址，刻意忽略 `X-Forwarded-For`**。否则攻击者只要每请求换一个伪造头，就能把「每来源」节流稀释成无效。代价是前置反向代理时所有请求看起来同源，此时应改用全局限流参数而非放开信任头。

锁定时长也**不落盘**，进程重启即重置——避免攻击者靠重启服务来固化自己的锁，也避免合法用户被持久性锁死。

### 响应头与传输

面板按静态资源站点 + JSON API 的形态加固：

| 响应头 | 取值 | 目的 |
| --- | --- | --- |
| `Content-Security-Policy` | `default-src 'self'`；`script-src 'self'`；`object-src 'none'`；`frame-ancestors` 放行同源与 `self` | 杜绝 XSS 与点击劫持，同时允许被 LuCI 内嵌 |
| `X-Content-Type-Options` | `nosniff` | 防止 MIME 嗅探导致的脚本执行 |
| `Referrer-Policy` | `no-referrer` | 避免把面板地址泄漏给外部站点 |
| `Permissions-Policy` | 关闭摄像头/麦克风/定位等 | 面板不需要任何设备能力 |
| `Cache-Control` | `/api/` 一律 `no-store` | 凭据与状态不进磁盘缓存 |
| `Strict-Transport-Security` | **仅在 TLS 下** | 不在明文部署上下发，避免把用户锁在无法访问的 HTTPS 上 |

CSP 的 `script-src 'self'` 意味着**页面不能有内联脚本**，所以主题初始化这类首帧前必须执行的逻辑单独放 `public/theme-init.js`，用外链引入。

TLS 可选但对公网暴露是强建议：

```text
自带证书：tls.cert_file / tls.key_file
自动自签：ECDSA P-256，10 年有效，落盘 data_dir，重启复用
明文跳转：tls.redirect_listen 额外监听，308 永久重定向到 HTTPS
```

启动日志会打印证书 SHA-256 指纹，便于人工核对。

### 一跳交接（LuCI）

LuCI 薄壳不该让用户再输一次口令，也不该把长期 Token 塞进 URL。因此引入**一次性交接码**：

```text
LuCI 前端 → rpcd /wrtdeck.handoff
              ↓ 仅回环地址可调用
          POST /api/v1/session/handoff  →  { code }
              ↓ 交给浏览器（URL 或 postMessage）
          POST /api/v1/session/redeem   →  { token }   （要求同源）

code：单次使用、60 秒过期、用后即焚
```

交接码与 Token 的本质区别是**它只能换一次、一分钟就死**，即使泄漏到浏览器历史或 Referer 也无长期价值。接口只接受回环来源，因此公网无法自行签发。

### 其余边界

`Exec` 默认关闭；HTTP/TCP/UDP/MQTT 默认允许；HTTP Response、TCP Response、API Body 全部设置大小上限；所有网络调用必须设置 deadline。

审计日志统一带 `[auth]` 前缀，记录**来源与结果**（成功/失败/锁定/改密），**绝不写入口令、Token 或交接码**，避免日志本身成为凭据泄漏渠道。

---

## 14. Go 工程结构

```text
wrtdeck/
├── cmd/
│   └── wrtdeck/
│       └── main.go
│
├── internal/
│   ├── api/
│   │   ├── server.go
│   │   ├── middleware.go      鉴权、安全响应头
│   │   ├── session.go         登录、改密、交接
│   │   ├── session_store.go   内存会话表
│   │   ├── throttle.go        登录节流与锁定
│   │   └── sse.go
│   ├── certs/                 TLS 证书准备（自带 / 自签）
│   ├── config/
│   │   ├── config.go
│   │   ├── secrets.go         Token 与口令落盘
│   │   └── password.go        PBKDF2 散列与强度校验
│   ├── registry/
│   ├── engine/
│   │   ├── executor.go
│   │   └── scheduler.go
│   ├── transport/
│   │   ├── http.go
│   │   ├── tcp.go
│   │   ├── udp.go
│   │   ├── mqtt.go
│   │   └── exec.go
│   ├── template/
│   ├── extract/
│   ├── state/
│   └── webui/
│       ├── embed.go
│       └── dist/
│
├── web/
│   ├── src/
│   │   ├── lib/
│   │   │   ├── auth.ts        登录状态机
│   │   │   ├── token.ts       凭据存储
│   │   │   ├── handoff.ts     一次性交接
│   │   │   └── theme.ts
│   │   └── views/
│   │       └── LoginView.vue
│   ├── public/
│   │   └── theme-init.js      CSP 要求脚本外置
│   ├── package.json
│   ├── pnpm-lock.yaml
│   └── vite.config.ts
│
├── packaging/
│   ├── openwrt/               procd 脚本 + apk / ipk 控制文件
│   └── luci/                  luci-app-wrtdeck 薄壳（rpcd + 菜单 + 视图）
│
├── scripts/                   构建、打包与校验脚本（兼容 macOS bash 3.2）
├── Makefile
└── go.mod
```

Vite 直接把产物输出到：

```text
internal/webui/dist/
```

所以：

```go
//go:embed dist/*
var content embed.FS
```

即可嵌入。`embed.FS` 可以直接交给 `net/http` 的静态文件服务。

---

## 15. 构建

Frontend：

```bash
pnpm install --frozen-lockfile
pnpm build
```

Tailwind CSS 4 使用官方 Vite 插件：

```text
tailwindcss
@tailwindcss/vite
```

其 CSS 在构建阶段生成，浏览器不存在 Tailwind runtime。

Go：

```bash
CGO_ENABLED=0 \
GOOS=linux \
GOARCH=arm64 \
go build \
  -trimpath \
  -ldflags="-s -w -buildid=" \
  -o dist/wrtdeck \
  ./cmd/wrtdeck
```

对于 OpenWrt ARM64，这是最优先的模式。

因为：

```text
CGO_ENABLED=0
```

得到的纯 Go 程序不依赖目标机器的 musl 动态库，因此实际上比“专门链接 musl”更省事：

```text
Linux ARM64 ELF
      │
      └── no libc dependency
             │
             └── OpenWrt musl ✓
```

以后如果引入必须使用 CGO 的库，再切换到 OpenWrt SDK：

```text
CC=aarch64-openwrt-linux-musl-gcc
CGO_ENABLED=1
```

V1 应坚持 `CGO_ENABLED=0`。

---

## 16. 体积与内存约束

项目设计时直接把资源预算作为约束：

| 项目                  |            建议目标 |
| ------------------- | --------------: |
| Go ELF + Web UI     |   尽量 ≤ 10–15 MB |
| Frontend gzip       | 尽量 ≤ 200–300 KB |
| 常驻 RSS              |      尽量 ≤ 20 MB |
| Source Worker       |               4 |
| Action Worker       |               4 |
| HTTP Response Limit |         256 KiB |
| API Request Limit   |          64 KiB |
| Runtime History     |           默认不保存 |

不引入：

```text
Gin / Echo / Fiber
SQLite
Axios
Pinia
Vue Router
大型 UI Framework
Chart.js / ECharts
JSONPath Engine
JavaScript Expression Engine
```

除非以后确实出现需求。

也不建议默认使用 UPX。减小 flash 占用的收益有限，而原始 ELF 在启动、调试、崩溃定位和兼容性方面更直接。

---

## 17. 最终核心执行链

整个项目可以归结为两条链：

```text
SOURCE

Registry
   ↓
Scheduler
   ↓
Template
   ↓
Transport
   ↓
Extractor
   ↓
State Cache
   ↓
SSE
   ↓
Dashboard
```

以及：

```text
ACTION

Dashboard Button
   ↓
Runtime Parameters
   ↓
Confirmation
   ↓
POST /run
   ↓
Validation
   ↓
Template
   ↓
Transport
   ↓
Result
   ↓
SSE / Toast
```

底层协议与 UI 完全解耦。

因此以后增加：

```text
WebSocket
Serial
GPIO
ubus
Wake-on-LAN
SSH
Modbus
BLE
```

原则上只需要增加新的 Transport，而不需要重新设计 Dashboard、Registry、模板参数和调度系统。

这应该作为 V1 的核心架构边界：

**Registry 描述“做什么”，Template 注入“运行时数据”，Transport 负责“怎么通信”，Source/Action 决定“何时执行”，Dashboard 只负责“怎么显示”。**
