package config

import (
	"errors"
	"strings"
	"testing"
)

func safeProduction() Config {
	return Config{
		App:      AppConfig{Env: "production"},
		HTTP:     HTTPConfig{CORSAllowedOrigins: []string{"https://admin.greenroot.in"}, MaxBodyBytes: 1 << 20, MaxUploadBytes: 12 << 20, TrustedProxies: []string{"172.16.0.0/12"}},
		Database: DatabaseConfig{URL: "postgres://app@db.internal/greenroot?sslmode=require"},
		JWT:      JWTConfig{Secret: strings.Repeat("s", 40), DeliveryCodeSecret: strings.Repeat("d", 40)},
		Storage:  StorageConfig{AccessKeyID: "prod-key", SecretAccessKey: "prod-secret", UseSSL: true, PublicURL: "https://files.greenroot.in"},
		Redis:    RedisConfig{Addr: "redis:6379"},
		SMS:      SMSConfig{Provider: "msg91"},
		Email:    EmailConfig{Provider: "ses"},
		Razorpay: RazorpayConfig{KeyID: "rzp_live_x", KeySecret: "live-secret", WebhookSecret: "wh-secret"},
		Push:     PushConfig{FCMServiceAccountJSON: `{"project_id":"greenroot"}`},
	}
}

func TestValidate_SafeProductionPasses(t *testing.T) {
	if err := safeProduction().Validate(); err != nil {
		t.Fatalf("safe production config rejected: %v", err)
	}
}

func TestValidate_LocalDefaultsAllowed(t *testing.T) {
	t.Setenv("APP_ENV", "local")
	if err := Load().Validate(); err != nil {
		t.Fatalf("local defaults must start: %v", err)
	}
}

func TestValidate_EachUnsafeSetting(t *testing.T) {
	cases := map[string]struct {
		mutate func(*Config)
		want   string
	}{
		"default jwt secret":  {func(c *Config) { c.JWT.Secret = devJWTSecret }, "JWT_SECRET"},
		"short jwt secret":    {func(c *Config) { c.JWT.Secret = "short" }, "JWT_SECRET"},
		"no database":         {func(c *Config) { c.Database.URL = "" }, "DATABASE_URL"},
		"no redis":            {func(c *Config) { c.Redis.Addr = "" }, "REDIS_ADDR"},
		"no cors":             {func(c *Config) { c.HTTP.CORSAllowedOrigins = nil }, "CORS_ALLOWED_ORIGINS"},
		"no body limit":       {func(c *Config) { c.HTTP.MaxBodyBytes = 0 }, "HTTP_MAX_BODY_BYTES"},
		"minio defaults":      {func(c *Config) { c.Storage.SecretAccessKey = devStorageSecret }, "STORAGE_"},
		"storage without tls": {func(c *Config) { c.Storage.UseSSL = false }, "STORAGE_USE_SSL"},
		"http file urls":      {func(c *Config) { c.Storage.PublicURL = "http://files.greenroot.in" }, "STORAGE_PUBLIC_URL"},
		"db sslmode default":  {func(c *Config) { c.Database.URL = "postgres://app@db.internal/greenroot" }, "sslmode"},
		"db sslmode disable":  {func(c *Config) { c.Database.URL = "postgres://app@db.internal/greenroot?sslmode=disable" }, "sslmode"},
		"db sslmode prefer":   {func(c *Config) { c.Database.URL = "postgres://app@db.internal/greenroot?sslmode=prefer" }, "sslmode"},
		"mock sms":            {func(c *Config) { c.SMS.Provider = "mock" }, "SMS_PROVIDER"},
		"mock email":          {func(c *Config) { c.Email.Provider = "" }, "EMAIL_PROVIDER"},
		"no razorpay keys":    {func(c *Config) { c.Razorpay = RazorpayConfig{} }, "RAZORPAY_KEY_ID"},
		"razorpay test key":   {func(c *Config) { c.Razorpay.KeyID = "rzp_test_x" }, "test key"},
		"no webhook secret":   {func(c *Config) { c.Razorpay.WebhookSecret = "" }, "RAZORPAY_WEBHOOK_SECRET"},
		"no push":             {func(c *Config) { c.Push.FCMServiceAccountJSON = "" }, "FCM_SERVICE_ACCOUNT_JSON"},
		"no trusted proxies":  {func(c *Config) { c.HTTP.TrustedProxies = nil }, "TRUSTED_PROXIES"},
		"no delivery secret":  {func(c *Config) { c.JWT.DeliveryCodeSecret = "" }, "DELIVERY_CODE_SECRET"},
		"delivery = jwt":      {func(c *Config) { c.JWT.DeliveryCodeSecret = c.JWT.Secret }, "DELIVERY_CODE_SECRET"},
		"mock opt-in in prod": {func(c *Config) { c.App.AllowMockProviders = true; c.SMS.Provider = "mock" }, "SMS_PROVIDER"},
	}
	for name, tc := range cases {
		cfg := safeProduction()
		tc.mutate(&cfg)
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: want error mentioning %s, got %v", name, tc.want, err)
		}
	}
}

func TestValidate_ListsAllProblemsAndAllowsMockOnlyInStaging(t *testing.T) {
	cfg := safeProduction()
	cfg.JWT.Secret, cfg.SMS.Provider, cfg.Email.Provider = devJWTSecret, "mock", "mock"
	var unsafe *UnsafeConfigError
	if err := cfg.Validate(); !errors.As(err, &unsafe) || len(unsafe.Problems) != 3 {
		t.Fatalf("want 3 problems in one error, got %v", err)
	}

	// Production ignores the opt-in: mock SMS means OTP 123456 for everyone.
	cfg.App.AllowMockProviders = true
	if err := cfg.Validate(); !errors.As(err, &unsafe) || len(unsafe.Problems) != 3 {
		t.Fatalf("production must refuse mock providers even when opted in, got %v", err)
	}

	cfg.App.Env = "staging"
	err := cfg.Validate()
	if !errors.As(err, &unsafe) || len(unsafe.Problems) != 1 || !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("staging opt-in must allow mock providers but never a weak JWT secret, got %v", err)
	}
	cfg.JWT.Secret = strings.Repeat("s", 40)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("staging with mock providers should start: %v", err)
	}
}

// On EC2 the instance role replaces storage keys; the MinIO defaults that
// remain in the unused key fields must not block the start.
func TestValidate_StorageIAMRoleNeedsNoKeys(t *testing.T) {
	cfg := safeProduction()
	cfg.Storage.AccessKeyID, cfg.Storage.SecretAccessKey = devStorageKey, devStorageSecret
	if err := cfg.Validate(); err == nil {
		t.Fatal("MinIO default keys accepted without the IAM role")
	}
	cfg.Storage.UseIAMRole = true
	if err := cfg.Validate(); err != nil {
		t.Fatalf("IAM role: %v", err)
	}
}

func TestValidate_StagingAcceptsRazorpayTestKeys(t *testing.T) {
	cfg := safeProduction()
	cfg.App.Env = "staging"
	cfg.Razorpay.KeyID = "rzp_test_x"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("staging with test keys should start: %v", err)
	}
}

// A forgotten or mistyped APP_ENV used to start in local mode with the public
// JWT secret: anyone could sign an admin token.
func TestValidate_AppEnvRequired(t *testing.T) {
	for _, env := range []string{"", "prodution", "live"} {
		c := Config{App: AppConfig{Env: env}}
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "APP_ENV") {
			t.Errorf("APP_ENV %q accepted: %v", env, err)
		}
	}
	t.Setenv("APP_ENV", "")
	if err := Load().Validate(); err == nil || !strings.Contains(err.Error(), "APP_ENV") {
		t.Fatalf("Load without APP_ENV must not validate: %v", err)
	}
}

func TestIsProduction(t *testing.T) {
	for env, want := range map[string]bool{"production": true, "PROD": true, " prod ": true, "staging": true, "local": false, "": false} {
		if got := (Config{App: AppConfig{Env: env}}).IsProduction(); got != want {
			t.Errorf("IsProduction(%q) = %v", env, got)
		}
	}
}

func TestDeliveryCodeKey(t *testing.T) {
	c := Config{JWT: JWTConfig{Secret: "jwt"}}
	if string(c.DeliveryCodeKey()) == "jwt" {
		t.Fatal("fallback key must not be the JWT secret itself")
	}
	c.JWT.DeliveryCodeSecret = "own"
	if string(c.DeliveryCodeKey()) != "own" {
		t.Fatal("DELIVERY_CODE_SECRET must be used when set")
	}
}

func TestValidate_TrustedProxies(t *testing.T) {
	local := Config{App: AppConfig{Env: "local"}}

	local.HTTP.TrustedProxies = []string{"10.0.0.0/8", "127.0.0.1"}
	if err := local.Validate(); err != nil {
		t.Fatalf("valid TRUSTED_PROXIES rejected: %v", err)
	}

	// A typo is rejected in every environment, not only production.
	local.HTTP.TrustedProxies = []string{"10.0.0.0/33"}
	err := local.Validate()
	if err == nil || !strings.Contains(err.Error(), "TRUSTED_PROXIES") {
		t.Fatalf("malformed TRUSTED_PROXIES in local: err = %v", err)
	}

	prod := safeProduction()
	prod.HTTP.TrustedProxies = []string{"not-an-ip"}
	err = prod.Validate()
	if err == nil || !strings.Contains(err.Error(), "TRUSTED_PROXIES") {
		t.Fatalf("malformed TRUSTED_PROXIES in production: err = %v", err)
	}
}

func TestValidate_DBTimeZone(t *testing.T) {
	c := Config{App: AppConfig{Env: "local"}}
	for _, tz := range []string{"", "Asia/Kolkata", "UTC"} {
		c.Database.TimeZone = tz
		if err := c.Validate(); err != nil {
			t.Fatalf("DB_TIMEZONE %q rejected: %v", tz, err)
		}
	}
	c.Database.TimeZone = "India/Mumbai"
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "DB_TIMEZONE") {
		t.Fatalf("invalid DB_TIMEZONE accepted: %v", err)
	}
	if Load().Database.TimeZone != "Asia/Kolkata" {
		t.Fatal("default DB_TIMEZONE must be the business zone Asia/Kolkata")
	}
}

func TestValidate_ClientVersions(t *testing.T) {
	c := Config{App: AppConfig{Env: "local"}}
	c.Client = ClientAppConfig{MinVersion: "1.2.0", LatestVersion: "1.3.1"}
	if err := c.Validate(); err != nil {
		t.Fatalf("valid versions rejected: %v", err)
	}
	for _, bad := range []ClientAppConfig{
		{MinVersion: "1.x", LatestVersion: "1.3.1"},
		{MinVersion: "1.2.0", LatestVersion: "latest"},
		{MinVersion: "2.0.0", LatestVersion: "1.9.9"},
	} {
		c.Client = bad
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "CLIENT_") {
			t.Errorf("%+v accepted: %v", bad, err)
		}
	}
}

func TestDatabaseEncrypted(t *testing.T) {
	ok := []string{
		"postgres://app@db.internal/greenroot?sslmode=require",
		"postgres://app@db.internal/greenroot?sslmode=verify-full",
		"postgres:///greenroot?host=/var/run/postgresql", // Unix socket
		"postgres://app@localhost/greenroot?sslmode=disable",
		"host=db.internal dbname=greenroot sslmode=require",
	}
	for _, dsn := range ok {
		if !databaseEncrypted(dsn) {
			t.Errorf("%s: want encrypted/local", dsn)
		}
	}
	bad := []string{
		"postgres://app@db.internal/greenroot",
		"postgres://app@db.internal/greenroot?sslmode=allow",
		"host=db.internal dbname=greenroot sslmode=disable",
	}
	for _, dsn := range bad {
		if databaseEncrypted(dsn) {
			t.Errorf("%s: want rejected", dsn)
		}
	}
}
