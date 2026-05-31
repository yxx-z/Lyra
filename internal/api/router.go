// internal/api/router.go
package api

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	v1 "github.com/yxx-z/lyra/internal/api/v1"
	"github.com/yxx-z/lyra/internal/scanner"
	"github.com/yxx-z/lyra/ui"
)

const version = "0.1.0"

// NewRouter builds the application router. s must not be nil.
func NewRouter(s *scanner.Scanner) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", handleHealth)

	lib := v1.NewLibraryHandler(s)
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/library/scan", lib.TriggerScan)
		r.Get("/library/scan/status", lib.ScanStatus)
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
