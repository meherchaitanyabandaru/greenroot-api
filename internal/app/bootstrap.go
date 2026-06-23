package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/config"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/logger"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/database"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
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

	deps := Dependencies{
		Config:     cfg,
		Logger:     logManager.Logger(),
		LogManager: logManager,
		DB:         db,
		JWT:        jwtplatform.NewService(cfg.JWT),
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
	return errors.Join(a.deps.DB.Close(), a.deps.LogManager.Close())
}
