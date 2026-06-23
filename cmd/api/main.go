package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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
