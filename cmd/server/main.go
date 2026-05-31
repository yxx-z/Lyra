// cmd/server/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yxx-z/lyra/internal/api"
	"github.com/yxx-z/lyra/internal/config"
	"github.com/yxx-z/lyra/internal/db"
	"github.com/yxx-z/lyra/internal/scanner"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "配置文件路径")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		os.Exit(1)
	}

	database, err := db.Open(cfg.Database.Path)
	if err != nil {
		slog.Error("打开数据库失败", "err", err)
		os.Exit(1)
	}
	defer database.Close()

	sc := scanner.NewScanner(database, cfg.Library)
	if err := sc.Start(); err != nil {
		slog.Error("启动扫描器失败", "err", err)
		os.Exit(1)
	}
	defer sc.Stop()

	router := api.NewRouter(sc)
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("Lyra 启动", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-serverErr:
		slog.Error("服务器启动失败", "err", err)
		os.Exit(1)
	}
	slog.Info("正在关闭服务器")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		slog.Error("优雅关闭失败", "err", err)
	}
}
