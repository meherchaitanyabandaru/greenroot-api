package redisutil

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var ErrLockBusy = errors.New("redis lock busy")

const LockTTL = 10 * time.Second

type Lock struct {
	client redis.Cmdable
	key    string
	token  string
	log    *slog.Logger
}

func LockKey(module string, id int64) string {
	return fmt.Sprintf("%s%s:%d", KeyLock, module, id)
}

func AcquireLock(ctx context.Context, client redis.Cmdable, log *slog.Logger, module string, id int64) (*Lock, error) {
	if client == nil {
		return &Lock{}, nil // no Redis configured (local dev): single process, no lock needed
	}
	key := LockKey(module, id)
	token := uuid.NewString()
	ok, err := client.SetNX(ctx, key, token, LockTTL).Result()
	if err != nil {
		// Fail closed: these locks guard money and delivery codes, and running
		// unlocked is exactly when parallel requests double-apply. The caller
		// answers "busy, try again".
		logger(log).Error("redis lock unavailable; refusing to run unlocked", "key", key, "error", err)
		return nil, fmt.Errorf("%w: redis unavailable: %w", ErrLockBusy, err)
	}
	if !ok {
		return nil, ErrLockBusy
	}
	return &Lock{client: client, key: key, token: token, log: log}, nil
}

// releaseScript deletes the lock only if it is still ours: after LockTTL
// another request may hold it, and a blind DEL would let a third one in.
var releaseScript = redis.NewScript(`if redis.call("GET", KEYS[1]) == ARGV[1] then return redis.call("DEL", KEYS[1]) end return 0`)

func (l *Lock) Release(ctx context.Context) {
	if l == nil || l.client == nil || l.key == "" {
		return
	}
	if err := releaseScript.Run(context.WithoutCancel(ctx), l.client, []string{l.key}, l.token).Err(); err != nil {
		logger(l.log).Warn("redis lock release failed", "key", l.key, "error", err)
	}
}

func logger(log *slog.Logger) *slog.Logger {
	if log != nil {
		return log
	}
	return slog.Default()
}
