package redisutil

// Global Search cache-aside layer.
//
// PostgreSQL remains the source of truth for every domain this touches
// (orders, quotations, plants, market ads). Redis here is a pure,
// transparent performance optimization: on a miss, the caller queries
// Postgres and stores the result; on a hit, Postgres is never touched.
// Nothing here is allowed to become authoritative -- if Redis is
// unavailable or misbehaves, every function in this file degrades to a
// cache miss (client == nil is always a no-op / "not cached").
//
// RBAC safety: cache keys are scoped by the *searching* user's ID, never
// shared across users. Two different users get two different cache
// entries even for the identical query string, so one user's search
// results can never leak into another's response. Search results that are
// actor-personalized (e.g. market ads' IsSavedByMe) are therefore always
// cached correctly.
//
// Invalidation: each module keeps a per-user "generation" counter (a
// plain Redis INCR). It's embedded in every cache key for that module+user,
// so bumping it instantly makes all previously-cached entries for that
// module+user unreachable (new lookups compute a new key; the stale ones
// simply expire via their own TTL, no explicit deletion needed). Mutations
// bump the generation for the acting user; other users who might also see
// the changed record (e.g. a buyer whose order an owner just confirmed)
// fall back to the short TTL on the volatile domains (orders/quotations)
// rather than a cross-actor invalidation fan-out -- a deliberate,
// documented tradeoff, not an oversight.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Recommended TTLs per the Global Search cache strategy. Callers pass these
// explicitly rather than this package guessing per module, so the choice
// stays visible at the call site.
const (
	SearchTTLPlants      = 30 * time.Minute
	SearchTTLMarketAds   = 2 * time.Minute
	SearchTTLOrders      = 20 * time.Second
	SearchTTLQuotations  = 20 * time.Second
	SearchTTLMembers     = 1 * time.Minute
	RecentSearchesTTL    = 30 * 24 * time.Hour
	SearchSuggestionsTTL = 10 * time.Minute
)

const maxRecentSearches = 10

// SearchCacheKey builds the cache-aside key for one module+user+generation+
// query. queryParts should include the search string plus every resolved
// filter that affects the result set (status, pagination, sort, nursery
// scope, ...) -- anything left out risks two different queries colliding
// on the same key and one masking the other's result.
func SearchCacheKey(module string, userID, generation int64, queryParts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(queryParts, "\x1f")))
	return fmt.Sprintf("%s%s:u%d:g%d:%x", KeySearchCache, module, userID, generation, sum[:8])
}

func searchGenerationKey(module string, userID int64) string {
	return fmt.Sprintf("%s%s:u%d", KeySearchGeneration, module, userID)
}

// SearchGeneration returns the current generation counter for module+user
// (0 if never bumped). Used to build the current cache key.
func SearchGeneration(ctx context.Context, client redis.Cmdable, log *slog.Logger, module string, userID int64) int64 {
	if client == nil {
		return 0
	}
	val, err := client.Get(ctx, searchGenerationKey(module, userID)).Int64()
	if errors.Is(err, redis.Nil) {
		return 0
	}
	if err != nil {
		logger(log).Warn("redis search generation read failed", "module", module, "error", err)
		return 0
	}
	return val
}

// BumpSearchGeneration invalidates every previously-cached search result
// for this module+user. Call at the mutation points listed in the Global
// Search spec (order/quotation/ad create-update-etc, member add/remove).
func BumpSearchGeneration(ctx context.Context, client redis.Cmdable, log *slog.Logger, module string, userID int64) {
	if client == nil {
		return
	}
	key := searchGenerationKey(module, userID)
	if err := client.Incr(ctx, key).Err(); err != nil {
		logger(log).Warn("redis search generation bump failed", "module", module, "error", err)
		return
	}
	logger(log).Info("search cache invalidate", "module", module, "user_id", userID)
}

// GetCachedSearch reports a cache hit/miss and logs it with latency, per the
// Global Search spec's observability requirement. On a hit it decodes JSON
// into T; any Redis or decode failure is treated as a miss (never surfaced
// to the caller as an error -- caching must never make a request fail).
func GetCachedSearch[T any](ctx context.Context, client redis.Cmdable, log *slog.Logger, module, query, key string) (T, bool) {
	var zero T
	if client == nil {
		return zero, false
	}
	start := time.Now()
	data, err := client.Get(ctx, key).Bytes()
	elapsed := time.Since(start)
	if errors.Is(err, redis.Nil) {
		logger(log).Info("search cache miss", "module", module, "query", query)
		return zero, false
	}
	if err != nil {
		logger(log).Warn("redis search cache read failed", "module", module, "key", key, "error", err)
		return zero, false
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		logger(log).Warn("redis search cache decode failed", "module", module, "key", key, "error", err)
		return zero, false
	}
	logger(log).Info("search cache hit", "module", module, "query", query, "redis_ms", elapsed.Milliseconds())
	return value, true
}

// SetCachedSearch stores value under key with the given TTL, logging the
// store per the observability spec. Best-effort: a write failure is logged
// and swallowed, never propagated (a cache-store failure must not fail the
// request that already has its Postgres-sourced answer in hand).
func SetCachedSearch[T any](ctx context.Context, client redis.Cmdable, log *slog.Logger, module, query, key string, value T, ttl time.Duration, pgElapsed time.Duration) {
	if client == nil {
		return
	}
	data, err := json.Marshal(value)
	if err != nil {
		logger(log).Warn("redis search cache encode failed", "module", module, "key", key, "error", err)
		return
	}
	if err := client.Set(ctx, key, data, ttl).Err(); err != nil {
		logger(log).Warn("redis search cache write failed", "module", module, "key", key, "error", err)
		return
	}
	logger(log).Info("search cache store", "module", module, "query", query,
		"postgres_ms", pgElapsed.Milliseconds(), "ttl_s", int(ttl.Seconds()), "cached", true)
}

// ── Recent searches (Redis-native; no Postgres counterpart) ──────────────

// RecordRecentSearch pushes query to the front of the user's recent-search
// list, de-duplicating and trimming to maxRecentSearches. This data has no
// Postgres source of truth -- it's inherently ephemeral per-device-session
// history, the same category of data as an OTP code, so living only in
// Redis (with a long 30-day TTL) is the correct home for it, not a gap in
// the "Postgres is authoritative" rule.
func RecordRecentSearch(ctx context.Context, client redis.Cmdable, log *slog.Logger, userID int64, query string) {
	if client == nil || strings.TrimSpace(query) == "" {
		return
	}
	key := fmt.Sprintf("%su%d", KeyRecentSearches, userID)
	normalized := strings.TrimSpace(query)
	pipe := client.TxPipeline()
	pipe.LRem(ctx, key, 0, normalized)
	pipe.LPush(ctx, key, normalized)
	pipe.LTrim(ctx, key, 0, maxRecentSearches-1)
	pipe.Expire(ctx, key, RecentSearchesTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		logger(log).Warn("redis recent-search record failed", "user_id", userID, "error", err)
	}
}

// RecentSearches returns the user's last searches, most recent first.
func RecentSearches(ctx context.Context, client redis.Cmdable, log *slog.Logger, userID int64) []string {
	if client == nil {
		return nil
	}
	key := fmt.Sprintf("%su%d", KeyRecentSearches, userID)
	items, err := client.LRange(ctx, key, 0, maxRecentSearches-1).Result()
	if err != nil {
		logger(log).Warn("redis recent-search read failed", "user_id", userID, "error", err)
		return nil
	}
	return items
}

// ClearRecentSearches removes a user's search history (called on logout).
func ClearRecentSearches(ctx context.Context, client redis.Cmdable, log *slog.Logger, userID int64) {
	if client == nil {
		return
	}
	key := fmt.Sprintf("%su%d", KeyRecentSearches, userID)
	if err := client.Del(ctx, key).Err(); err != nil {
		logger(log).Warn("redis recent-search clear failed", "user_id", userID, "error", err)
	}
}

// ── Search suggestions (Redis-native popularity tracking) ────────────────

// RecordSearchTerm bumps the normalized query's popularity score for
// module, so genuinely-popular terms (not a hardcoded list) surface as
// suggestions. Fire-and-forget from the caller's perspective.
func RecordSearchTerm(ctx context.Context, client redis.Cmdable, log *slog.Logger, module, query string) {
	normalized := strings.ToLower(strings.TrimSpace(query))
	if client == nil || normalized == "" {
		return
	}
	key := KeySearchTermFreq + module
	if err := client.ZIncrBy(ctx, key, 1, normalized).Err(); err != nil {
		logger(log).Warn("redis search term record failed", "module", module, "error", err)
	}
}

// SearchSuggestions returns the top-N most-searched terms for module,
// highest first. Cheap enough (a single ZREVRANGE) that this doesn't need
// its own extra cache layer on top -- the sorted set itself IS the cache,
// refreshed continuously by RecordSearchTerm.
func SearchSuggestions(ctx context.Context, client redis.Cmdable, log *slog.Logger, module string, limit int64) []string {
	if client == nil {
		return nil
	}
	key := KeySearchTermFreq + module
	items, err := client.ZRevRange(ctx, key, 0, limit-1).Result()
	if err != nil {
		logger(log).Warn("redis search suggestions read failed", "module", module, "error", err)
		return nil
	}
	return items
}
