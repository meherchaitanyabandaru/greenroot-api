package redisutil

import (
	"context"
	"errors"
	"testing"

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
