package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port            string
	BaseURL         string
	RedisAddr       string
	LinkTTL         time.Duration
	RateLimit       int64
	RateWindow      time.Duration
	ShutdownTimeout time.Duration
}

func Load() (Config, error) {
	var (
		cfg Config
		err error
	)

	cfg.Port = getString("PORT", "8080")
	cfg.BaseURL = getString("BASE_URL", "http://localhost:"+cfg.Port)
	cfg.RedisAddr = getString("REDIS_ADDR", "localhost:6379")

	if cfg.LinkTTL, err = getDuration("LINK_TTL", 24*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.RateWindow, err = getDuration("RATE_WINDOW", time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = getDuration("SHUTDOWN_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.RateLimit, err = getInt("RATE_LIMIT", 10); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func getString(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getDuration(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration like 30s or 5m, got %q", key, v)
	}
	return d, nil
}

func getInt(key string, def int64) (int64, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", key, v)
	}
	return n, nil
}
