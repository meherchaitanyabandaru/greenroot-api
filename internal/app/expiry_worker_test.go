package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func expiryTestDeps(t *testing.T) (Dependencies, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return Dependencies{Redis: rdb, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, rdb
}

func TestProcessDueExpirySet_ExpiresOnlyDueIDs(t *testing.T) {
	deps, rdb := expiryTestDeps(t)
	ctx := context.Background()
	const key = "test:expiry"
	now := time.Now().Unix()

	// 250 due ids (more than one batch of 100), one future id, one junk id.
	for i := 1; i <= 250; i++ {
		rdb.ZAdd(ctx, key, redis.Z{Score: float64(now - int64(i)), Member: strconv.Itoa(i)})
	}
	rdb.ZAdd(ctx, key, redis.Z{Score: float64(now + 3600), Member: "999"})
	rdb.ZAdd(ctx, key, redis.Z{Score: float64(now - 1), Member: "not-a-number"})

	expired := map[int64]bool{}
	err := processDueExpirySet(ctx, deps, key, func(_ context.Context, _ Dependencies, id int64) error {
		expired[id] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(expired) != 250 || expired[999] {
		t.Fatalf("expired %d ids (999 expired: %v), want 250 due ids only", len(expired), expired[999])
	}
	left, _ := rdb.ZRange(ctx, key, 0, -1).Result()
	if len(left) != 1 || left[0] != "999" {
		t.Fatalf("remaining = %v, want only the future id", left)
	}
}

func TestProcessDueExpirySet_FailingUpdateDoesNotLoopForever(t *testing.T) {
	deps, rdb := expiryTestDeps(t)
	ctx := context.Background()
	const key = "test:expiry"
	rdb.ZAdd(ctx, key, redis.Z{Score: 1, Member: "1"}, redis.Z{Score: 2, Member: "2"})

	calls := 0
	done := make(chan error, 1)
	go func() {
		done <- processDueExpirySet(ctx, deps, key, func(context.Context, Dependencies, int64) error {
			calls++
			return errors.New("database down")
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("processDueExpirySet did not return when every update failed")
	}
	if calls != 2 {
		t.Fatalf("expire called %d times, want 2 (one pass)", calls)
	}
	// Failed ids stay for the next tick.
	if n, _ := rdb.ZCard(ctx, key).Result(); n != 2 {
		t.Fatalf("remaining = %d, want 2", n)
	}
}
