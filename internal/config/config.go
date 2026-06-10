// internal/config/config.go
package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Auth      AuthConfig      `yaml:"auth"`
	Library   LibraryConfig   `yaml:"library"`
	Database  DatabaseConfig  `yaml:"database"`
	Cache     CacheConfig     `yaml:"cache"`
	Scraper   ScraperConfig   `yaml:"scraper"`
	Transcode TranscodeConfig `yaml:"transcode"`
	Subsonic  SubsonicConfig  `yaml:"subsonic"`
}

type ServerConfig struct {
	Host    string `yaml:"host"`
	Port    int    `yaml:"port"`
	BaseURL string `yaml:"base_url"`
}

type AuthConfig struct {
	Disable  bool   `yaml:"disable"`
	Token    string `yaml:"token"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type LibraryConfig struct {
	Paths        []string `yaml:"paths"`
	ScanInterval int      `yaml:"scan_interval"`
	Watch        bool     `yaml:"watch"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type CacheConfig struct {
	ArtworkDir         string `yaml:"artwork_dir"`
	ArtworkMaxSizeMB   int    `yaml:"artwork_max_size_mb"`
	TranscodeDir       string `yaml:"transcode_dir"`
	TranscodeMaxSizeMB int    `yaml:"transcode_max_size_mb"`
}

type ScraperConfig struct {
	Enabled     bool              `yaml:"enabled"`
	MusicBrainz MusicBrainzConfig `yaml:"musicbrainz"`
	LastFM      LastFMConfig      `yaml:"lastfm"`
	AcoustID    AcoustIDConfig    `yaml:"acoustid"`
	Netease     NeteaseConfig     `yaml:"netease"`
	Spotify     SpotifyConfig     `yaml:"spotify"`
}

type MusicBrainzConfig struct {
	UserAgent string `yaml:"user_agent"`
}

type LastFMConfig struct {
	APIKey string `yaml:"api_key"`
}

type AcoustIDConfig struct {
	APIKey     string `yaml:"api_key"`
	FpcalcPath string `yaml:"fpcalc_path"`
}

type NeteaseConfig struct {
	Enabled bool `yaml:"enabled"`
}

type SpotifyConfig struct {
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
}

type TranscodeConfig struct {
	FFmpegPath     string `yaml:"ffmpeg_path"`
	FfprobePath    string `yaml:"ffprobe_path"`
	DefaultFormat  string `yaml:"default_format"`
	DefaultBitrate int    `yaml:"default_bitrate"`
}

type SubsonicConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Password string `yaml:"password"`
}

func Default() *Config {
	return &Config{
		Server:   ServerConfig{Host: "0.0.0.0", Port: 4533},
		Auth:     AuthConfig{Disable: false, Username: "admin"},
		Library:  LibraryConfig{ScanInterval: 3600, Watch: true},
		Database: DatabaseConfig{Path: "./data/music.db"},
		Cache:    CacheConfig{ArtworkDir: "./data/artwork", ArtworkMaxSizeMB: 2048, TranscodeDir: "./data/transcode", TranscodeMaxSizeMB: 2048},
		Scraper:  ScraperConfig{Enabled: true, Netease: NeteaseConfig{Enabled: true}, AcoustID: AcoustIDConfig{FpcalcPath: "fpcalc"}},
		Transcode: TranscodeConfig{
			FFmpegPath:     "ffmpeg",
			FfprobePath:    "ffprobe",
			DefaultFormat:  "mp3",
			DefaultBitrate: 192,
		},
		Subsonic: SubsonicConfig{Enabled: true},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, fmt.Errorf("打开配置文件 %q: %w", path, err)
	}
	defer f.Close()
	if err := yaml.NewDecoder(f).Decode(cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件: %w", err)
	}
	return cfg, nil
}
