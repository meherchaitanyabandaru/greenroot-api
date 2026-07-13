package redisgeo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestService(t *testing.T) (*Service, func()) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return New(client, WithLastSeenTTL(120*time.Second)), func() {
		_ = client.Close()
		mr.Close()
	}
}

func TestUpsertAndGetDriver(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer cleanup()

	loc, err := svc.UpsertDriver(context.Background(), 42, 17.385, 78.4867)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if loc.DriverID != 42 {
		t.Fatalf("driver id: want 42, got %d", loc.DriverID)
	}

	got, err := svc.GetDriver(context.Background(), 42)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected live location, got nil")
	}
	if got.DriverID != 42 {
		t.Fatalf("driver id: want 42, got %d", got.DriverID)
	}
}

func TestUpsertUpdatesDriverLocation(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer cleanup()

	_, _ = svc.UpsertDriver(context.Background(), 42, 17.385, 78.4867)
	_, err := svc.UpsertDriver(context.Background(), 42, 17.45, 78.50)
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := svc.GetDriver(context.Background(), 42)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.Latitude < 17.44 || got.Latitude > 17.46 {
		t.Fatalf("expected updated latitude around 17.45, got %#v", got)
	}
}

func TestNearbyDrivers(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer cleanup()

	_, _ = svc.UpsertDriver(context.Background(), 42, 17.385, 78.4867)
	_, _ = svc.UpsertDriver(context.Background(), 43, 28.6139, 77.2090)

	drivers, err := svc.Nearby(context.Background(), 17.385, 78.4867, 5, 10)
	if err != nil {
		t.Fatalf("nearby: %v", err)
	}
	if len(drivers) != 1 || drivers[0].DriverID != 42 {
		t.Fatalf("expected only driver 42 nearby, got %#v", drivers)
	}
}

func TestRemoveDriver(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer cleanup()

	_, _ = svc.UpsertDriver(context.Background(), 42, 17.385, 78.4867)
	if err := svc.RemoveDriver(context.Background(), 42); err != nil {
		t.Fatalf("remove: %v", err)
	}
	got, err := svc.GetDriver(context.Background(), 42)
	if err != nil {
		t.Fatalf("get after remove: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil after remove, got %#v", got)
	}
}

func TestRedisUnavailable(t *testing.T) {
	svc := New(nil)
	_, err := svc.UpsertDriver(context.Background(), 42, 17.385, 78.4867)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}
