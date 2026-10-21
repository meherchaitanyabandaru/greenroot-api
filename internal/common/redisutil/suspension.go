package redisutil

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

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

// Suspension describes why a user is blocked, so the client can say so.
type Suspension struct {
	Code        string    `json:"code"` // USER_SUSPENDED | NURSERY_SUSPENDED
	Reason      string    `json:"reason,omitempty"`
	SuspendedAt time.Time `json:"suspended_at"`
}

// SuspendUserFor is SuspendUser with the details the API returns to clients.
func SuspendUserFor(ctx context.Context, rdb redis.Cmdable, log *slog.Logger, userID int64, s Suspension) {
	if rdb == nil {
		return
	}
	data, _ := json.Marshal(s)
	key := fmt.Sprintf("%s%d", KeySuspendedUser, userID)
	if err := rdb.Set(ctx, key, data, 0).Err(); err != nil {
		logger(log).Warn("redis user suspension write failed", "user_id", userID, "error", err)
	}
}

// UserSuspension reads the flag written by SuspendUser/SuspendUserFor. ok is
// false when the user is not suspended (or Redis is unavailable).
func UserSuspension(ctx context.Context, rdb redis.Cmdable, userID int64) (Suspension, bool) {
	if rdb == nil {
		return Suspension{}, false
	}
	raw, err := rdb.Get(ctx, fmt.Sprintf("%s%d", KeySuspendedUser, userID)).Bytes()
	if err != nil {
		return Suspension{}, false
	}
	var s Suspension
	if json.Unmarshal(raw, &s) != nil || s.Code == "" {
		s = Suspension{Code: "USER_SUSPENDED"} // legacy "1" flag
	}
	return s, true
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

// MarkNurseryClaimsStale records that the subscription data baked into access
// tokens for nurseryID changed now. Tokens issued before this moment are
// refused with 401 so the client refreshes and gets current claims. The key
// outlives the longest access token (ttl) and then expires on its own.
func MarkNurseryClaimsStale(ctx context.Context, rdb redis.Cmdable, log *slog.Logger, nurseryID int64, ttl time.Duration) {
	if rdb == nil || nurseryID <= 0 {
		return
	}
	key := fmt.Sprintf("%s%d", KeyNurseryClaimsStale, nurseryID)
	if err := rdb.Set(ctx, key, time.Now().Unix(), ttl).Err(); err != nil {
		logger(log).Warn("redis claims-stale write failed", "nursery_id", nurseryID, "error", err)
	}
}

// NurseryClaimsStaleSince returns the Unix second set by MarkNurseryClaimsStale,
// or 0 when nothing changed recently (or Redis is unavailable).
func NurseryClaimsStaleSince(ctx context.Context, rdb redis.Cmdable, nurseryID int64) int64 {
	if rdb == nil || nurseryID <= 0 {
		return 0
	}
	v, err := rdb.Get(ctx, fmt.Sprintf("%s%d", KeyNurseryClaimsStale, nurseryID)).Int64()
	if err != nil {
		return 0
	}
	return v
}
