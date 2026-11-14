package config

import (
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Port != "8080" || cfg.BaseURL != "http://localhost:8080" {
		t.Errorf("unexpected port/base url: %q %q", cfg.Port, cfg.BaseURL)
	}
	if cfg.LinkTTL != 24*time.Hour || cfg.RateLimit != 10 || cfg.RateWindow != time.Minute {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
}

func TestLoad_FromEnv(t *testing.T) {
	t.Setenv("PORT", "9000")
	t.Setenv("LINK_TTL", "90m")
	t.Setenv("RATE_LIMIT", "3")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Port != "9000" || cfg.BaseURL != "http://localhost:9000" {
		t.Errorf("unexpected port/base url: %q %q", cfg.Port, cfg.BaseURL)
	}
	if cfg.LinkTTL != 90*time.Minute || cfg.RateLimit != 3 {
		t.Errorf("unexpected values: %+v", cfg)
	}
}

func TestLoad_Invalid(t *testing.T) {
	tests := map[string]string{
		"LINK_TTL":    "tomorrow",
		"RATE_LIMIT":  "-5",
		"RATE_WINDOW": "0s",
	}
	for key, value := range tests {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, value)
			if _, err := Load(); err == nil {
				t.Fatalf("Load() with %s=%q must fail", key, value)
			}
		})
	}
}
