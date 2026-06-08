// cmd/server/main.go
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/yxx-z/lyra/internal/api"
	"github.com/yxx-z/lyra/internal/config"
	"github.com/yxx-z/lyra/internal/db"
	"github.com/yxx-z/lyra/internal/lyrics"
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

	if cfg.Auth.Token == "" && !cfg.Auth.Disable {
		// config.yaml 通常以只读挂载，无法写回；改为持久化到可写的数据目录，
		// 使 token 在容器/进程重启后保持稳定，避免每次重启都让浏览器会话失效。
		tokenPath := filepath.Join(filepath.Dir(cfg.Database.Path), ".auth_token")
		if data, rerr := os.ReadFile(tokenPath); rerr == nil && strings.TrimSpace(string(data)) != "" {
			cfg.Auth.Token = strings.TrimSpace(string(data))
			slog.Info("已从数据目录加载持久化认证 Token", "path", tokenPath)
		} else {
			b := make([]byte, 16)
			if _, err := rand.Read(b); err != nil {
				slog.Error("生成 token 失败", "err", err)
				os.Exit(1)
			}
			cfg.Auth.Token = hex.EncodeToString(b)
			if werr := os.WriteFile(tokenPath, []byte(cfg.Auth.Token), 0o600); werr != nil {
				slog.Warn("持久化 Token 失败，重启后会话将失效", "path", tokenPath, "err", werr)
			} else {
				slog.Info("已生成并持久化认证 Token", "path", tokenPath)
			}
		}
	}
	if cfg.Auth.Password == "" && !cfg.Auth.Disable {
		slog.Warn("auth.password 未设置，请在 config.yaml 中配置登录密码")
	}

	lyricsProviders := []lyrics.Provider{lyrics.NewEmbeddedProvider()}
	if cfg.Scraper.Netease.Enabled {
		lyricsProviders = append(lyricsProviders, lyrics.NewNeteaseProvider(nil))
	}
	lyricsProviders = append(lyricsProviders, lyrics.NewLRCLIBClient("https://lrclib.net", cfg.Scraper.MusicBrainz.UserAgent, nil))
	lyricsService := lyrics.NewLyricsService(database, lyricsProviders...)
	sc := scanner.NewScanner(database, cfg.Library, cfg.Transcode.FfprobePath, lyricsService, cfg.Scraper.Enabled)
	if err := sc.Start(); err != nil {
		slog.Error("启动扫描器失败", "err", err)
		os.Exit(1)
	}
	defer sc.Stop()

	router := api.NewRouter(sc, database, cfg)
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
