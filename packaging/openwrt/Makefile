# OpenWrt 包定义：把交叉编译好的 owdash ELF 与 procd 脚本打包成 ipk
#
# 用法：把本目录放入 OpenWrt SDK 的 package/ 下，或在 SDK 内执行
#   make package/owdash/compile V=s

include $(TOPDIR)/rules.mk

PKG_NAME:=owdash
PKG_VERSION:=1.0.0
PKG_RELEASE:=1

PKG_MAINTAINER:=WrtDeck
PKG_LICENSE:=MIT

include $(INCLUDE_DIR)/package.mk

define Package/owdash
  SECTION:=utils
  CATEGORY:=Utilities
  TITLE:=WrtDeck 轻量设备控制面板
  DEPENDS:=+ca-bundle
  PKGARCH:=all
endef

define Package/owdash/description
 运行在 OpenWrt 上的轻量 Dashboard，通过注册表把 HTTP/TCP/UDP/MQTT
 设备状态与控制命令动态变成面板卡片和操作按钮。前端资源已通过
 embed.FS 编入二进制，无需额外依赖。
endef

define Build/Compile
	# 二进制由仓库根目录的 make release 产出，这里只负责打包
endef

define Package/owdash/install
	$(INSTALL_DIR) $(1)/usr/bin
	$(INSTALL_BIN) $(PKG_BUILD_DIR)/owdash-linux-arm64 $(1)/usr/bin/owdash

	$(INSTALL_DIR) $(1)/etc/init.d
	$(INSTALL_BIN) ./owdash.init $(1)/etc/init.d/owdash

	$(INSTALL_DIR) $(1)/etc/owdash
	$(INSTALL_DIR) $(1)/etc/config
endef

define Package/owdash/postinst
#!/bin/sh
[ -n "$${IPKG_INSTROOT}" ] || {
	/etc/init.d/owdash enable
	/etc/init.d/owdash start
}
exit 0
endef

define Package/owdash/prerm
#!/bin/sh
[ -n "$${IPKG_INSTROOT}" ] || {
	/etc/init.d/owdash stop
	/etc/init.d/owdash disable
}
exit 0
endef

$(eval $(call BuildPackage,owdash))
