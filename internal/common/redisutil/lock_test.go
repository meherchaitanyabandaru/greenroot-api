package redisutil

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestAcquireLockLifecycle(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()

	first, err := AcquireLock(ctx, client, nil, "orders", 42)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if !server.Exists(LockKey("orders", 42)) {
		t.Fatal("lock key was not written")
	}

	_, err = AcquireLock(ctx, client, nil, "orders", 42)
	if !errors.Is(err, ErrLockBusy) {
		t.Fatalf("second acquire: got %v, want ErrLockBusy", err)
	}

	first.Release(ctx)
	if server.Exists(LockKey("orders", 42)) {
		t.Fatal("lock key was not deleted")
	}

	second, err := AcquireLock(ctx, client, nil, "orders", 42)
	if err != nil {
		t.Fatalf("reacquire after release: %v", err)
	}
	second.Release(ctx)
}

// An expired lock taken over by another request must survive the first
// holder's late Release.
func TestLockReleaseKeepsAnotherHoldersLock(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()

	stale, err := AcquireLock(ctx, client, nil, "orders", 7)
	if err != nil {
		t.Fatal(err)
	}
	server.FastForward(LockTTL + time.Second)
	current, err := AcquireLock(ctx, client, nil, "orders", 7)
	if err != nil {
		t.Fatalf("acquire after expiry: %v", err)
	}
	stale.Release(ctx)
	if !server.Exists(LockKey("orders", 7)) {
		t.Fatal("stale holder released the current holder's lock")
	}
	current.Release(ctx)
	if server.Exists(LockKey("orders", 7)) {
		t.Fatal("current holder could not release its lock")
	}
}

// Redis trouble must not let money and delivery-code operations run unlocked.
func TestAcquireLockFailsClosedWhenRedisIsDown(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	server.Close()
	if _, err := AcquireLock(context.Background(), client, nil, "subscriptions", 1); !errors.Is(err, ErrLockBusy) {
		t.Fatalf("got %v, want ErrLockBusy", err)
	}
}

func TestAcquireLockWait(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()

	first, err := AcquireLock(ctx, client, nil, "dispatches", 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLockWait(ctx, client, nil, "dispatches", 7, 120*time.Millisecond); !errors.Is(err, ErrLockBusy) {
		t.Fatalf("held past the wait: got %v, want ErrLockBusy", err)
	}
	go func() { time.Sleep(60 * time.Millisecond); first.Release(ctx) }()
	second, err := AcquireLockWait(ctx, client, nil, "dispatches", 7, time.Second)
	if err != nil {
		t.Fatalf("released while waiting: got %v", err)
	}
	second.Release(ctx)
}
