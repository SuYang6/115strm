package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"115strm/internal/api"
	"115strm/internal/config"
	"115strm/internal/service/auditlog"
	"115strm/internal/service/auth"
	"115strm/internal/service/driver115"
	"115strm/internal/service/logbuffer"
	"115strm/internal/service/scheduler"
	"115strm/internal/service/strm302"
	"115strm/internal/store"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml.example", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if err := config.EnsureRuntimeDirs(cfg); err != nil {
		log.Fatalf("prepare directories: %v", err)
	}

	db, err := store.Open(cfg.Storage.DBFile)
	if err != nil {
		log.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		log.Fatalf("migrate sqlite: %v", err)
	}

	logs, err := logbuffer.New(cfg.Storage.LogFile, 800)
	if err != nil {
		log.Fatalf("open log file: %v", err)
	}
	defer logs.Close()
	if stopped, err := db.MarkActiveSTRMJobsStopped("服务已重启，旧运行任务已停止"); err != nil {
		logs.Warn("清理旧 STRM 运行任务失败：%v", err)
	} else if stopped > 0 {
		logs.Warn("服务启动时已停止 %d 个旧 STRM 运行任务", stopped)
	}
	if stopped, err := db.MarkRunningTaskRunsStopped("服务已重启，旧运行任务已停止"); err != nil {
		logs.Warn("清理旧定时任务运行记录失败：%v", err)
	} else if stopped > 0 {
		logs.Warn("服务启动时已停止 %d 个旧定时任务运行记录", stopped)
	}

	audit, err := auditlog.New(cfg.Storage.LogFile, cfg.Storage.AuditKeepDays)
	if err != nil {
		log.Fatalf("open audit log: %v", err)
	}

	authSvc, err := auth.New(db, cfg, logs)
	if err != nil {
		log.Fatalf("init auth: %v", err)
	}
	driverSvc := driver115.New(cfg, *configPath, db, authSvc.JWTSecret(), logs)
	strmSvc := strm302.New(cfg, db, driverSvc, logs, audit)
	schedulerSvc := scheduler.New(cfg, db, strmSvc, logs)
	if err := schedulerSvc.Load(); err != nil {
		logs.Warn("定时任务加载失败：%v", err)
	}
	defer schedulerSvc.Stop()

	dep := api.Dependencies{
		ConfigPath: *configPath,
		Config:     cfg,
		DB:         db,
		Logs:       logs,
		Audit:      audit,
		Auth:       authSvc,
		Driver115:  driverSvc,
		STRM:       strmSvc,
		Scheduler:  schedulerSvc,
	}

	router := api.NewRouter(dep)
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	servers := make([]*http.Server, 0, 2)
	startServer := func(name, addr string, handler http.Handler) {
		srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
		servers = append(servers, srv)
		go func() {
			logs.Info("%s 正在监听 %s", name, addr)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("%s listen: %v", name, err)
			}
		}()
	}
	startServer("115strm", addr, router)

	if cfg.Tasks.LocalTriggerEnabled {
		if cfg.Tasks.LocalTriggerPort == cfg.Server.Port || cfg.Tasks.LocalTriggerPort == cfg.Emby.ProxyPort {
			log.Fatalf("tasks local_trigger_port conflicts with another service port: %d", cfg.Tasks.LocalTriggerPort)
		}
		localTriggerAddr := fmt.Sprintf("%s:%d", cfg.Tasks.LocalTriggerHost, cfg.Tasks.LocalTriggerPort)
		startServer("local trigger", localTriggerAddr, api.NewLocalTriggerRouter(dep))
	}

	if cfg.Emby.ProxyPort > 0 {
		if cfg.Emby.ProxyPort == cfg.Server.Port {
			log.Fatalf("emby proxy_port must be different from server port: %d", cfg.Emby.ProxyPort)
		}
		if cfg.Emby.APIKey == "" {
			logs.Warn("Emby 网关已启用，但 emby.api_key 为空，直链播放查询会失败")
		}
		gateway, err := api.NewEmbyGateway(dep)
		if err != nil {
			log.Fatalf("init emby gateway: %v", err)
		}
		gatewayAddr := fmt.Sprintf("%s:%d", cfg.Emby.ProxyHost, cfg.Emby.ProxyPort)
		startServer("emby gateway", gatewayAddr, gateway)
	}

	wait := make(chan os.Signal, 1)
	signal.Notify(wait, syscall.SIGINT, syscall.SIGTERM)
	<-wait
	logs.Info("收到关闭请求")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, srv := range servers {
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("shutdown %s: %v", srv.Addr, err)
		}
	}
}

func init() {
	wd, _ := os.Getwd()
	if wd != "" {
		_ = os.MkdirAll(filepath.Join(wd, "data"), 0755)
	}
}
