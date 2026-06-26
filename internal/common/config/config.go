package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	App      AppConfig
	HTTP     HTTPConfig
	Logging  LoggingConfig
	Database DatabaseConfig
	JWT      JWTConfig
	Storage  StorageConfig
}

type AppConfig struct {
	Name    string
	Env     string
	Version string
}

type HTTPConfig struct {
	Host               string
	Port               int
	CORSAllowedOrigins []string
	ReadHeaderTimeout  time.Duration
	ReadTimeout        time.Duration
	WriteTimeout       time.Duration
	IdleTimeout        time.Duration
}

func (c HTTPConfig) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

type LoggingConfig struct {
	Dir           string
	RetentionDays int
	Level         string
}

type DatabaseConfig struct {
	URL             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnectTimeout  time.Duration
}

type JWTConfig struct {
	Secret        string
	Issuer        string
	AccessTTL     time.Duration
	RefreshTTL    time.Duration
	SigningMethod string
}

type StorageConfig struct {
	Endpoint        string // "localhost:9000" for MinIO, "s3.amazonaws.com" for AWS
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          bool
	Region          string
	PublicURL       string // base URL for constructing permanent file URLs
}

func Load() Config {
	return Config{
		App: AppConfig{
			Name:    getString("APP_NAME", "greenroot-api"),
			Env:     getString("APP_ENV", "local"),
			Version: getString("APP_VERSION", "0.1.0"),
		},
		HTTP: HTTPConfig{
			Host:               getString("HTTP_HOST", "0.0.0.0"),
			Port:               getInt("HTTP_PORT", 8080),
			CORSAllowedOrigins: getStringSlice("CORS_ALLOWED_ORIGINS", []string{"http://localhost:5173", "http://127.0.0.1:5173"}),
			ReadHeaderTimeout:  getDuration("HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
			ReadTimeout:        getDuration("HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:       getDuration("HTTP_WRITE_TIMEOUT", 15*time.Second),
			IdleTimeout:        getDuration("HTTP_IDLE_TIMEOUT", 60*time.Second),
		},
		Logging: LoggingConfig{
			Dir:           getString("LOG_DIR", "logs"),
			RetentionDays: getInt("LOG_RETENTION_DAYS", 90),
			Level:         getString("LOG_LEVEL", "info"),
		},
		Database: DatabaseConfig{
			URL:             getString("DATABASE_URL", ""),
			MaxOpenConns:    getInt("DB_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    getInt("DB_MAX_IDLE_CONNS", 25),
			ConnMaxLifetime: getDuration("DB_CONN_MAX_LIFETIME", 5*time.Minute),
			ConnectTimeout:  getDuration("DB_CONNECT_TIMEOUT", 5*time.Second),
		},
		JWT: JWTConfig{
			Secret:        getString("JWT_SECRET", "local-dev-change-me"),
			Issuer:        getString("JWT_ISSUER", "greenroot-api"),
			AccessTTL:     getDuration("JWT_ACCESS_TTL", 15*time.Minute),
			RefreshTTL:    getDuration("JWT_REFRESH_TTL", 720*time.Hour),
			SigningMethod: getString("JWT_SIGNING_METHOD", "HS256"),
		},
		Storage: StorageConfig{
			Endpoint:        getString("STORAGE_ENDPOINT", "localhost:9000"),
			AccessKeyID:     getString("STORAGE_ACCESS_KEY", "greenroot"),
			SecretAccessKey: getString("STORAGE_SECRET_KEY", "greenroot123"),
			UseSSL:          getBool("STORAGE_USE_SSL", false),
			Region:          getString("STORAGE_REGION", "us-east-1"),
			PublicURL:       getString("STORAGE_PUBLIC_URL", "http://localhost:9000"),
		},
	}
}

func getString(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getStringSlice(key string, fallback []string) []string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	if len(values) == 0 {
		return fallback
	}
	return values
}

func getBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
