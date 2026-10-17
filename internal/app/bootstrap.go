package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/config"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/logger"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/revocation"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/database"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/database/migrate"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/database/migrations"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/market"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/notifications"
	platformemail "github.com/meherchaitanyabandaru/greenroot-api/platform/email"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	platformsms "github.com/meherchaitanyabandaru/greenroot-api/platform/sms"
	"github.com/meherchaitanyabandaru/greenroot-api/platform/storage"
	"github.com/redis/go-redis/v9"
)

type App struct {
	deps   Dependencies
	server *http.Server
}

func Bootstrap(ctx context.Context) (*App, error) {
	cfg := config.Load()
	// Refuse to start with settings that are only safe for local development.
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	logManager, err := logger.NewManager(cfg.Logging)
	if err != nil {
		return nil, err
	}

	if err := logManager.CleanupOldLogs(); err != nil {
		logManager.Logger().Warn("log cleanup failed", "error", err)
	}

	db, err := database.Open(ctx, cfg.Database)
	if err != nil {
		_ = logManager.Close()
		return nil, err
	}

	// The schema must match the code: pending migrations refuse a production
	// start and warn elsewhere. Apply them with `go run ./cmd/migrate up`.
	if cfg.Database.URL != "" {
		all, err := migrate.Load(migrations.FS)
		if err != nil {
			_ = logManager.Close()
			_ = db.Close()
			return nil, err
		}
		if pending, err := migrate.New(db, all).CheckCurrent(ctx); err != nil {
			if !errors.Is(err, migrate.ErrPending) || cfg.IsProduction() {
				_ = logManager.Close()
				_ = db.Close()
				return nil, fmt.Errorf("%w: %s (run cmd/migrate up)", err, versions(pending))
			}
			logManager.Logger().Warn("database has pending migrations; run `go run ./cmd/migrate up`", "pending", versions(pending))
		}

		// The API must connect as greenroot_app (row access only), never as
		// the schema owner or a superuser. See greenroot-infra roles.sql.
		problems, err := database.PrivilegeProblems(ctx, db)
		if err != nil {
			_ = logManager.Close()
			_ = db.Close()
			return nil, fmt.Errorf("check database privileges: %w", err)
		}
		if len(problems) > 0 {
			if cfg.IsProduction() {
				_ = logManager.Close()
				_ = db.Close()
				return nil, fmt.Errorf("DATABASE_URL role is over-privileged (use greenroot_app): %s", strings.Join(problems, "; "))
			}
			logManager.Logger().Warn("DATABASE_URL role is over-privileged; production will refuse to start", "problems", problems)
		}
	}

	storageClient, err := storage.New(storage.Config{
		Endpoint:        cfg.Storage.Endpoint,
		AccessKeyID:     cfg.Storage.AccessKeyID,
		SecretAccessKey: cfg.Storage.SecretAccessKey,
		UseSSL:          cfg.Storage.UseSSL,
		Region:          cfg.Storage.Region,
		PublicURL:       cfg.Storage.PublicURL,
	})
	if err != nil {
		_ = logManager.Close()
		_ = db.Close()
		return nil, err
	}

	smsProvider, err := platformsms.New(cfg.SMS.Provider)
	if err != nil {
		_ = logManager.Close()
		_ = db.Close()
		return nil, err
	}
	if cfg.IsProduction() && !smsProvider.Live() {
		logManager.Logger().Warn("ALLOW_MOCK_PROVIDERS: SMS is mock in production; OTP is the fixed dev code 123456 for every account")
	}
	emailProvider, err := platformemail.New(cfg.Email.Provider)
	if err != nil {
		_ = logManager.Close()
		_ = db.Close()
		return nil, err
	}
	if cfg.IsProduction() && !emailProvider.Live() {
		logManager.Logger().Warn("ALLOW_MOCK_PROVIDERS: email is mock in production; nothing is delivered")
	}

	deps := Dependencies{
		Config:     cfg,
		Logger:     logManager.Logger(),
		LogManager: logManager,
		DB:         db,
		JWT:        jwtplatform.NewService(cfg.JWT),
		Storage:    storageClient,
		Audit:      auditlog.NewService(db, logManager.Logger()),
		Redis:      connectRedis(ctx, cfg.Redis, logManager.Logger()),
		SMS:        smsProvider,
		Email:      emailProvider,
	}

	// Sync suspension state into Redis before accepting any traffic.
	// Closes the gap where users suspended before this deploy have no Redis key.
	redisutil.ReconcileSuspensions(ctx, deps.DB, deps.Redis, deps.Logger)

	server := &http.Server{
		Addr:              cfg.HTTP.Addr(),
		Handler:           NewRouter(deps),
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
	}

	return &App{deps: deps, server: server}, nil
}

func (a *App) Run(ctx context.Context) error {
	// Background: expire subscriptions at midnight + clean revocation map every 15 min.
	go runCronJobs(ctx, a.deps)
	if a.deps.Redis != nil {
		go market.StartCounterFlusher(ctx, a.deps.DB, a.deps.Redis, a.deps.Logger, 10*time.Second)
		go notifications.StartQueueWorker(ctx, a.deps.DB, a.deps.Redis, notifications.MockSender{}, a.deps.Logger)
	}
	go runRedisExpiryJobs(ctx, a.deps)

	serverErrors := make(chan error, 1)
	go func() {
		a.deps.Logger.Info("api server starting", "addr", a.server.Addr, "env", a.deps.Config.App.Env)
		serverErrors <- a.server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return a.server.Shutdown(shutdownCtx)
	}
}

func (a *App) Close() error {
	// Drain pending audit writes before closing the DB connection.
	a.deps.Audit.Close()
	if a.deps.Redis != nil {
		_ = a.deps.Redis.Close()
	}
	return errors.Join(a.deps.DB.Close(), a.deps.LogManager.Close())
}

// connectRedis creates a Redis client when REDIS_ADDR is set.
// Returns nil (graceful no-op) when the address is empty.
func connectRedis(ctx context.Context, cfg config.RedisConfig, log *slog.Logger) *redis.Client {
	if cfg.Addr == "" {
		return nil
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Warn("redis not reachable — rate limiting disabled", "addr", cfg.Addr, "error", err)
		_ = rdb.Close()
		return nil
	}
	log.Info("redis connected", "addr", cfg.Addr)
	return rdb
}

// runCronJobs runs periodic maintenance tasks:
//   - every 15 min: clean up expired revocation entries (tiny in-memory map)
//   - daily at 00:05: mark subscriptions whose end_date has passed as EXPIRED
func runCronJobs(ctx context.Context, deps Dependencies) {
	cleanupTicker := time.NewTicker(15 * time.Minute)
	defer cleanupTicker.Stop()

	// Fire expiry job once at startup (catches any missed midnight), then daily.
	expireSubscriptions(ctx, deps)
	expireTicker := time.NewTicker(24 * time.Hour)
	defer expireTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-cleanupTicker.C:
			revocation.Cleanup()
		case <-expireTicker.C:
			expireSubscriptions(ctx, deps)
		}
	}
}

func expireSubscriptions(ctx context.Context, deps Dependencies) {
	const q = `
		UPDATE public.user_subscriptions
		SET subscription_status = 'EXPIRED', updated_at = CURRENT_TIMESTAMP
		WHERE subscription_status IN ('ACTIVE', 'TRIAL')
		  AND end_date IS NOT NULL
		  AND end_date < CURRENT_DATE
	`
	res, err := deps.DB.ExecContext(ctx, q)
	if err != nil {
		slog.Error("subscription expiry job failed", "error", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		slog.Info("subscription expiry job: marked expired", "count", n)
	}
}

func versions(ms []migrate.Migration) string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Version
	}
	return strings.Join(out, ", ")
}
