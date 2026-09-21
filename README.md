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

项目已完成端到端闭环：Go 后端、Vue 前端、SSE 实时通道、注册表热更新、全部五种传输、OpenWrt ipk 打包与打包测试均可运行。

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
| Bearer Token 鉴权（文件 / 环境变量两种来源） | 可用 |
| 运行历史（内存环形缓冲，不写闪存） | 可用 |
| Vue 3 + Tailwind 4 + Reka UI 前端 | 可用 |
| 深浅双主题（跟随系统 + 右上角手动切换） | 可用 |
| procd 启动脚本与 ipk 打包 | 可用，附 42 项结构校验与安装模拟 |

## 环境要求

- Go 1.23+（脚本会自动探测 `/opt/homebrew/bin/go`、`/usr/local/go/bin/go`）
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
make ipk            # 打包 OpenWrt 可安装 ipk
make check-ipk      # 校验 ipk 内部结构（42 项断言）
make test-ipk       # 完整打包测试：结构校验 + 安装模拟运行
make check          # go vet + 前端类型检查
make clean          # 清理构建产物
```

## 运行模式

**开发模式**（`-dev`）：关闭鉴权、放行跨域、监听回环地址、写入示例注册项。

**生产模式**：启用随机 Bearer Token，Token 首次启动生成并写入 `secrets.json`（权限 0600）。

```bash
./dist/owdash -config /etc/owdash/config.json
./dist/owdash -print-token          # 打印当前 Token
```

前端在服务端开启鉴权时会在右上角显示「设置 Token」入口，Token 保存在浏览器 localStorage。SSE 无法自定义请求头，因此事件流允许通过 `?token=` 查询参数传递。

## 界面主题

界面有深色与浅色两套配色，右上角最右侧的月亮 / 太阳按钮用于切换。

- **默认跟随系统**：没有手动切过时，`prefers-color-scheme` 变化会实时生效。
- **手动切换后固定**：点击按钮会把选择写入 `localStorage` 的 `owdash.theme`，此后不再跟随系统。想恢复跟随系统，清掉该键即可。
- **不闪屏**：主题在 `index.html` 的内联脚本里定下来，早于首帧渲染，刷新不会先亮后暗。
- **配色集中在一处**：组件里只写 `bg-surface` / `text-ink-body` 这类语义名，实际色值集中在 `web/src/style.css` 的 `:root` 与 `:root.dark` 两块，新增主题不需要改组件。

## API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/health` | 健康检查，不需要鉴权 |
| GET | `/api/v1/dashboard` | 全部信息源状态 + 动作定义 |
| GET | `/api/v1/registry` | 注册表全量 |
| PUT | `/api/v1/registry/{id}` | 注册或更新（幂等，ID 取自路径） |
| DELETE | `/api/v1/registry/{id}` | 删除注册项 |
| POST | `/api/v1/actions/{id}/run` | 执行动作 |
| POST | `/api/v1/sources/{id}/refresh` | 手动刷新信息源（订阅型返回 409） |
| GET | `/api/v1/sources/{id}/history` | 该信息源的运行历史采样 |
| GET | `/api/v1/events` | SSE 实时事件流 |

## 目录结构

```text
cmd/owdash/           进程入口
internal/
  api/                HTTP 路由、中间件、SSE
  config/             配置与密钥
  registry/           注册模型、校验、持久化、示例数据
  engine/             执行器、调度器、MQTT 订阅管理
  transport/          HTTP / TCP / UDP / Exec / MQTT 连接池
  mqtt/               零依赖的 MQTT 3.1.1 客户端
  template/           最小模板系统
  extract/            响应体取值
  state/              运行时状态缓存、运行历史与事件广播
  webui/              embed.FS 静态资源
web/                  Vue 3 + TypeScript + Vite + Tailwind 4 前端
  public/             站点图标（favicon.ico 内含 16/32/48，另有 48x48 PNG，由 make icons 生成）
  src/assets/         界面内 logo（由 make icons 生成）
  src/style.css       语义色 token 与深浅两套取值
  src/lib/theme.ts    主题状态：本地存储、跟随系统、切换
assets/WrtDeck.png    图标母版，站点图标与 logo 的唯一来源
scripts/              开发、构建与打包测试脚本（兼容 macOS bash 3.2）
packaging/openwrt/    procd 启动脚本与 ipk 控制文件
```

## 资源占用

| 项目 | 实测 |
| --- | --- |
| 本机二进制 | 7.5 MB |
| linux/arm64 静态 ELF | 7.4 MB |
| ipk 安装包（gzip） | 3.0 MB |
| 前端 JS（gzip） | 72 KB |
| 前端 CSS（gzip） | 8.0 KB |
| 界面 logo（256px PNG） | 48 KB |

后端零第三方依赖，`go.mod` 只有 `module` 与 `go` 两行。

## 已知限制

- MQTT 只实现 3.1.1，不支持 MQTT 5.0 与 WebSocket 承载（`ws://` / `wss://` 会在连接时给出明确错误）。
- Exec 传输默认关闭，需要在 `config.json` 中开启 `exec.enabled` 并配置 `exec.allowlist`。
- 运行历史只驻内存，进程重启即清空；需要长期留存请自行拉取 `/api/v1/sources/{id}/history`。
- ipk 已通过结构校验与安装模拟，但**尚未在真实 OpenWrt 设备上安装验证**（构建环境无设备、QEMU 或容器）。
