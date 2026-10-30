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

// errLockUnavailable marks a busy answer caused by Redis being down (still ErrLockBusy for callers).
var errLockUnavailable = errors.New("redis unavailable")

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
		return nil, fmt.Errorf("%w: %w: %w", ErrLockBusy, errLockUnavailable, err)
	}
	if !ok {
		return nil, ErrLockBusy
	}
	return &Lock{client: client, key: key, token: token, log: log}, nil
}

// AcquireLockWait is AcquireLock that waits up to wait for a busy lock, for
// requests whose loser should get the real answer (e.g. "trip taken") rather
// than "busy, try again". Redis errors are not retried.
func AcquireLockWait(ctx context.Context, client redis.Cmdable, log *slog.Logger, module string, id int64, wait time.Duration) (*Lock, error) {
	deadline := time.Now().Add(wait)
	for {
		lock, err := AcquireLock(ctx, client, log, module, id)
		if !errors.Is(err, ErrLockBusy) || errors.Is(err, errLockUnavailable) || !time.Now().Before(deadline) {
			return lock, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
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
