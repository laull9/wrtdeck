// wrtdeck 是运行在 OpenWrt 上的轻量设备控制与信息 Dashboard。
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"wrtdeck/internal/api"
	"wrtdeck/internal/certs"
	"wrtdeck/internal/config"
	"wrtdeck/internal/engine"
	"wrtdeck/internal/gateway"
	"wrtdeck/internal/registry"
	"wrtdeck/internal/state"
	"wrtdeck/internal/transport"
	"wrtdeck/internal/webui"
)

// version 由构建脚本通过 -ldflags 注入
var version = "dev"

// shutdown_timeout 是优雅退出的最长等待时间
const shutdown_timeout = 5 * time.Second

// options 汇总命令行开关
type options struct {
	config_path    string
	data_dir       string
	listen         string
	dev            bool
	seed_demo      bool
	print_token    bool
	show_version   bool
	reset_set      bool
	reset_password string
	gateway        bool
	export_web     string
}

// main 解析参数后启动服务或执行一次性的维护命令
func main() {
	opts := parse_flags()
	if opts.show_version {
		fmt.Println(version)
		return
	}
	if err := dispatch(opts); err != nil {
		log.Fatalf("启动失败: %v", err)
	}
}

// dispatch 把短命的一次性命令与常驻服务分开处理
func dispatch(opts options) error {
	switch {
	case opts.gateway:
		return run_gateway(opts)
	case opts.export_web != "":
		return run_export_web(opts.export_web)
	}
	return run(opts)
}

// parse_flags 解析命令行参数
func parse_flags() options {
	var opts options
	flag.StringVar(&opts.config_path, "config", "./data/config.json", "配置文件路径")
	flag.StringVar(&opts.data_dir, "data-dir", "", "数据目录，覆盖配置中的 data_dir")
	flag.StringVar(&opts.listen, "listen", "", "监听地址，覆盖配置中的 listen")
	flag.BoolVar(&opts.dev, "dev", false, "开发模式：关闭鉴权、放行跨域并写入示例注册项")
	flag.BoolVar(&opts.seed_demo, "seed-demo", false, "注册表为空时写入自检示例注册项")
	flag.BoolVar(&opts.print_token, "print-token", false, "打印 API Token 后退出")
	flag.BoolVar(&opts.show_version, "version", false, "打印版本后退出")
	// 网关模式由设备 Web 服务器的 CGI 调用：处理完这一条请求就退出
	flag.BoolVar(&opts.gateway, "gateway", false, "以 CGI 网关身份处理一次请求后退出（供 LuCI 同源内嵌）")
	// 导出前端资源到 Web 根目录，让设备自带 Web 服务器直接服务面板页面
	flag.StringVar(&opts.export_web, "export-web", "", "把内嵌前端资源导出到指定目录后退出")
	// 口令重置是设备上唯一的找回手段：忘记口令时从串口或 SSH 以 root 执行
	flag.Func("reset-password", "把登录口令重置为给定值后退出；值为 - 时从标准输入读取一行",
		func(value string) error {
			opts.reset_set = true
			opts.reset_password = value
			return nil
		})
	flag.Parse()
	return opts
}

// run_export_web 把内嵌的前端资源导出到设备上的 Web 根目录。
//
// 面板页面因此和 LuCI 处在同一个源上：同一个域名、同一个端口、
// 也就自动继承了设备 Web 服务器当前的加密方式，不必再让浏览器去猜 IP 与端口。
func run_export_web(dir string) error {
	result, err := webui.Export(dir)
	if err != nil {
		return err
	}
	if result.Changed {
		log.Printf("已导出前端资源：%s（%d 个文件，指纹 %s）", result.Dir, result.Files, result.Fingerprint)
		return nil
	}
	log.Printf("前端资源已是最新（指纹 %s），跳过导出", result.Fingerprint)
	return nil
}

// run_gateway 以 CGI 网关身份处理一次 API 请求。
//
// 这个模式不启动任何常驻服务，也不碰注册表与运行时状态：
// 设备 Web 服务器每收到一次 API 调用就起一个进程，进程只做转发。
func run_gateway(opts options) error {
	cfg, err := load_gateway_config(opts)
	if err != nil {
		return err
	}
	return gateway.Run(gateway.Options{
		Upstream:   cfg.UpstreamURL(),
		Token:      gateway_token(cfg),
		VerifyLuci: cfg.Gateway.VerifyLuci(),
		Inject:     cfg.Gateway.Inject(),
		Timeout:    cfg.Gateway.Timeout(),
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Logger:     log.New(os.Stderr, "wrtdeck-gateway ", 0),
	})
}

// load_gateway_config 只读地装载网关需要的配置。
// 刻意不走 config.Load：它会在文件缺失时写出一份样例，
// 而「一次浏览器请求顺带写出配置文件」不是这里该有的副作用。
func load_gateway_config(opts options) (*config.Config, error) {
	if opts.data_dir == "" && opts.listen == "" {
		data, err := os.ReadFile(opts.config_path)
		switch {
		case err == nil:
			cfg, parse_err := config.Parse(data)
			if parse_err != nil {
				return nil, parse_err
			}
			return cfg, nil
		case !os.IsNotExist(err):
			return nil, err
		}
		cfg := config.Default()
		cfg.DataDir = filepath.Dir(opts.config_path)
		return cfg, nil
	}

	cfg, err := config.Load(opts.config_path)
	if err != nil {
		return nil, err
	}
	if opts.data_dir != "" {
		cfg.DataDir = opts.data_dir
	}
	if opts.listen != "" {
		cfg.Listen = opts.listen
	}
	return cfg, nil
}

// gateway_token 读取用于注入的 API Token。
//
// 密钥文件不存在时直接返回空串，不在这里生成：网关是随请求生灭的短命进程，
// 顺手生成 Token 会带来一次 200ms 量级的 PBKDF2 计算拖慢每一次页面访问，
// 而且「设备凭据」也不该由一次浏览器的偶然访问来决定。
func gateway_token(cfg *config.Config) string {
	if _, err := os.Stat(cfg.SecretsPath()); err != nil {
		return ""
	}
	secrets, err := config.LoadSecrets(cfg.SecretsPath(), cfg.Auth.TokenEnv)
	if err != nil {
		log.Printf("网关读取密钥失败，本次不注入凭据: %v", err)
		return ""
	}
	return secrets.Token()
}

// run 完成装配、启动与优雅退出
func run(opts options) error {
	cfg, err := config.Load(opts.config_path)
	if err != nil {
		return err
	}
	if opts.data_dir != "" {
		cfg.DataDir = opts.data_dir
	}
	if cfg.DataDir == "" {
		cfg.DataDir = filepath.Dir(opts.config_path)
	}
	if opts.listen != "" {
		cfg.Listen = opts.listen
	}
	if opts.dev {
		cfg.Auth.Disabled = true
		cfg.SeedDemo = true
		cfg.Listen = local_listen(cfg.Listen)
	}
	if opts.seed_demo {
		cfg.SeedDemo = true
	}
	if err = os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}

	secrets, err := config.LoadSecrets(cfg.SecretsPath(), cfg.Auth.TokenEnv)
	if err != nil {
		return err
	}
	if opts.print_token {
		fmt.Println(secrets.Token())
		return nil
	}
	if opts.reset_set {
		return reset_password(secrets, opts.reset_password)
	}

	store := registry.NewStore(cfg.RegistryPath())
	if err = store.Load(); err != nil {
		return err
	}
	if store.Count() == 0 && cfg.SeedDemo {
		if err = seed_demo(store, cfg); err != nil {
			return err
		}
	}

	states := state.NewStore()
	history := state.NewHistory(cfg.Limits.HistoryLimit)
	hub := state.NewHub()
	defer hub.Close()

	// MQTT 连接池按需建连，没有 MQTT 注册项时不会产生任何连接
	pool := transport.NewMQTTPool(transport.MQTTOptions{
		KeepaliveSeconds:  cfg.MQTT.KeepaliveSeconds,
		ConnectTimeout:    time.Duration(cfg.MQTT.ConnectTimeoutMS) * time.Millisecond,
		MaxReconnectDelay: time.Duration(cfg.MQTT.MaxReconnectDelayMS) * time.Millisecond,
		MaxPayloadBytes:   cfg.Limits.MaxBodyBytes,
	}, log.Default())
	defer pool.Close()

	exec_opts := transport.Options{
		MaxBodyBytes:   cfg.Limits.MaxBodyBytes,
		DefaultTimeout: time.Duration(cfg.Limits.DefaultTimeoutMS) * time.Millisecond,
		ExecEnabled:    cfg.Exec.Enabled,
		ExecAllowlist:  cfg.Exec.Allowlist,
		MQTT:           pool,
	}
	exec := engine.NewExecutor(store, states, hub, history, secrets, exec_opts, cfg.Workers.Action)

	root_ctx, stop_signals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop_signals()

	subs := engine.NewSubscriberManager(exec, store, pool)
	pool.SetConnectionWatcher(subs.OnConnectionChange)

	sched := engine.NewScheduler(exec, store, states, hub,
		cfg.Workers.Source, cfg.Limits.DefaultTimeoutMS, cfg.Limits.MinIntervalMS)
	sched.SetSubscribers(subs)

	server := api.NewServer(cfg, store, states, hub, history, pool, exec, sched, secrets,
		webui.Handler(), version, opts.dev)

	// TLS 证书在监听之前准备好：证书写错时应当启动失败，而不是等到第一个请求
	tls_result, err := certs.Prepare(certs.Settings{
		Enabled:        cfg.TLS.Enabled,
		CertFile:       cfg.TLS.CertFile,
		KeyFile:        cfg.TLS.KeyFile,
		DataDir:        cfg.DataDir,
		AutoSelfSigned: cfg.TLS.SelfSignedEnabled(),
	})
	if err != nil {
		return err
	}

	http_server := &http.Server{
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// 先绑定端口再启动调度器，否则指向自身的自检信息源首次采集必然连接失败
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("监听 %s 失败: %w", cfg.Listen, err)
	}

	// 可选的明文监听：只做 308 跳转，让记着旧地址的书签也能落到 HTTPS 上
	var redirect_server *http.Server
	if tls_result.Enabled && cfg.TLS.RedirectListen != "" {
		redirect_listener, err := net.Listen("tcp", cfg.TLS.RedirectListen)
		if err != nil {
			return fmt.Errorf("监听 %s 失败: %w", cfg.TLS.RedirectListen, err)
		}
		redirect_server = &http.Server{Handler: redirect_handler(cfg.Listen, cfg.Auth.AllowedHosts), ReadHeaderTimeout: 5 * time.Second}
		go func() {
			if err := redirect_server.Serve(redirect_listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("明文跳转监听退出: %v", err)
			}
		}()
	}

	sched.Start(root_ctx)
	defer sched.Stop()

	banner(cfg, opts, secrets, tls_result)

	err_chan := make(chan error, 1)
	go func() {
		var serve_err error
		if tls_result.Enabled {
			serve_err = http_server.ServeTLS(listener, tls_result.CertFile, tls_result.KeyFile)
		} else {
			serve_err = http_server.Serve(listener)
		}
		if serve_err != nil && !errors.Is(serve_err, http.ErrServerClosed) {
			err_chan <- serve_err
		}
	}()

	select {
	case err = <-err_chan:
		return err
	case <-root_ctx.Done():
		log.Printf("收到退出信号，正在关闭")
	}

	shutdown_ctx, cancel := context.WithTimeout(context.Background(), shutdown_timeout)
	defer cancel()
	if redirect_server != nil {
		_ = redirect_server.Shutdown(shutdown_ctx)
	}
	return http_server.Shutdown(shutdown_ctx)
}

// redirect_handler 把明文请求 308 跳到面板的 HTTPS 地址，并校验主机名合法性
func redirect_handler(tls_addr string, allowed_hosts []string) http.Handler {
	_, tls_port, err := net.SplitHostPort(tls_addr)
	if err != nil || tls_port == "" {
		tls_port = "443"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if parsed_host, _, err := net.SplitHostPort(r.Host); err == nil {
			host = parsed_host
		}
		host = strings.Trim(host, "[]")
		if host == "" || strings.ContainsAny(host, "/\\ \t\r\n;\"'") || !redirect_host_allowed(host, allowed_hosts) {
			http.Error(w, "无法重定向到未受信任的主机名", http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, "https://"+net.JoinHostPort(host, tls_port)+r.URL.RequestURI(),
			http.StatusPermanentRedirect)
	})
}

// redirect_host_allowed 检查重定向目标主机名是否属于允许列表或本机地址
func redirect_host_allowed(host string, allowed []string) bool {
	if net.ParseIP(host) != nil || host == "localhost" {
		return true
	}
	for _, item := range allowed {
		target := strings.ToLower(strings.TrimSpace(item))
		if target == "" {
			continue
		}
		if target == strings.ToLower(host) || (strings.HasPrefix(target, "*.") && strings.HasSuffix(strings.ToLower(host), target[1:])) {
			return true
		}
	}
	for _, suffix := range []string{".lan", ".local", ".home", ".home.arpa", ".internal"} {
		if strings.HasSuffix(strings.ToLower(host), suffix) {
			return true
		}
	}
	if name, err := os.Hostname(); err == nil && strings.EqualFold(name, host) {
		return true
	}
	return false
}

// reset_password 重置登录口令，是设备主人忘记口令时唯一的找回手段。
// 它需要能读到密钥文件，因此实际权限门槛与 root 等价；API Token 不受影响。
func reset_password(secrets *config.Secrets, value string) error {
	password := strings.TrimSpace(value)
	if password == "" || password == "-" {
		fmt.Fprint(os.Stderr, "请输入新的登录口令: ")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return fmt.Errorf("读取口令失败: %w", err)
		}
		password = strings.TrimSpace(line)
	}
	if err := config.CheckPasswordStrength(password); err != nil {
		return err
	}
	if err := secrets.SetPassword(password, false); err != nil {
		return err
	}
	fmt.Println("登录口令已重置，请用新口令登录面板。")
	fmt.Println("API Token 未发生变化，脚本与设备本机调用不受影响。")
	return nil
}

// seed_demo 写入示例注册项，注册项通过 ${secret.api_token} 回调本服务
func seed_demo(store *registry.Store, cfg *config.Config) error {
	base_url := fmt.Sprintf("http://%s", local_hostport(cfg.Listen))
	for _, entry := range registry.SeedDemo(base_url, cfg.Limits.DefaultTimeoutMS*2) {
		if err := store.Put(entry); err != nil {
			return fmt.Errorf("写入示例注册项失败: %w", err)
		}
	}
	log.Printf("已写入 %d 条自检示例注册项", store.Count())
	return nil
}
