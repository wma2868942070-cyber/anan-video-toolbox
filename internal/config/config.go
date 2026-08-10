package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wma2868942070-cyber/anan-video-toolbox/internal/store"
)

type Config struct {
	Host        string
	Port        string
	DBPath      string
	ModelFile   string
	APIKey      string
	CORSOrigins string
	CanvasDir   string
}

func Load() Config {
	cwd, _ := os.Getwd()

	cfg := Config{
		Host:        envOrCompat("ANAN_VIDEO_TOOLBOX_HOST", "LEOSTUDIO_HOST", "127.0.0.1"),
		Port:        envOrCompat("ANAN_VIDEO_TOOLBOX_PORT", "LEOSTUDIO_PORT", "8001"),
		DBPath:      envOrCompat("ANAN_VIDEO_TOOLBOX_DB", "LEOSTUDIO_DB", defaultDBPath(cwd)),
		ModelFile:   envOrCompat("ANAN_VIDEO_TOOLBOX_MODEL_FILE", "LEOSTUDIO_MODEL_FILE", filepath.Join(cwd, "model_id.txt")),
		APIKey:      envOrCompat("ANAN_VIDEO_TOOLBOX_API_KEY", "LEOSTUDIO_API_KEY", ""),
		CORSOrigins: envOrCompat("ANAN_VIDEO_TOOLBOX_CORS_ORIGINS", "LEOSTUDIO_CORS_ORIGINS", "*"),
		CanvasDir:   envOrCompat("ANAN_VIDEO_TOOLBOX_CANVAS_DIR", "LEOSTUDIO_CANVAS_DIR", filepath.Join(cwd, "dist", "infinite-canvas")),
	}
	return cfg
}

func (c Config) Addr() string {
	return fmt.Sprintf("%s:%s", c.Host, c.Port)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrCompat(key, legacyKey, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return envOr(legacyKey, fallback)
}

func defaultDBPath(cwd string) string {
	cfg, err := os.UserConfigDir()
	if err != nil || cfg == "" {
		return filepath.Join(cwd, "data", "app.db")
	}
	current := filepath.Join(cfg, "anan-video-toolbox", "app.db")
	legacy := filepath.Join(cfg, "leostudio", "app.db")
	return store.PreferredDBPath(current, legacy)
}
