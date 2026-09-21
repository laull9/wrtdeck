# WrtDeck 开发指南

本文面向参与 WrtDeck 开发、调试或从源码构建的开发者。

普通用户请参考：
- 快速安装：[docs/INSTALL.md](./INSTALL.md)
- 系统架构与设计细节：[docs/ARCHITECTURE.md](./ARCHITECTURE.md)

## 1. 开发环境要求

- **Go**：1.24+（构建脚本会自动探测 `/opt/homebrew/bin/go`、`/usr/local/go/bin/go` 或读取 `GO_BIN`）
- **Node.js**：18+
- **pnpm**：推荐 10+（未安装全局 pnpm 时会自动尝试 `corepack pnpm`）
- **Python 3**：仅标准库，用于 `scripts/apk_build.py` 生成 OpenWrt apk 包

前端依赖镜像已配置为 npmmirror（见 `web/.npmrc`）。

## 2. 本地调试与快速开始

执行以下命令同时启动后端开发进程与前端 Vite 开发服务器：

```bash
make dev
```

- **前端地址**：http://127.0.0.1:5173
- **后端监听**：`127.0.0.1:8080`（前端 Vite 自动将 `/api` 请求反向代理到后端）
- **首次运行行为**：自动写入 5 条自检示例数据（`demo-*`），模拟设备状态采集、数据提取、卡片渲染与 SSE 实时更新全流程，不需要外部硬件即可预览。
- **调试特性**：开发模式（`-dev`）下关闭口令认证并放行跨域，方便热重载开发。

如果需要单独启动某一部件：

```bash
make dev-backend    # 仅启动后端开发进程
make dev-frontend   # 仅启动前端 Vite 服务
```

## 3. 常用 Makefile 目标

```bash
# 帮助
make help           # 查看全部构建与测试目标

# 开发与构建
make dev            # 前后端联动开发
make web            # 仅构建前端产物到 internal/webui/dist
make backend        # 仅编译本机后端（需前端产物已就绪）
make build          # 构建完整本机版本（dist/wrtdeck）
make release        # 交叉编译 OpenWrt ARM64 静态 ELF（dist/wrtdeck-linux-arm64）
make release-all    # 一键构建所有支持架构的二进制、apk、ipk 与校验文件

# 打包 OpenWrt 安装包
make apk            # 打包 OpenWrt 25.12+ 使用的 apk
make ipk            # 打包 OpenWrt 24.10 及更早使用的 ipk
make luci           # 打包 LuCI 内嵌薄壳（输出 apk 与 ipk 两种格式）
make packages       # 一次性打出面板本体与薄壳的 4 种安装包

# 测试与校验
make check          # 代码格式检查、go vet 与前端 TypeScript 类型检查
make check-apk      # 校验 apk 包结构、分段、校验和与本地签名
make check-ipk      # 校验 ipk 归档结构、control 元数据与文件属主
make check-luci     # 校验薄壳结构与 LuCI 接口契约
make test-apk       # 模拟 OpenWrt 安装运行与版本升级（apk）
make test-ipk       # 模拟 OpenWrt 安装运行与版本升级（ipk）
make test-pkg       # 执行全部打包校验与安装模拟测试

# 图标与清理
make icons          # 从 assets/WrtDeck.png 母版重新生成站点与界面图标
make clean          # 清理构建产物与临时文件
```

## 4. 目录结构与模块分工

```text
cmd/
  wrtdeck/            主程序入口、命令行参数解析与横幅输出
internal/
  api/                HTTP 路由注册、安全中间件、SSE 事件流与限速逻辑
  certs/              TLS 自签证书自动生成与自带证书载入
  config/             配置文件解析、默认配置与口令散列计算
  engine/             设备调度器、执行引擎与订阅管理器
  extract/            采集数据提取器（raw / json path / regex）
  gateway/            LuCI 内嵌同源 CGI 反向代理网关
  mqtt/               零第三方依赖的轻量 MQTT 3.1.1 客户端
  registry/           信息源与动作注册表模型、JSON 持久化与示例数据
  state/              状态内存缓存、环形历史记录与广播中心
  template/           变量替换与过滤模板引擎
  transport/          采集与控制传输协议（HTTP / TCP / UDP / Exec / MQTT）
  webui/              通过 go:embed 打包的前端静态资源与文件导出
web/                  前端工程（Vue 3 + Tailwind 4 + Reka UI + TypeScript）
packaging/
  openwrt/            procd 启动脚本与 apk / ipk 控制脚本
  luci/               LuCI 内嵌薄壳源码（rpcd 插件、菜单、视图与 CGI 脚本）
scripts/              开发、跨平台构建、格式断言与模拟测试脚本
docs/                 设计与使用文档
```

## 5. 本地运行与命令行参数

编译出的二进制支持以下常用参数：

```bash
./dist/wrtdeck -config /etc/wrtdeck/config.json   # 指定配置文件启动
./dist/wrtdeck -dev                              # 以开发模式运行（关闭鉴权）
./dist/wrtdeck -print-token                      # 读取并打印当前配置中的 API Token
./dist/wrtdeck -reset-password -                 # 从标准输入读取新口令并就地更新
./dist/wrtdeck -export-web /www/wrtdeck          # 将内嵌前端页面释放到指定目录
./dist/wrtdeck -version                          # 打印版本信息
```

## 6. 代码与协作规范

请严格遵循 [AGENTS.md](../AGENTS.md) 约定的工程规范：
- 源码注释全部采用中文，每个函数顶部保留一行简明中文注释。
- 单代码文件行数控制在 500 行以内，平行逻辑主动拆分文件。
- 变量采用全小写字母加下划线命名（snake_case）。
- 提交代码前确保 `make check` 与 `go test ./...` 全部通过。
