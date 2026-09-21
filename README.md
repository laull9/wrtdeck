<p align="center">
  <img src="./web/src/assets/logo.png" alt="WrtDeck" width="120" />
</p>

# WrtDeck

WrtDeck 是一个运行在 OpenWrt 上的轻量级设备控制面板，用统一配置把 HTTP、TCP、UDP、MQTT 等设备状态与控制命令动态变成 Dashboard 卡片和操作按钮。

- 架构设计：[docs/ARCHITECTURE.md](./docs/ARCHITECTURE.md)
- 安装部署：[docs/INSTALL.md](./docs/INSTALL.md)
- 协作约定：[AGENTS.md](./AGENTS.md)

> 站点图标与界面 logo 都由 `assets/WrtDeck.png` 生成，换图标执行 `make icons`。

## 当前进度

项目已完成端到端闭环：Go 后端、Vue 前端、SSE 实时通道、注册表热更新、全部五种传输、口令 + Token 双凭据认证、OpenWrt apk / ipk 双格式打包与打包测试均可运行。

| 能力 | 状态 |
| --- | --- |
| Registry 注册表（JSON 持久化 + 原子写） | 可用 |
| Scheduler 调度采集（worker pool、跳过未完成的上一轮、stale 降级） | 可用 |
| Transport：HTTP / TCP / UDP / Exec | 可用 |
| Transport：MQTT（3.1.1，自研实现，零第三方依赖） | 可用 |
| MQTT 订阅型信息源（推送驱动 + 连接池 + 断线重连恢复订阅） | 可用 |
| Template 变量替换与 url/json 过滤器 | 可用 |
| Extractor：raw / json 点分路径 / regex | 可用 |
| State Cache + SSE 推送 | 可用 |
| 口令 + Token 双凭据认证（PBKDF2 口令、12h 会话、强制首改） | 可用 |
| 登录防爆破（每来源节流 + 指数退避锁定 + 全局限流） | 可用 |
| 公网暴露加固（严格 CSP、安全响应头、可选 TLS、明文 308 重定向） | 可用 |
| LuCI 薄壳一跳登录（一次性交接码，不回传 Token） | 可用 |
| 运行历史（内存环形缓冲，不写闪存） | 可用 |
| Vue 3 + Tailwind 4 + Reka UI 前端 | 可用 |
| 深浅双主题（跟随系统 + 右上角手动切换） | 可用 |
| procd 启动脚本与 apk / ipk 双格式打包 | 可用，附结构校验与安装模拟 |

## 环境要求

- Go 1.24+（脚本会自动探测 `/opt/homebrew/bin/go`、`/usr/local/go/bin/go`）
- Node 18+，pnpm（没有全局 pnpm 时会退回 `corepack pnpm`）
- 前端依赖走 npmmirror 镜像（见 `web/.npmrc`）

## 快速开始

```bash
make dev
```

后端监听 `127.0.0.1:8080`，前端开发服务器在 `127.0.0.1:5173`，`/api` 由 Vite 代理到后端。浏览器打开 http://127.0.0.1:5173 即可。

首次启动会写入 5 条自检示例注册项（`demo-*`），它们请求本服务自身的 health 接口，因此不需要任何外部设备就能看到完整的「调度 → 传输 → 提取 → 状态 → SSE → 卡片」链路。

## 常用命令

```bash
make help           # 查看全部目标
make dev            # 前后端并行开发
make dev-backend    # 只启动后端（dev 模式，关闭鉴权）
make dev-frontend   # 只启动前端
make web            # 只构建前端到 internal/webui/dist
make icons          # 从 assets/WrtDeck.png 重新生成站点图标与界面 logo
make build          # 前端 + 本机后端 → dist/owdash
make release        # 交叉编译 OpenWrt linux/arm64 静态 ELF

make apk            # 打包 apk（OpenWrt 25.12 起的默认格式）
make ipk            # 打包 ipk（24.10 及更早版本）
make luci           # 打包 luci-app-wrtdeck 薄壳（apk + ipk 一次出）
make packages       # 面板与薄壳的四种产物一次性打全

make check-apk      # 校验 apk 内部结构（段边界、签名、逐文件校验和）
make check-ipk      # 校验 ipk 内部结构
make check-luci     # 校验薄壳结构 + 与面板前端的交接契约
make test-apk       # apk 完整打包测试：结构校验 + 安装模拟运行
make test-ipk       # 同上，针对 ipk
make test-pkg       # 上面两项都跑一遍

make check          # go vet + 前端类型检查
make clean          # 清理构建产物
```

## 运行模式

**开发模式**（`-dev`）：关闭鉴权、放行跨域、监听回环地址、写入示例注册项。

**生产模式**：启用 **口令 + Token 双凭据**。

- **登录口令**：首次启动生成默认口令 `admin`，服务日志与安装提示都会明确告知。浏览器打开面板即进入登录页，用 `admin` 登录后会**强制要求修改口令**，改完才能进入面板。
- **API Token**：首启同时生成 43 字符随机 Token，写入 `secrets.json`（权限 0600），供脚本、设备本地调用或面板的「用 Token 登录」入口使用。

```bash
./dist/owdash -config /etc/owdash/config.json
./dist/owdash -print-token                 # 打印当前 API Token
./dist/owdash -reset-password -            # 从 stdin 读新口令并重置（- 可换成直接写值）
```

口令用 PBKDF2-HMAC-SHA256（210000 轮、16 字节盐）存储，登录成功后换发**短期会话凭据**（43 字符随机、TTL 默认 12 小时、驻内存）。会话凭据默认只存在浏览器会话存储里，勾选「记住」才落到 localStorage。

按「可能暴露在公网」加固的部分：

| 措施 | 说明 |
| --- | --- |
| 强制首改口令 | 未改口令前**只有私网/回环来源能登录**，且会话凭据只能调 `me` / `password` / `logout` |
| 登录节流 | 同一来源连续失败达 `auth.max_login_attempts`（默认 5）后锁定，锁定时长按指数退避从 15 分钟翻倍至 1 小时上限；另有 50 次/分钟的全局限流兜底 |
| 严格 CSP | `default-src 'self'`，`script-src 'self'`（无内联脚本）、`frame-ancestors` 放行同源与 LuCI |
| 安全响应头 | `X-Content-Type-Options: nosniff`、`Referrer-Policy: no-referrer`、`Permissions-Policy` 收窄；`/api/` 一律 `Cache-Control: no-store` |
| HSTS | 仅在启用 TLS 时下发 |
| 可选 TLS | 自带证书或自动生成 ECDSA P-256 自签证书（10 年有效，落盘 `data_dir`）；启用后另起明文监听并 308 重定向 |
| 反节流绕过 | 来源判定**刻意忽略 `X-Forwarded-For`**，避免攻击者伪造头绕过登录锁定 |

审计日志只记录来源与结果（`[auth]` 前缀），**绝不写入口令、Token 或交接码**。

## 界面主题

界面有深色与浅色两套配色，右上角最右侧的月亮 / 太阳按钮用于切换。

- **默认跟随系统**：没有手动切过时，`prefers-color-scheme` 变化会实时生效。
- **手动切换后固定**：点击按钮会把选择写入 `localStorage` 的 `owdash.theme`，此后不再跟随系统。想恢复跟随系统，清掉该键即可。
- **不闪屏**：主题在首帧前由 `public/theme-init.js` 定下来，刷新不会先亮后暗。它单独成文件而**不写成内联脚本**，是为了配合 CSP 的 `script-src 'self'`。
- **配色集中在一处**：组件里只写 `bg-surface` / `text-ink-body` 这类语义名，实际色值集中在 `web/src/style.css` 的 `:root` 与 `:root.dark` 两块，新增主题不需要改组件。

## API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/health` | 健康检查，不需要鉴权 |
| POST | `/api/v1/session/login` | 用登录口令换会话凭据（未改口令时仅私网来源可用） |
| GET | `/api/v1/session/me` | 当前凭据身份与是否待改口令 |
| POST | `/api/v1/session/password` | 修改登录口令（改后全部会话失效并换发新凭据） |
| POST | `/api/v1/session/logout` | 注销当前会话凭据 |
| POST | `/api/v1/session/handoff` | 仅回环可调，签发一次性交接码（供 LuCI 一跳登录） |
| POST | `/api/v1/session/redeem` | 用一次性交接码换会话凭据，要求同源 |
| GET | `/api/v1/dashboard` | 全部信息源状态 + 动作定义 |
| GET | `/api/v1/registry` | 注册表全量 |
| PUT | `/api/v1/registry/{id}` | 注册或更新（幂等，ID 取自路径） |
| DELETE | `/api/v1/registry/{id}` | 删除注册项 |
| POST | `/api/v1/actions/{id}/run` | 执行动作 |
| POST | `/api/v1/sources/{id}/refresh` | 手动刷新信息源（订阅型返回 409） |
| GET | `/api/v1/sources/{id}/history` | 该信息源的运行历史采样 |
| GET | `/api/v1/events` | SSE 实时事件流 |

除 `health` 与 `login` / `redeem` 外，所有接口都需要凭据：脚本侧用 `Authorization: Bearer <API Token>`，浏览器侧用会话凭据。SSE 无法自定义请求头，因此事件流允许通过 `?token=` 查询参数传递。

## 目录结构

```text
cmd/owdash/           进程入口
internal/
  api/                HTTP 路由、中间件、SSE、会话与登录节流
  certs/              TLS 证书准备（自带或自签）
  config/             配置、密钥与口令散列
  registry/           注册模型、校验、持久化、示例数据
  engine/             执行器、调度器、MQTT 订阅管理
  transport/          HTTP / TCP / UDP / Exec / MQTT 连接池
  mqtt/               零依赖的 MQTT 3.1.1 客户端
  template/           最小模板系统
  extract/            响应体取值
  state/              运行时状态缓存、运行历史与事件广播
  webui/              embed.FS 静态资源
web/                  Vue 3 + TypeScript + Vite + Tailwind 4 前端
  public/             站点图标与 theme-init.js（CSP 要求脚本外置）
  src/assets/         界面内 logo（由 make icons 生成）
  src/lib/auth.ts     认证状态：登录、改密、Token 登录、一次性交接
  src/lib/token.ts    凭据存储：会话存储优先，勾选「记住」才落 localStorage
  src/lib/handoff.ts  LuCI 一跳交接（URL / postMessage 双通道）
  src/lib/theme.ts    主题状态：本地存储、跟随系统、切换
  src/style.css       语义色 token 与深浅两套取值
assets/WrtDeck.png    图标母版，站点图标与 logo 的唯一来源
scripts/              开发、构建与打包测试脚本（兼容 macOS bash 3.2）
packaging/openwrt/    procd 启动脚本与 apk / ipk 控制文件
packaging/luci/       luci-app-wrtdeck 薄壳的 rpcd / menu / 前端视图
```

## 资源占用

| 项目 | 实测 |
| --- | --- |
| 本机二进制 | 7.5 MB |
| linux/arm64 静态 ELF | 7.9 MB |
| apk 安装包 | 3.2 MB |
| ipk 安装包（gzip） | 3.2 MB |
| luci-app-wrtdeck 薄壳 apk | 8 KB |
| luci-app-wrtdeck 薄壳 ipk | 9 KB |
| 前端 JS（gzip） | 72 KB |
| 前端 CSS（gzip） | 8.0 KB |
| 界面 logo（256px PNG） | 48 KB |

后端零第三方依赖，`go.mod` 只有 `module` 与 `go` 两行。

## 已知限制

- MQTT 只实现 3.1.1，不支持 MQTT 5.0 与 WebSocket 承载（`ws://` / `wss://` 会在连接时给出明确错误）。
- Exec 传输默认关闭，需要在 `config.json` 中开启 `exec.enabled` 并配置 `exec.allowlist`。
- 运行历史只驻内存，进程重启即清空；需要长期留存请自行拉取 `/api/v1/sources/{id}/history`。
- 会话凭据同样只驻内存，进程重启后浏览器需要重新登录，**API Token 不受影响**。
- apk / ipk 已通过结构校验与安装模拟，但**尚未在真实 OpenWrt 设备上安装验证**（构建环境无设备、QEMU 或容器）。
