package logger

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/config"
)

func TestManagerWritesAppAndErrorLogs(t *testing.T) {
	dir := t.TempDir()

	manager, err := NewManager(config.LoggingConfig{
		Dir:           dir,
		RetentionDays: 90,
		Level:         "debug",
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	manager.Logger().Info("hello")
	manager.Logger().Error("boom", slog.String("code", "test_error"))

	if err := manager.Close(); err != nil {
		t.Fatalf("close logger: %v", err)
	}

	appLog := readOnlyLogFile(t, dir, "app.log")
	errorLog := readOnlyLogFile(t, dir, "errors.log")

	if !strings.Contains(appLog, "hello") || !strings.Contains(appLog, "boom") {
		t.Fatalf("expected app.log to contain info and error entries, got %q", appLog)
	}
	if !strings.Contains(errorLog, "boom") || !strings.Contains(errorLog, "test_error") {
		t.Fatalf("expected errors.log to contain error entry, got %q", errorLog)
	}
}

func readOnlyLogFile(t *testing.T, dir string, name string) string {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(dir, "*", "*", name))
	if err != nil {
		t.Fatalf("glob log file: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one %s file, got %d", name, len(matches))
	}

	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	return string(data)
}
