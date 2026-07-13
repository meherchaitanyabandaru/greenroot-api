package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/config"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/logger"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/revocation"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/database"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/market"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	"github.com/meherchaitanyabandaru/greenroot-api/platform/storage"
	"github.com/redis/go-redis/v9"
)

type App struct {
	deps   Dependencies
	server *http.Server
}

func Bootstrap(ctx context.Context) (*App, error) {
	cfg := config.Load()

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

	deps := Dependencies{
		Config:     cfg,
		Logger:     logManager.Logger(),
		LogManager: logManager,
		DB:         db,
		JWT:        jwtplatform.NewService(cfg.JWT),
		Storage:    storageClient,
		Audit:      auditlog.NewService(db, logManager.Logger()),
		Redis:      connectRedis(ctx, cfg.Redis, logManager.Logger()),
	}

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
	go market.StartCounterFlusher(ctx, a.deps.DB, a.deps.Redis, a.deps.Logger, time.Minute)

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
