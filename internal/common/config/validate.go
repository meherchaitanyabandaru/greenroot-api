package config

import (
	"fmt"
	"strings"
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
// lists all of them. Outside production it returns nil: local and test
// setups keep their convenient defaults.
//
// Mock SMS/email can be allowed explicitly with ALLOW_MOCK_PROVIDERS=true
// (e.g. a closed beta before DLT registration). Nothing else is overridable.
func (c Config) Validate() error {
	if !c.IsProduction() {
		return nil
	}
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
	}
	if len(errs) == 0 {
		return nil
	}
	lines := make([]string, len(errs))
	for i, err := range errs {
		lines[i] = "  - " + err.Error()
	}
	return &UnsafeConfigError{Problems: errs, msg: "unsafe production configuration:\n" + strings.Join(lines, "\n")}
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
