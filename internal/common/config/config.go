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
	Redis    RedisConfig
	SMS      SMSConfig
	Email    EmailConfig
	Razorpay RazorpayConfig
	Push     PushConfig
	Client   ClientAppConfig
}

// ClientAppConfig is the mobile/web app version policy served at
// GET /api/v1/app/version-policy. Versions are semantic (1.4.2).
type ClientAppConfig struct {
	// MinVersion: older apps must update before they can be used.
	MinVersion string
	// LatestVersion: newer releases are offered, not forced.
	LatestVersion    string
	UpdateURLAndroid string
	UpdateURLIOS     string
}

type AppConfig struct {
	Name    string
	Env     string
	Version string
	// AllowMockProviders permits mock SMS/email/payments in staging. It has no
	// effect in production, where every provider must be live.
	AllowMockProviders bool
	// DocsEnabled serves /swagger and /openapi.yaml; off by default in
	// production-like environments.
	DocsEnabled bool
}

type HTTPConfig struct {
	Host               string
	Port               int
	CORSAllowedOrigins []string
	ReadHeaderTimeout  time.Duration
	ReadTimeout        time.Duration
	WriteTimeout       time.Duration
	IdleTimeout        time.Duration
	MaxBodyBytes       int64 // JSON and other bodies
	MaxUploadBytes     int64 // multipart uploads (avatar, quotation PDF)
	// TrustedProxies lists the load balancers / reverse proxies (CIDRs or IPs)
	// whose X-Forwarded-For header is believed. Empty = trust no proxy header.
	TrustedProxies []string
	// AdminAllowedCIDRs: networks ADMIN accounts may be used from (empty = any).
	AdminAllowedCIDRs []string
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
	// TimeZone is the session time zone: the business calendar for
	// CURRENT_DATE, date-based public codes and date columns. Timestamps are
	// timestamptz, so stored instants do not depend on it.
	TimeZone string
}

type JWTConfig struct {
	Secret string
	// DeliveryCodeSecret derives the 4-digit delivery codes. Separate from
	// Secret so rotating the JWT key does not change codes of trips on the way.
	DeliveryCodeSecret string
	Issuer             string
	AccessTTL          time.Duration
	RefreshTTL         time.Duration
	// AdminRefreshTTL: admin sessions end sooner (a stolen admin token is
	// worth the most); they sign in again with an OTP.
	AdminRefreshTTL time.Duration
	SigningMethod   string
}

type StorageConfig struct {
	Endpoint        string // "localhost:9000" for MinIO, "s3.amazonaws.com" for AWS
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          bool
	Region          string
	PublicURL       string // base URL for constructing permanent file URLs
	BucketPrefix    string // e.g. "greenroot-prod-": S3 bucket names are global
	UseIAMRole      bool   // AWS: credentials from the EC2 instance role, no keys
}

// SMSConfig selects the outbound SMS gateway. DLT identifiers (PE ID, headers,
// template IDs) are managed in the database via the admin UI, not here.
type SMSConfig struct {
	Provider string // "mock" (default) never sends
}

// EmailConfig selects the outbound email gateway. Sender identity and
// templates are managed in the database via the admin UI.
type EmailConfig struct {
	Provider string // "mock" (default) never sends
}

// RazorpayConfig: both keys set = real Razorpay checkout for subscriptions;
// either empty = mock payments (local dev, journeys).
// PushConfig: FCM HTTP v1 with a Google service account (JSON key).
type PushConfig struct {
	FCMServiceAccountJSON string
}

type RazorpayConfig struct {
	KeyID     string
	KeySecret string
	// WebhookSecret signs Razorpay webhooks (Dashboard → Webhooks); not the key secret.
	WebhookSecret string
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

// devCORSOrigins are the local admin/web dev servers. They are never a
// default in production, where CORS_ALLOWED_ORIGINS must be set explicitly.
var devCORSOrigins = []string{
	"http://localhost:5173", "http://127.0.0.1:5173",
	"http://localhost:3000", "http://127.0.0.1:3000",
	"http://localhost:4040", "http://127.0.0.1:4040",
	"http://localhost:7080", "http://127.0.0.1:7080",
}

func Load() Config {
	// No default: a forgotten APP_ENV must not start the API with the
	// development secrets and mock OTP (Validate rejects an empty value).
	appEnv := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	productionLike := appEnv == "production" || appEnv == "prod" || appEnv == "staging"
	defaultOrigins := devCORSOrigins
	if productionLike || appEnv == "" {
		defaultOrigins = nil
	}
	return Config{
		App: AppConfig{
			Name:    getString("APP_NAME", "greenroot-api"),
			Env:     appEnv,
			Version: getString("APP_VERSION", "0.1.0"),

			AllowMockProviders: getBool("ALLOW_MOCK_PROVIDERS", false),
			DocsEnabled:        getBool("DOCS_ENABLED", !productionLike),
		},
		HTTP: HTTPConfig{
			Host:               getString("HTTP_HOST", "0.0.0.0"),
			Port:               getInt("HTTP_PORT", 8080),
			CORSAllowedOrigins: getStringSlice("CORS_ALLOWED_ORIGINS", defaultOrigins),
			ReadHeaderTimeout:  getDuration("HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
			ReadTimeout:        getDuration("HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:       getDuration("HTTP_WRITE_TIMEOUT", 15*time.Second),
			IdleTimeout:        getDuration("HTTP_IDLE_TIMEOUT", 60*time.Second),
			MaxBodyBytes:       int64(getInt("HTTP_MAX_BODY_BYTES", 1<<20)),    // 1 MB
			MaxUploadBytes:     int64(getInt("HTTP_MAX_UPLOAD_BYTES", 12<<20)), // 10 MB PDF + form overhead
			TrustedProxies:     getStringSlice("TRUSTED_PROXIES", nil),
			AdminAllowedCIDRs:  getStringSlice("ADMIN_ALLOWED_CIDRS", nil),
		},
		Client: ClientAppConfig{
			MinVersion:       getString("CLIENT_MIN_VERSION", "1.0.0"),
			LatestVersion:    getString("CLIENT_LATEST_VERSION", "1.0.0"),
			UpdateURLAndroid: getString("CLIENT_UPDATE_URL_ANDROID", ""),
			UpdateURLIOS:     getString("CLIENT_UPDATE_URL_IOS", ""),
		},
		Logging: LoggingConfig{
			Dir:           getString("LOG_DIR", "logs"),
			RetentionDays: getInt("LOG_RETENTION_DAYS", 90),
			Level:         getString("LOG_LEVEL", "info"),
		},
		Database: DatabaseConfig{
			URL:             getString("DATABASE_URL", ""),
			MaxOpenConns:    getInt("DB_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    getInt("DB_MAX_IDLE_CONNS", 5),
			ConnMaxLifetime: getDuration("DB_CONN_MAX_LIFETIME", 5*time.Minute),
			ConnectTimeout:  getDuration("DB_CONNECT_TIMEOUT", 5*time.Second),
			TimeZone:        getString("DB_TIMEZONE", "Asia/Kolkata"),
		},
		JWT: JWTConfig{
			Secret:             getString("JWT_SECRET", devJWTSecret),
			DeliveryCodeSecret: getString("DELIVERY_CODE_SECRET", ""),
			Issuer:             getString("JWT_ISSUER", "greenroot-api"),
			AccessTTL:          getDuration("JWT_ACCESS_TTL", 15*time.Minute),
			RefreshTTL:         getDuration("JWT_REFRESH_TTL", 720*time.Hour),
			AdminRefreshTTL:    getDuration("JWT_ADMIN_REFRESH_TTL", 12*time.Hour),
			SigningMethod:      getString("JWT_SIGNING_METHOD", "HS256"),
		},
		Storage: StorageConfig{
			Endpoint:        getString("STORAGE_ENDPOINT", "localhost:9000"),
			AccessKeyID:     getString("STORAGE_ACCESS_KEY", "greenroot"),
			SecretAccessKey: getString("STORAGE_SECRET_KEY", "greenroot123"),
			UseSSL:          getBool("STORAGE_USE_SSL", false),
			Region:          getString("STORAGE_REGION", "us-east-1"),
			PublicURL:       getString("STORAGE_PUBLIC_URL", "http://localhost:9000"),
			BucketPrefix:    getString("STORAGE_BUCKET_PREFIX", ""),
			UseIAMRole:      getBool("STORAGE_USE_IAM_ROLE", false),
		},
		Redis: RedisConfig{
			Addr:     getString("REDIS_ADDR", ""),
			Password: getString("REDIS_PASSWORD", ""),
			DB:       getInt("REDIS_DB", 0),
		},
		SMS: SMSConfig{
			Provider: getString("SMS_PROVIDER", "mock"),
		},
		Email: EmailConfig{
			Provider: getString("EMAIL_PROVIDER", "mock"),
		},
		Razorpay: RazorpayConfig{
			KeyID:         getString("RAZORPAY_KEY_ID", ""),
			KeySecret:     getString("RAZORPAY_KEY_SECRET", ""),
			WebhookSecret: getString("RAZORPAY_WEBHOOK_SECRET", ""),
		},
		Push: PushConfig{
			FCMServiceAccountJSON: getString("FCM_SERVICE_ACCOUNT_JSON", ""),
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
