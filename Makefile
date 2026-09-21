# WrtDeck 构建入口
#
# 所有目标都委托给 scripts/ 下的脚本，脚本内部会自动探测 go 与 pnpm，
# 因此这里不假设任何工具已经进入 PATH。

SHELL := /bin/sh

VERSION ?= dev
GOARCH  ?= arm64

.DEFAULT_GOAL := help

.PHONY: help dev dev-backend dev-frontend web icons backend build release run \
        apk ipk luci packages check-apk check-ipk check-luci check-ipk-luci \
        test-apk test-ipk test-pkg fmt vet check clean

# 列出全部可用目标
help:
	@echo "WrtDeck 可用目标："
	@echo "  make dev           并行启动前后端开发服务"
	@echo "  make dev-backend   只启动后端（dev 模式，无鉴权）"
	@echo "  make dev-frontend  只启动前端 Vite 开发服务器"
	@echo "  make web           构建前端到 internal/webui/dist"
	@echo "  make icons         从 assets/WrtDeck.png 重新生成站点图标与界面 logo"
	@echo "  make backend       只构建后端（使用已有前端产物）"
	@echo "  make build         构建前端 + 本机后端，产物在 dist/wrtdeck"
	@echo ""
	@echo "  make apk           打包 OpenWrt apk（25.12 及更新版本的默认格式）"
	@echo "  make ipk           打包 OpenWrt ipk（24.10 及更早版本）"
	@echo "  make luci          打包 luci-app-wrtdeck 薄壳（apk + ipk 两种格式）"
	@echo "  make packages      面板与薄壳的四种产物一次打全"
	@echo ""
	@echo "  make check-apk     校验 apk 内部结构（段边界、签名、逐文件校验和）"
	@echo "  make check-ipk     校验 ipk 内部结构"
	@echo "  make check-luci    校验薄壳 apk 的结构与源码级交接契约"
	@echo "  make check-ipk-luci 校验薄壳 ipk 的同一批断言（两种格式共用一份）"
	@echo "  make test-apk      完整打包测试：结构校验 + 安装模拟运行（apk）"
	@echo "  make test-ipk      同上，针对 ipk"
	@echo "  make test-pkg      上面两项都跑一遍"
	@echo ""
	@echo "  make release       交叉编译 OpenWrt linux/arm64 静态 ELF"
	@echo "  make run           以 dev 模式直接运行后端"
	@echo "  make fmt           格式化 Go 代码"
	@echo "  make vet           静态检查"
	@echo "  make check         fmt + vet + 前端类型检查"
	@echo "  make clean         清理构建产物与运行时数据"

# 前后端并行开发
dev:
	@sh scripts/dev.sh

# 只启动后端
dev-backend:
	@sh scripts/dev-backend.sh

# 只启动前端
dev-frontend:
	@sh scripts/dev-frontend.sh

# 构建前端
web:
	@sh scripts/build-web.sh

# 重新生成站点图标与界面 logo，母版为 assets/WrtDeck.png
icons:
	@sh scripts/make-icons.sh

# 构建本机后端，前端产物需已存在
backend:
	@GO_BIN="$$(command -v go || echo /opt/homebrew/bin/go)" \
	  "$$GO_BIN" build -trimpath \
	  -ldflags="-s -w -X main.version=$(VERSION)" \
	  -o dist/wrtdeck ./cmd/wrtdeck

# 完整构建
build:
	@VERSION=$(VERSION) sh scripts/build.sh

# 交叉编译 OpenWrt
release:
	@VERSION=$(VERSION) GOARCH=$(GOARCH) sh scripts/release.sh

# 打包面板本体：apk 是 25.12 起的默认格式，ipk 供 24.10 及更早版本
apk:
	@VERSION=$(VERSION) GOARCH=$(GOARCH) PKG_FORMAT=apk sh scripts/package-openwrt.sh

ipk:
	@VERSION=$(VERSION) GOARCH=$(GOARCH) PKG_FORMAT=ipk sh scripts/package-openwrt.sh

# 薄壳与 CPU 架构无关，因此两种格式一起出，省得用户纠结装哪个
luci:
	@VERSION=$(VERSION) PKG_FORMAT=both sh scripts/package-luci.sh

# 四种产物一次打全
packages: apk ipk luci

# 结构校验：三个脚本都不传路径时自动取 dist/ 下最新的对应产物
check-apk:
	@sh scripts/check-apk.sh

check-ipk:
	@sh scripts/check-ipk.sh

check-luci:
	@sh scripts/check-apk.sh --preset=luci

check-ipk-luci:
	@sh scripts/check-ipk.sh --preset=luci

# 完整打包测试：先校验结构，再模拟一次安装运行
test-apk: check-apk
	@sh scripts/simulate-openwrt.sh "$$(ls -1 dist/wrtdeck-*.apk 2>/dev/null | head -1)"

test-ipk: check-ipk
	@sh scripts/simulate-openwrt.sh "$$(ls -1 dist/wrtdeck_*.ipk 2>/dev/null | head -1)"

test-pkg: check-apk check-ipk check-luci check-ipk-luci
	@sh scripts/simulate-openwrt.sh "$$(ls -1 dist/wrtdeck-*.apk 2>/dev/null | head -1)"
	@sh scripts/simulate-openwrt.sh "$$(ls -1 dist/wrtdeck_*.ipk 2>/dev/null | head -1)"

# 直接运行
run:
	@sh scripts/dev-backend.sh

# 格式化
fmt:
	@GO_BIN="$$(command -v go || echo /opt/homebrew/bin/go)"; \
	  "$$GO_BIN" fmt ./... && "$$GO_BIN" vet ./...

# 静态检查
vet:
	@GO_BIN="$$(command -v go || echo /opt/homebrew/bin/go)"; "$$GO_BIN" vet ./...

# 全量检查
check: vet
	@cd web && (pnpm typecheck 2>/dev/null || corepack pnpm typecheck)

# 清理
clean:
	@rm -rf dist data
	@find internal/webui/dist -mindepth 1 ! -name '.gitkeep' -delete
	@echo "已清理构建产物"
