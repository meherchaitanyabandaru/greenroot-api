package redisutil

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrLockBusy = errors.New("redis lock busy")

const LockTTL = 10 * time.Second

type Lock struct {
	client redis.Cmdable
	key    string
	log    *slog.Logger
}

func LockKey(module string, id int64) string {
	return fmt.Sprintf("%s%s:%d", KeyLock, module, id)
}

func AcquireLock(ctx context.Context, client redis.Cmdable, log *slog.Logger, module string, id int64) (*Lock, error) {
	if client == nil {
		return &Lock{}, nil
	}
	key := LockKey(module, id)
	ok, err := client.SetNX(ctx, key, "1", LockTTL).Result()
	if err != nil {
		logger(log).Warn("redis lock acquisition failed; continuing without distributed lock", "key", key, "error", err)
		return &Lock{}, nil
	}
	if !ok {
		return nil, ErrLockBusy
	}
	return &Lock{client: client, key: key, log: log}, nil
}

func (l *Lock) Release(ctx context.Context) {
	if l == nil || l.client == nil || l.key == "" {
		return
	}
	if err := l.client.Del(ctx, l.key).Err(); err != nil {
		logger(l.log).Warn("redis lock release failed", "key", l.key, "error", err)
	}
}

func logger(log *slog.Logger) *slog.Logger {
	if log != nil {
		return log
	}
	return slog.Default()
}
