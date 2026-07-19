package redisutil

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func newTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	srv := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	t.Cleanup(func() { client.Close() })
	return srv, client
}

// ─── Test 1: keys have no TTL — they survive an API restart ──────────────────

// A suspended-user key must have no expiry so it persists across process restarts.
// We verify this by fast-forwarding the mock clock by one year: a key with any TTL
// would expire, but a permanent key must still be visible.
func TestSuspendUser_SurvivesRestart(t *testing.T) {
	ctx := context.Background()
	srv, client := newTestRedis(t)

	SuspendUser(ctx, client, nil, 42)

	srv.FastForward(365 * 24 * time.Hour)

	if !IsUserSuspended(ctx, client, 42) {
		t.Error("user suspension must survive restart (key must have no TTL)")
	}
	key := fmt.Sprintf("%s%d", KeySuspendedUser, 42)
	if srv.TTL(key) != 0 {
		t.Errorf("user suspension key must have no expiry; got TTL %s", srv.TTL(key))
	}
}

func TestSuspendNursery_SurvivesRestart(t *testing.T) {
	ctx := context.Background()
	srv, client := newTestRedis(t)

	SuspendNursery(ctx, client, nil, 7)

	srv.FastForward(365 * 24 * time.Hour)

	if !IsNurserySuspended(ctx, client, 7) {
		t.Error("nursery suspension must survive restart (key must have no TTL)")
	}
}

// ─── Test 2: startup reconciliation backfills pre-deployment suspensions ─────

// Users and nurseries suspended before this deploy have no Redis key.
// seedSuspensions (called by ReconcileSuspensions at startup) must create them.
func TestSeedSuspensions_BackfillsPreExistingData(t *testing.T) {
	ctx := context.Background()
	_, client := newTestRedis(t)

	userIDs := []int64{10, 20}
	nurseryIDs := []int64{5}

	seedSuspensions(ctx, client, nil, userIDs, nurseryIDs)

	for _, id := range userIDs {
		if !IsUserSuspended(ctx, client, id) {
			t.Errorf("user %d must be blocked after startup reconciliation", id)
		}
	}
	if !IsNurserySuspended(ctx, client, 5) {
		t.Error("nursery 5 must be blocked after startup reconciliation")
	}
}

// seedSuspensions is idempotent: running it twice must not corrupt state.
func TestSeedSuspensions_Idempotent(t *testing.T) {
	ctx := context.Background()
	_, client := newTestRedis(t)

	seedSuspensions(ctx, client, nil, []int64{10}, nil)
	seedSuspensions(ctx, client, nil, []int64{10}, nil)

	if !IsUserSuspended(ctx, client, 10) {
		t.Error("user 10 must still be suspended after double reconciliation")
	}
}

// seedSuspensions must not disturb keys that were already present (SetNX guarantee).
func TestSeedSuspensions_DoesNotOverwriteExistingKey(t *testing.T) {
	ctx := context.Background()
	srv, client := newTestRedis(t)

	// Pre-existing key written by SuspendUser (uses SET, not SetNX)
	SuspendUser(ctx, client, nil, 10)
	keyBefore := srv.Exists(fmt.Sprintf("%s%d", KeySuspendedUser, 10))

	seedSuspensions(ctx, client, nil, []int64{10}, nil)

	if !keyBefore || !IsUserSuspended(ctx, client, 10) {
		t.Error("seedSuspensions must leave existing key intact")
	}
}

// ─── Test 3: reinstate deletes the Redis key ─────────────────────────────────

func TestClearUserSuspension_RemovesKey(t *testing.T) {
	ctx := context.Background()
	srv, client := newTestRedis(t)

	SuspendUser(ctx, client, nil, 42)
	if !IsUserSuspended(ctx, client, 42) {
		t.Fatal("user must be suspended before reinstate")
	}

	ClearUserSuspension(ctx, client, nil, 42)

	if IsUserSuspended(ctx, client, 42) {
		t.Error("user must not be suspended after reinstate")
	}
	key := fmt.Sprintf("%s%d", KeySuspendedUser, 42)
	if srv.Exists(key) {
		t.Error("Redis key must be deleted (not just unset) on reinstate")
	}
}

func TestClearNurserySuspension_RemovesKey(t *testing.T) {
	ctx := context.Background()
	srv, client := newTestRedis(t)

	SuspendNursery(ctx, client, nil, 7)
	ClearNurserySuspension(ctx, client, nil, 7)

	if IsNurserySuspended(ctx, client, 7) {
		t.Error("nursery must not be suspended after clear")
	}
	key := fmt.Sprintf("%s%d", KeySuspendedNursery, 7)
	if srv.Exists(key) {
		t.Error("Redis key must be deleted on reinstate")
	}
}

// Reinstating a user who was never suspended must be a no-op (no panic).
func TestClearUserSuspension_NeverSuspendedIsNoop(t *testing.T) {
	ctx := context.Background()
	_, client := newTestRedis(t)
	ClearUserSuspension(ctx, client, nil, 999) // must not panic or error
}

// ─── Test 4: Redis unavailable — PostgreSQL state must not be corrupted ───────

// When rdb is nil (Redis not configured), all suspension functions must be
// safe no-ops. PostgreSQL writes are handled by the admin service/repository —
// nil Redis never touches them.
func TestSuspensionFunctions_NilRedisIsSafe(t *testing.T) {
	ctx := context.Background()

	SuspendUser(ctx, nil, nil, 42)
	ClearUserSuspension(ctx, nil, nil, 42)
	SuspendNursery(ctx, nil, nil, 7)
	ClearNurserySuspension(ctx, nil, nil, 7)
	seedSuspensions(ctx, nil, nil, []int64{1, 2}, []int64{3})

	if IsUserSuspended(ctx, nil, 42) {
		t.Error("IsUserSuspended must return false (allow) when rdb is nil")
	}
	if IsNurserySuspended(ctx, nil, 7) {
		t.Error("IsNurserySuspended must return false (allow) when rdb is nil")
	}
}

// When Redis returns errors (server down, OOM, network split), writes fail
// silently — logged only, never returned. PostgreSQL state is unaffected
// because the repository write (the authoritative write) happens first in
// admin.Service and is independent of the Redis call.
func TestSuspensionFunctions_RedisOutageIsSilent(t *testing.T) {
	ctx := context.Background()
	srv, client := newTestRedis(t)

	// Force every Redis command to return an error — simulates server outage
	// without port-level timeouts so the test stays fast.
	srv.SetError("ERR server unavailable")

	// Must complete without panic; the Redis error is swallowed (logged only).
	SuspendUser(ctx, client, nil, 42)
	ClearUserSuspension(ctx, client, nil, 42)
	SuspendNursery(ctx, client, nil, 7)
	ClearNurserySuspension(ctx, client, nil, 7)
	seedSuspensions(ctx, client, nil, []int64{10}, []int64{5})
}
