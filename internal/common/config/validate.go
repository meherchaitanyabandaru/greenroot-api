package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/clientip"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/semver"
)

// Defaults that are only acceptable for local development. Validate rejects
// them in production; they are not secrets.
// #nosec G101 -- known public dev defaults, used only to detect misconfiguration.
const (
	devJWTSecret      = "local-dev-change-me"
	devStorageKey     = "greenroot"
	devStorageSecret  = "greenroot123"
	minJWTSecretBytes = 32
)

// Environments the API knows. Anything else (including empty) refuses to
// start: a typo must not silently fall back to development behaviour.
var knownEnvs = map[string]bool{
	"local": true, "development": true, "dev": true, "test": true, "ci": true,
	"staging": true, "production": true, "prod": true,
}

func (c Config) env() string { return strings.ToLower(strings.TrimSpace(c.App.Env)) }

// IsProduction reports whether production safety rules apply. Staging runs
// under the same rules (same infrastructure, real secrets) so problems show
// up there first; see IsStaging for the few things it may relax.
func (c Config) IsProduction() bool {
	env := c.env()
	return env == "production" || env == "prod" || env == "staging"
}

// IsStaging: production rules, but mock providers may be allowed and
// Razorpay test keys are expected.
func (c Config) IsStaging() bool { return c.env() == "staging" }

// DeliveryCodeKey is the HMAC key for delivery codes. Outside production an
// unset DELIVERY_CODE_SECRET falls back to the JWT secret (Validate requires
// a separate one in production).
func (c Config) DeliveryCodeKey() []byte {
	if c.JWT.DeliveryCodeSecret != "" {
		return []byte(c.JWT.DeliveryCodeSecret)
	}
	return []byte("delivery-code|" + c.JWT.Secret)
}

// Validate returns every unsafe setting for production, so one failed start
// lists all of them. Outside production only malformed values are rejected:
// local and test setups keep their convenient defaults.
//
// Mock SMS/email/payments can be allowed in staging with
// ALLOW_MOCK_PROVIDERS=true. Nothing is overridable in production.
func (c Config) Validate() error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if !knownEnvs[c.env()] {
		add("APP_ENV must be set to one of local, development, test, ci, staging or production (got %q)", c.App.Env)
	}
	// A typo here would silently trust nobody (or the wrong network), so it is
	// an error in every environment.
	if _, err := clientip.ParsePrefixes(c.HTTP.TrustedProxies); err != nil {
		add("TRUSTED_PROXIES: %v", err)
	}
	// Empty keeps the server's zone; a typo must not silently do the same.
	if tz := c.Database.TimeZone; tz != "" {
		if _, err := time.LoadLocation(tz); err != nil {
			add("DB_TIMEZONE %q is not an IANA time zone (e.g. Asia/Kolkata)", tz)
		}
	}
	// A typo would silently disable forced updates. Empty means unset.
	minV, errMin := semver.Parse(c.Client.MinVersion)
	latestV, errLatest := semver.Parse(c.Client.LatestVersion)
	if c.Client.MinVersion != "" && errMin != nil {
		add("CLIENT_MIN_VERSION: %v", errMin)
	}
	if c.Client.LatestVersion != "" && errLatest != nil {
		add("CLIENT_LATEST_VERSION: %v", errLatest)
	}
	if errMin == nil && errLatest == nil && latestV.Less(minV) {
		add("CLIENT_LATEST_VERSION %s is older than CLIENT_MIN_VERSION %s", c.Client.LatestVersion, c.Client.MinVersion)
	}
	if c.IsProduction() {
		errs = append(errs, c.productionProblems()...)
	}
	if len(errs) == 0 {
		return nil
	}
	label := "invalid configuration"
	if c.IsProduction() {
		label = "unsafe production configuration"
	}
	lines := make([]string, len(errs))
	for i, err := range errs {
		lines[i] = "  - " + err.Error()
	}
	return &UnsafeConfigError{Problems: errs, msg: label + ":\n" + strings.Join(lines, "\n")}
}

func (c Config) productionProblems() []error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if c.JWT.Secret == devJWTSecret || len(c.JWT.Secret) < minJWTSecretBytes {
		add("JWT_SECRET must be set to a random value of at least %d bytes (anyone can forge tokens with the default)", minJWTSecretBytes)
	}
	if len(c.JWT.DeliveryCodeSecret) < minJWTSecretBytes || c.JWT.DeliveryCodeSecret == c.JWT.Secret {
		add("DELIVERY_CODE_SECRET must be a random value of at least %d bytes, different from JWT_SECRET", minJWTSecretBytes)
	}
	if c.Database.URL == "" {
		add("DATABASE_URL is required")
	}
	// Behind a load balancer or reverse proxy every client would otherwise
	// share the proxy's IP: one rate-limit bucket for the whole country.
	if len(c.HTTP.TrustedProxies) == 0 {
		add("TRUSTED_PROXIES is required: list the reverse proxy / load balancer CIDRs (use 127.0.0.1/32 if clients connect directly)")
	}
	if c.Redis.Addr == "" {
		add("REDIS_ADDR is required (OTP storage, rate limits and token revocation depend on Redis)")
	}
	if len(c.HTTP.CORSAllowedOrigins) == 0 {
		add("CORS_ALLOWED_ORIGINS must list the admin UI origin(s)")
	}
	if c.HTTP.MaxBodyBytes <= 0 || c.HTTP.MaxUploadBytes <= 0 {
		add("HTTP_MAX_BODY_BYTES and HTTP_MAX_UPLOAD_BYTES must be positive")
	}
	if c.Storage.AccessKeyID == devStorageKey || c.Storage.SecretAccessKey == devStorageSecret {
		add("STORAGE_ACCESS_KEY / STORAGE_SECRET_KEY are the local MinIO defaults")
	}
	if !c.Storage.UseSSL {
		add("STORAGE_USE_SSL must be true: files and credentials would cross the network unencrypted")
	}
	if !strings.HasPrefix(c.Storage.PublicURL, "https://") {
		add("STORAGE_PUBLIC_URL must start with https://")
	}
	if c.Database.URL != "" && !databaseEncrypted(c.Database.URL) {
		add("DATABASE_URL must use sslmode=require, verify-ca or verify-full (the default \"prefer\" silently falls back to plaintext); a Unix socket or localhost is fine")
	}
	// Production never runs mock providers: mock SMS accepts the fixed OTP
	// 123456 for every account (admins included) and mock payments renew
	// subscriptions for free. Staging may opt in with ALLOW_MOCK_PROVIDERS.
	if !(c.IsStaging() && c.App.AllowMockProviders) {
		if isMock(c.SMS.Provider) {
			add("SMS_PROVIDER is mock: OTP is the fixed dev code 123456 for every account")
		}
		if isMock(c.Email.Provider) {
			add("EMAIL_PROVIDER is mock: no email is delivered")
		}
		if c.Razorpay.KeyID == "" || c.Razorpay.KeySecret == "" {
			add("RAZORPAY_KEY_ID / RAZORPAY_KEY_SECRET are not set: subscription payments are mock and owners renew without paying")
		} else if strings.HasPrefix(c.Razorpay.KeyID, "rzp_test_") && !c.IsStaging() {
			add("RAZORPAY_KEY_ID is a test key: no real money is collected (test keys belong in staging)")
		}
	}
	if c.Razorpay.KeyID != "" && c.Razorpay.WebhookSecret == "" {
		add("RAZORPAY_WEBHOOK_SECRET is required with Razorpay keys: without the webhook a payment made while the app closed is charged but never applied")
	}
	return errs
}

// UnsafeConfigError lists every problem found by Validate.
type UnsafeConfigError struct {
	Problems []error
	msg      string
}

func (e *UnsafeConfigError) Error() string   { return e.msg }
func (e *UnsafeConfigError) Unwrap() []error { return e.Problems }

func isMock(provider string) bool {
	p := strings.ToLower(strings.TrimSpace(provider))
	return p == "" || p == "mock"
}

// databaseEncrypted reports whether every connection pgx may make to the
// database is TLS, or never leaves the machine (Unix socket / localhost).
func databaseEncrypted(dsn string) bool {
	cfg, err := pgconn.ParseConfig(dsn)
	if err != nil {
		return true // a malformed URL fails at connect; not this check's job
	}
	if strings.HasPrefix(cfg.Host, "/") || cfg.Host == "localhost" || cfg.Host == "127.0.0.1" || cfg.Host == "::1" {
		return true
	}
	if cfg.TLSConfig == nil {
		return false
	}
	for _, fb := range cfg.Fallbacks {
		if fb.TLSConfig == nil {
			return false // "prefer"/"allow": plaintext fallback
		}
	}
	return true
}
