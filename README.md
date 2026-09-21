# WrtDeck

WrtDeck 是一个运行在 OpenWrt 上的轻量级设备控制面板，用统一配置把 HTTP、TCP、UDP、MQTT 等设备状态与控制命令动态变成 Dashboard 卡片和操作按钮。

- 架构设计：[docs/ARCHITECTURE.md](./docs/ARCHITECTURE.md)
- 协作约定：[AGENTS.md](./AGENTS.md)

## 当前进度

项目骨架已跑通：Go 后端、Vue 前端、SSE 实时通道、注册表热更新与 OpenWrt 交叉编译全部可用。

| 能力 | 状态 |
| --- | --- |
| Registry 注册表（JSON 持久化 + 原子写） | 可用 |
| Scheduler 调度采集（worker pool、跳过未完成的上一轮、stale 降级） | 可用 |
| Transport：HTTP / TCP / UDP / Exec | 可用 |
| Transport：MQTT | 占位，需引入 eclipse-paho 后接线 |
| Template 变量替换与 url/json 过滤器 | 可用 |
| Extractor：raw / json 点分路径 / regex | 可用 |
| State Cache + SSE 推送 | 可用 |
| Bearer Token 鉴权 | 可用 |
| Vue 3 + Tailwind 4 + Reka UI 前端 | 可用 |
| procd 启动脚本与 ipk 打包 | 已备好，未在真机验证 |

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
make build          # 前端 + 本机后端 → dist/owdash
make release        # 交叉编译 OpenWrt linux/arm64 静态 ELF
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

## API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/health` | 健康检查，不需要鉴权 |
| GET | `/api/v1/dashboard` | 全部信息源状态 + 动作定义 |
| GET | `/api/v1/registry` | 注册表全量 |
| PUT | `/api/v1/registry/{id}` | 注册或更新（幂等，ID 取自路径） |
| DELETE | `/api/v1/registry/{id}` | 删除注册项 |
| POST | `/api/v1/actions/{id}/run` | 执行动作 |
| POST | `/api/v1/sources/{id}/refresh` | 手动刷新信息源 |
| GET | `/api/v1/events` | SSE 实时事件流 |

## 目录结构

```text
cmd/owdash/           进程入口
internal/
  api/                HTTP 路由、中间件、SSE
  config/             配置与密钥
  registry/           注册模型、校验、持久化、示例数据
  engine/             执行器与调度器
  transport/          HTTP / TCP / UDP / Exec / MQTT
  template/           最小模板系统
  extract/            响应体取值
  state/              运行时状态缓存与事件广播
  webui/              embed.FS 静态资源
web/                  Vue 3 + TypeScript + Vite + Tailwind 4 前端
scripts/              开发与构建脚本（兼容 macOS bash 3.2）
packaging/openwrt/    procd 启动脚本与 ipk 包定义
```

## 资源占用

| 项目 | 实测 |
| --- | --- |
| 本机二进制 | 7.2 MB |
| linux/arm64 静态 ELF | 7.1 MB |
| 前端 JS（gzip） | 60 KB |
| 前端 CSS（gzip） | 6.9 KB |

## 已知限制

- MQTT 传输尚未接线，注册 MQTT 条目时会在执行阶段返回明确错误。
- Exec 传输默认关闭，需要在 `config.json` 中开启 `exec.enabled` 并配置 `exec.allowlist`。
- `packaging/openwrt/` 已备好，但尚未在真实 OpenWrt 设备上验证。
