// internal/api/router.go
package api

import (
	"database/sql"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/yxx-z/lyra/internal/api/middleware"
	"github.com/yxx-z/lyra/internal/api/subsonic"
	v1 "github.com/yxx-z/lyra/internal/api/v1"
	"github.com/yxx-z/lyra/internal/auth"
	"github.com/yxx-z/lyra/internal/config"
	lyricspkg "github.com/yxx-z/lyra/internal/lyrics"
	metadatapkg "github.com/yxx-z/lyra/internal/metadata"
	"github.com/yxx-z/lyra/internal/playlists"
	"github.com/yxx-z/lyra/internal/scanner"
	"github.com/yxx-z/lyra/internal/transcode"
	"github.com/yxx-z/lyra/internal/userdata"
	"github.com/yxx-z/lyra/ui"
)

const version = "0.1.0"

// NewRouter builds the application router.
func NewRouter(s *scanner.Scanner, db *sql.DB, cfg *config.Config) http.Handler {
	r := chi.NewRouter()
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)

	tcache := transcode.NewCache(cfg.Cache.TranscodeDir, cfg.Cache.TranscodeMaxSizeMB)
	tsvc := transcode.NewService(cfg.Transcode.FFmpegPath, cfg.Transcode.DefaultBitrate, tcache)
	streamH := v1.NewStreamHandler(db, tsvc)

	keyPath := filepath.Join(filepath.Dir(cfg.Database.Path), "secret.key")
	key, err := auth.LoadOrCreateKey(keyPath)
	if err != nil {
		panic("加载主密钥失败: " + err.Error())
	}
	users := auth.NewUserStore(db)
	sessions := auth.NewSessionStore(db)
	settings := auth.NewSettingsStore(db)
	udStore := userdata.NewStore(db)
	plStore := playlists.NewStore(db)

	r.Get("/health", handleHealth)

	authH := v1.NewAuthHandler(users, sessions)
	setupH := v1.NewSetupHandler(users, sessions, db)
	accountH := v1.NewAccountHandler(users, key)
	starH := v1.NewStarHandler(db, udStore)
	plH := v1.NewPlaylistHandler(db, plStore)
	adminH := v1.NewAdminHandler(users, settings)
	registerH := v1.NewRegisterHandler(users, sessions, settings)
	r.Post("/api/v1/auth/login", authH.Login)
	r.Post("/api/v1/auth/logout", authH.Logout)
	r.Get("/api/v1/setup/status", setupH.Status)
	r.Post("/api/v1/setup", setupH.Create)
	r.Get("/api/v1/register/status", registerH.Status)
	r.Post("/api/v1/register", registerH.Register)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.SessionAuth(sessions, users, cfg.Auth.Disable))

		r.Get("/auth/me", authH.Me)
		r.Post("/auth/session", authH.Session)
		r.Post("/account/password", accountH.ChangePassword)
		r.Post("/account/subsonic-password", accountH.SetSubsonicPassword)

		r.Route("/admin", func(r chi.Router) {
			r.Use(middleware.RequireAdmin)
			r.Get("/users", adminH.ListUsers)
			r.Post("/users", adminH.CreateUser)
			r.Delete("/users/{id}", adminH.DeleteUser)
			r.Post("/users/{id}/password", adminH.ResetPassword)
			r.Post("/users/{id}/role", adminH.SetRole)
			r.Get("/settings", adminH.GetSettings)
			r.Post("/settings", adminH.SetSettings)
		})

		lib := v1.NewLibraryHandler(s)
		r.Post("/library/scan", lib.TriggerScan)
		r.Get("/library/scan/status", lib.ScanStatus)

		albums := v1.NewAlbumsHandler(db, udStore)
		r.Get("/albums", albums.ListAlbums)
		r.Get("/albums/{id}", albums.GetAlbum)

		artists := v1.NewArtistsHandler(db)
		r.Get("/artists", artists.ListArtists)
		r.Get("/artists/{id}", artists.GetArtist)

		cover := v1.NewCoverHandler(db)
		r.Get("/cover/{id}", cover.GetCover)

		r.Get("/tracks/{id}/stream", streamH.Stream)

		lyrics := v1.NewLyricsHandler(db)
		r.Get("/tracks/{id}/lyrics", lyrics.GetLyrics)
		r.Put("/tracks/{id}/lyrics", lyrics.PutLyrics)
		r.Delete("/tracks/{id}/lyrics", lyrics.DeleteLyrics)

		lyricsService := lyricspkg.NewLyricsService(
			db,
			lyricspkg.NewEmbeddedProvider(),
			lyricspkg.NewLRCLIBClient("https://lrclib.net", cfg.Scraper.MusicBrainz.UserAgent, nil),
		)
		scrape := v1.NewScrapeHandler(lyricsService)
		r.Post("/tracks/{id}/scrape", scrape.ScrapeTrack)
		r.Post("/tracks/{id}/lyrics/upgrade", scrape.UpgradeLyrics)

		metadataService := metadatapkg.NewMetadataService(
			db,
			metadatapkg.NewMusicBrainzClient("https://musicbrainz.org", cfg.Scraper.MusicBrainz.UserAgent, nil),
			metadatapkg.NewCoverArtClient("https://coverartarchive.org", nil),
			cfg.Cache.ArtworkDir,
		)
		albumScrape := v1.NewAlbumScrapeHandler(metadataService)
		r.Post("/albums/{id}/scrape", albumScrape.ScrapeAlbum)

		search := v1.NewSearchHandler(db, udStore)
		r.Get("/search", search.Search)

		r.Get("/star", starH.StarStatus)
		r.Post("/star", starH.Star)
		r.Post("/unstar", starH.Unstar)
		r.Post("/tracks/{id}/scrobble", starH.Scrobble)
		r.Get("/favorites", starH.Favorites)
		r.Get("/recently-played", starH.RecentlyPlayed)
		r.Get("/most-played", starH.MostPlayed)

		r.Get("/playlists", plH.List)
		r.Post("/playlists", plH.Create)
		r.Get("/playlists/{id}", plH.Get)
		r.Patch("/playlists/{id}", plH.Update)
		r.Delete("/playlists/{id}", plH.Delete)
		r.Post("/playlists/{id}/tracks", plH.AddTracks)
		r.Put("/playlists/{id}/tracks", plH.ReplaceTracks)
	})

	subCover := v1.NewCoverHandler(db)
	subHandler := subsonic.NewHandler(db, cfg, streamH, subCover, users, key, udStore, plStore)
	r.Route("/rest", subHandler.RegisterRoutes)

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
