package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/app"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/secrets"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Production settings come from AWS Secrets Manager (AWS_SECRETS_ID).
	loaded, err := secrets.Load(ctx)
	if err != nil {
		slog.Error("failed to load secrets", slog.Any("error", err))
		os.Exit(1)
	}
	if len(loaded) > 0 {
		slog.Info("settings loaded from AWS Secrets Manager", "secret", os.Getenv(secrets.EnvVar), "keys", loaded)
	}

	application, err := app.Bootstrap(ctx)
	if err != nil {
		slog.Error("failed to bootstrap api", slog.Any("error", err))
		os.Exit(1)
	}
	defer application.Close()

	if err := application.Run(ctx); err != nil {
		slog.Error("api server failed", slog.Any("error", err))
		os.Exit(1)
	}
}
