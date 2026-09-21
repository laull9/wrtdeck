// owdash 是运行在 OpenWrt 上的轻量设备控制与信息 Dashboard。
package main

import (
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
	"syscall"
	"time"

	"owdash/internal/api"
	"owdash/internal/config"
	"owdash/internal/engine"
	"owdash/internal/registry"
	"owdash/internal/state"
	"owdash/internal/transport"
	"owdash/internal/webui"
)

// version 由构建脚本通过 -ldflags 注入
var version = "dev"

// shutdown_timeout 是优雅退出的最长等待时间
const shutdown_timeout = 5 * time.Second

// options 汇总命令行开关
type options struct {
	config_path  string
	data_dir     string
	listen       string
	dev          bool
	seed_demo    bool
	print_token  bool
	show_version bool
}

// main 解析参数后启动服务并等待退出信号
func main() {
	opts := parse_flags()
	if opts.show_version {
		fmt.Println(version)
		return
	}
	if err := run(opts); err != nil {
		log.Fatalf("启动失败: %v", err)
	}
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
	flag.Parse()
	return opts
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

	sched.Start(root_ctx)
	defer sched.Stop()

	banner(cfg, opts, secrets.Token(), secrets.FromEnv())

	err_chan := make(chan error, 1)
	go func() {
		if err := http_server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			err_chan <- err
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
	return http_server.Shutdown(shutdown_ctx)
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

// banner 打印启动信息与访问地址
func banner(cfg *config.Config, opts options, token string, from_env bool) {
	log.Printf("WrtDeck %s 启动中", version)
	log.Printf("监听地址: %s", cfg.Listen)
	log.Printf("数据目录: %s", cfg.DataDir)
	if cfg.Auth.Disabled {
		log.Printf("鉴权状态: 已关闭（开发模式）")
	} else if from_env {
		log.Printf("鉴权状态: 已开启，Token 来自环境变量 %s", cfg.Auth.TokenEnv)
	} else {
		log.Printf("鉴权状态: 已开启，Token 见 %s", cfg.SecretsPath())
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
