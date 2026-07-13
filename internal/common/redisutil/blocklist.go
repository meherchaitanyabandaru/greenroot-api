package redisutil

import (
	"context"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

func IsBlocklisted(ctx context.Context, rdb redis.Cmdable, log *slog.Logger, jti string) bool {
	if rdb == nil || jti == "" {
		return false
	}
	ok, err := rdb.Exists(ctx, KeyBlocklist+jti).Result()
	if err != nil {
		logger(log).Warn("redis token blocklist check failed; allowing token", "jti", jti, "error", err)
		return false
	}
	return ok > 0
}

func BlocklistToken(ctx context.Context, rdb redis.Cmdable, log *slog.Logger, jti string, ttl time.Duration) {
	if rdb == nil || jti == "" || ttl <= 0 {
		return
	}
	if err := rdb.Set(ctx, KeyBlocklist+jti, "1", ttl).Err(); err != nil {
		logger(log).Warn("redis token blocklist write failed", "jti", jti, "ttl", ttl.String(), "error", err)
	}
}
