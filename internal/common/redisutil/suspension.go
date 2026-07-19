package redisutil

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"
)

// SuspendUser writes a permanent Redis flag for userID.
// Survives API restarts — cleared only by ClearUserSuspension.
func SuspendUser(ctx context.Context, rdb redis.Cmdable, log *slog.Logger, userID int64) {
	if rdb == nil {
		return
	}
	key := fmt.Sprintf("%s%d", KeySuspendedUser, userID)
	if err := rdb.Set(ctx, key, "1", 0).Err(); err != nil {
		logger(log).Warn("redis user suspension write failed", "user_id", userID, "error", err)
	}
}

// ClearUserSuspension removes the Redis suspension flag for userID.
func ClearUserSuspension(ctx context.Context, rdb redis.Cmdable, log *slog.Logger, userID int64) {
	if rdb == nil {
		return
	}
	key := fmt.Sprintf("%s%d", KeySuspendedUser, userID)
	if err := rdb.Del(ctx, key).Err(); err != nil {
		logger(log).Warn("redis user suspension clear failed", "user_id", userID, "error", err)
	}
}

// IsUserSuspended checks the Redis flag for userID.
// Returns false (allow) on Redis errors to avoid blocking users on Redis outage.
func IsUserSuspended(ctx context.Context, rdb redis.Cmdable, userID int64) bool {
	if rdb == nil {
		return false
	}
	key := fmt.Sprintf("%s%d", KeySuspendedUser, userID)
	n, err := rdb.Exists(ctx, key).Result()
	return err == nil && n > 0
}

// SuspendNursery writes a permanent Redis flag for nurseryID.
func SuspendNursery(ctx context.Context, rdb redis.Cmdable, log *slog.Logger, nurseryID int64) {
	if rdb == nil {
		return
	}
	key := fmt.Sprintf("%s%d", KeySuspendedNursery, nurseryID)
	if err := rdb.Set(ctx, key, "1", 0).Err(); err != nil {
		logger(log).Warn("redis nursery suspension write failed", "nursery_id", nurseryID, "error", err)
	}
}

// ClearNurserySuspension removes the Redis suspension flag for nurseryID.
func ClearNurserySuspension(ctx context.Context, rdb redis.Cmdable, log *slog.Logger, nurseryID int64) {
	if rdb == nil {
		return
	}
	key := fmt.Sprintf("%s%d", KeySuspendedNursery, nurseryID)
	if err := rdb.Del(ctx, key).Err(); err != nil {
		logger(log).Warn("redis nursery suspension clear failed", "nursery_id", nurseryID, "error", err)
	}
}

// IsNurserySuspended checks the Redis flag for nurseryID.
func IsNurserySuspended(ctx context.Context, rdb redis.Cmdable, nurseryID int64) bool {
	if rdb == nil {
		return false
	}
	key := fmt.Sprintf("%s%d", KeySuspendedNursery, nurseryID)
	n, err := rdb.Exists(ctx, key).Result()
	return err == nil && n > 0
}
