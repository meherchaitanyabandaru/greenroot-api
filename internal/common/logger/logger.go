package logger

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/config"
)

type Manager struct {
	cfg       config.LoggingConfig
	log       *slog.Logger
	appFile   *os.File
	errorFile *os.File
}

func NewManager(cfg config.LoggingConfig) (*Manager, error) {
	logDir := datedLogDir(cfg.Dir, time.Now())
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, err
	}

	appFile, err := os.OpenFile(filepath.Join(logDir, "app.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}

	errorFile, err := os.OpenFile(filepath.Join(logDir, "errors.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		_ = appFile.Close()
		return nil, err
	}

	appHandler := slog.NewJSONHandler(io.MultiWriter(os.Stdout, appFile), &slog.HandlerOptions{
		Level: parseLevel(cfg.Level),
	})
	errorHandler := slog.NewJSONHandler(errorFile, &slog.HandlerOptions{
		Level: slog.LevelError,
	})

	return &Manager{
		cfg:       cfg,
		log:       slog.New(splitHandler{app: appHandler, errors: errorHandler}),
		appFile:   appFile,
		errorFile: errorFile,
	}, nil
}

func (m *Manager) Logger() *slog.Logger {
	return m.log
}

func (m *Manager) CleanupOldLogs() error {
	if m.cfg.RetentionDays <= 0 {
		return nil
	}

	cutoff := time.Now().AddDate(0, 0, -m.cfg.RetentionDays)
	var cleanupErr error

	err := filepath.WalkDir(m.cfg.Dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.ModTime().Before(cutoff) {
			cleanupErr = errors.Join(cleanupErr, os.Remove(path))
		}
		return nil
	})

	return errors.Join(err, cleanupErr)
}

func (m *Manager) Close() error {
	return errors.Join(m.appFile.Close(), m.errorFile.Close())
}

func datedLogDir(base string, now time.Time) string {
	return filepath.Join(base, now.Format("2006-01"), now.Format("2006-01-02"))
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type splitHandler struct {
	app    slog.Handler
	errors slog.Handler
}

func (h splitHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.app.Enabled(ctx, level) || h.errors.Enabled(ctx, level)
}

func (h splitHandler) Handle(ctx context.Context, record slog.Record) error {
	err := h.app.Handle(ctx, record)
	if record.Level >= slog.LevelError {
		err = errors.Join(err, h.errors.Handle(ctx, record))
	}
	return err
}

func (h splitHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return splitHandler{app: h.app.WithAttrs(attrs), errors: h.errors.WithAttrs(attrs)}
}

func (h splitHandler) WithGroup(name string) slog.Handler {
	return splitHandler{app: h.app.WithGroup(name), errors: h.errors.WithGroup(name)}
}
