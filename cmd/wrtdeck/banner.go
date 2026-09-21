package main

import (
	"log"
	"net"
	"strings"

	"wrtdeck/internal/certs"
	"wrtdeck/internal/config"
	"wrtdeck/internal/transport"
)

// banner 打印启动信息、访问地址与暴露面告警
func banner(cfg *config.Config, opts options, secrets *config.Secrets, tls_result certs.Result) {
	scheme := "http"
	if tls_result.Enabled {
		scheme = "https"
	}
	log.Printf("WrtDeck %s 启动中", version)
	log.Printf("监听地址: %s://%s", scheme, cfg.Listen)
	log.Printf("数据目录: %s", cfg.DataDir)
	if cfg.Auth.Disabled {
		log.Printf("鉴权状态: 已关闭（开发模式）")
	} else {
		log.Printf("鉴权状态: 已开启，口令登录 + API Token 双凭据，会话有效期 %s", cfg.Auth.SessionTTL())
		if secrets.FromEnv() {
			log.Printf("API Token: 来自环境变量 %s", cfg.Auth.TokenEnv)
		} else {
			log.Printf("API Token: 见 %s（/etc/init.d/wrtdeck token 可直接打印）", cfg.SecretsPath())
		}
		if secrets.MustChange() {
			log.Printf("初始口令: 仍为 %q，请登录后立即修改（未修改前只允许内网登录）", config.DefaultPassword)
		} else {
			log.Printf("登录口令: 已于 %s 修改", secrets.PasswordUpdated().Format("2006-01-02 15:04"))
		}
		log.Printf("登录限速: 单来源 %d 次失败即锁定 %s，连续失败成倍延长",
			cfg.Auth.MaxAttempts(), cfg.Auth.Lockout())
	}

	if tls_result.Enabled {
		log.Printf("TLS: 已启用，证书 %s，SHA-256 指纹 %s", tls_result.CertFile, tls_result.Fingerprint)
		if tls_result.SelfSigned {
			log.Printf("TLS: 当前是自签证书，浏览器会提示不受信任；公网访问建议换正式证书或置于反向代理之后")
		}
	} else if !cfg.Auth.Disabled {
		if listen_is_public(cfg.Listen) {
			log.Printf("警告: 监听 %s 且未启用 TLS，口令与 Token 都是明文传输；"+
				"要暴露到公网请配置 tls.cert_file / tls.key_file，或置于 HTTPS 反向代理之后", cfg.Listen)
		} else {
			log.Printf("TLS: 未启用（当前只监听本机地址）")
		}
	}

	if cfg.Limits.HistoryLimit > 0 {
		log.Printf("运行历史: 每个信息源保留 %d 条采样（仅内存）", cfg.Limits.HistoryLimit)
	} else {
		log.Printf("运行历史: 已关闭（limits.history_limit = 0）")
	}
	if opts.dev {
		log.Printf("前端开发地址: http://127.0.0.1:5173")
	}
	log.Printf("可用传输: %v", transport.Names())
	if !cfg.Exec.Enabled {
		log.Printf("Exec 传输: 已禁用（默认关闭），启用需在配置中设置 exec.enabled")
	}
}

// listen_is_public 判断监听地址是否超出了本机范围
func listen_is_public(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return true
	}
	host = strings.Trim(host, "[]")
	if host == "" || host == "0.0.0.0" || host == "::" {
		return true
	}
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsLoopback()
}

// local_listen 在开发模式下把监听地址收敛到回环地址
func local_listen(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "127.0.0.1:8080"
	}
	return net.JoinHostPort("127.0.0.1", port)
}

// local_hostport 把监听地址换成可被本机访问的地址，用于自检示例
func local_hostport(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "127.0.0.1:8080"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}
