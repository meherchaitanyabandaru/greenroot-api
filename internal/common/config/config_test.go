package config

import "testing"

func TestLoadUsesDefaults(t *testing.T) {
	t.Setenv("APP_NAME", "")
	t.Setenv("HTTP_PORT", "")
	t.Setenv("LOG_RETENTION_DAYS", "")

	cfg := Load()

	if cfg.App.Name != "greenroot-api" {
		t.Fatalf("expected default app name, got %q", cfg.App.Name)
	}
	if cfg.HTTP.Port != 8080 {
		t.Fatalf("expected default port 8080, got %d", cfg.HTTP.Port)
	}
	if cfg.Logging.RetentionDays != 90 {
		t.Fatalf("expected default retention 90, got %d", cfg.Logging.RetentionDays)
	}
}

func TestHTTPAddr(t *testing.T) {
	cfg := HTTPConfig{Host: "127.0.0.1", Port: 9090}

	if got := cfg.Addr(); got != "127.0.0.1:9090" {
		t.Fatalf("expected addr 127.0.0.1:9090, got %q", got)
	}
}

func TestLoadParsesCORSAllowedOrigins(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://localhost:5173, http://127.0.0.1:5173")

	cfg := Load()

	if len(cfg.HTTP.CORSAllowedOrigins) != 2 {
		t.Fatalf("expected two origins, got %d", len(cfg.HTTP.CORSAllowedOrigins))
	}
	if cfg.HTTP.CORSAllowedOrigins[0] != "http://localhost:5173" {
		t.Fatalf("expected first localhost origin, got %q", cfg.HTTP.CORSAllowedOrigins[0])
	}
	if cfg.HTTP.CORSAllowedOrigins[1] != "http://127.0.0.1:5173" {
		t.Fatalf("expected second localhost origin, got %q", cfg.HTTP.CORSAllowedOrigins[1])
	}
}
