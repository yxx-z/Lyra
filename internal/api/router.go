// internal/api/router.go
package api

import (
	"database/sql"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/yxx-z/lyra/internal/api/middleware"
	v1 "github.com/yxx-z/lyra/internal/api/v1"
	"github.com/yxx-z/lyra/internal/config"
	"github.com/yxx-z/lyra/internal/scanner"
	"github.com/yxx-z/lyra/ui"
)

const version = "0.1.0"

// NewRouter builds the application router.
func NewRouter(s *scanner.Scanner, db *sql.DB, cfg *config.Config) http.Handler {
	r := chi.NewRouter()
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)

	r.Get("/health", handleHealth)

	authH := v1.NewAuthHandler(cfg)
	r.Post("/api/v1/auth/login", authH.Login)
	r.Post("/api/v1/auth/logout", authH.Logout)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.BearerAuth(cfg.Auth.Token, cfg.Auth.Disable))

		r.Post("/auth/session", authH.Session)

		lib := v1.NewLibraryHandler(s)
		r.Post("/library/scan", lib.TriggerScan)
		r.Get("/library/scan/status", lib.ScanStatus)

		albums := v1.NewAlbumsHandler(db)
		r.Get("/albums", albums.ListAlbums)
		r.Get("/albums/{id}", albums.GetAlbum)

		artists := v1.NewArtistsHandler(db)
		r.Get("/artists", artists.ListArtists)
		r.Get("/artists/{id}", artists.GetArtist)

		cover := v1.NewCoverHandler(db)
		r.Get("/cover/{id}", cover.GetCover)

		stream := v1.NewStreamHandler(db, cfg.Transcode, cfg.Cache.TranscodeDir)
		r.Get("/tracks/{id}/stream", stream.Stream)

		search := v1.NewSearchHandler(db)
		r.Get("/search", search.Search)
	})

	// 所有非 API 路由返回嵌入的前端文件
	sub, err := fs.Sub(ui.Dist, "dist")
	if err != nil {
		panic("embed ui/dist 失败: " + err.Error())
	}
	r.Handle("/*", http.FileServer(http.FS(sub)))

	return r
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"version": version,
	}); err != nil {
		slog.Error("写 health 响应失败", "err", err)
	}
}
