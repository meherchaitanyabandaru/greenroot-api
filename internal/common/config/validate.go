package config

import (
	"fmt"
	"strings"
	"time"

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

// IsProduction reports whether production safety rules apply.
func (c Config) IsProduction() bool {
	env := strings.ToLower(strings.TrimSpace(c.App.Env))
	return env == "production" || env == "prod"
}

// Validate returns every unsafe setting for production, so one failed start
// lists all of them. Outside production only malformed values are rejected:
// local and test setups keep their convenient defaults.
//
// Mock SMS/email can be allowed explicitly with ALLOW_MOCK_PROVIDERS=true
// (e.g. a closed beta before DLT registration). Nothing else is overridable.
func (c Config) Validate() error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

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
	if c.Database.URL == "" {
		add("DATABASE_URL is required")
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
	if !c.App.AllowMockProviders {
		if isMock(c.SMS.Provider) {
			add("SMS_PROVIDER is mock: OTP is the fixed dev code 123456 for every account (set ALLOW_MOCK_PROVIDERS=true only for a closed beta)")
		}
		if isMock(c.Email.Provider) {
			add("EMAIL_PROVIDER is mock: no email is delivered (set ALLOW_MOCK_PROVIDERS=true only for a closed beta)")
		}
		if c.Razorpay.KeyID == "" || c.Razorpay.KeySecret == "" {
			add("RAZORPAY_KEY_ID / RAZORPAY_KEY_SECRET are not set: subscription payments are mock and owners renew without paying (set ALLOW_MOCK_PROVIDERS=true only for a closed beta)")
		} else if strings.HasPrefix(c.Razorpay.KeyID, "rzp_test_") {
			add("RAZORPAY_KEY_ID is a test key: no real money is collected (use live keys, or ALLOW_MOCK_PROVIDERS=true for a closed beta)")
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
