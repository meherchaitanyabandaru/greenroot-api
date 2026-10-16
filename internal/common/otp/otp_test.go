package otp

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

var policy = Policy{Prefix: "t:", TTL: 5 * time.Minute, Cooldown: 30 * time.Second, MaxPerHour: 3, MaxAttempts: 3}

func store(t *testing.T) (*Store, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { client.Close() })
	return New(client, policy), server
}

func TestGenerate(t *testing.T) {
	re := regexp.MustCompile(`^[0-9]{6}$`)
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		code, err := Generate()
		if err != nil || !re.MatchString(code) {
			t.Fatalf("Generate() = %q, %v", code, err)
		}
		seen[code] = true
	}
	if len(seen) < 45 {
		t.Fatalf("codes are not random enough: %d unique of 50", len(seen))
	}
}

func TestSaveVerify_HashedSingleUse(t *testing.T) {
	ctx := context.Background()
	s, server := store(t)
	if err := s.Save(ctx, "u1", "482913"); err != nil {
		t.Fatal(err)
	}
	if v, _ := server.Get("t:u1"); v == "482913" {
		t.Fatal("code stored in clear text")
	}
	if !s.Verify(ctx, "u1", "482913", "") {
		t.Fatal("correct code rejected")
	}
	if s.Verify(ctx, "u1", "482913", "") {
		t.Fatal("code must be single-use")
	}
}

func TestVerify_BurnsAfterMaxAttempts(t *testing.T) {
	ctx := context.Background()
	s, _ := store(t)
	_ = s.Save(ctx, "u1", "482913")
	for i := int64(0); i < policy.MaxAttempts; i++ {
		if s.Verify(ctx, "u1", "000000", "") {
			t.Fatal("wrong code accepted")
		}
	}
	if s.Verify(ctx, "u1", "482913", "") {
		t.Fatal("code must be burned after max wrong attempts")
	}
}

func TestVerify_DevFallbackOnlyWhenAllowed(t *testing.T) {
	ctx := context.Background()
	s, _ := store(t)
	if !s.Verify(ctx, "nobody", "123456", "123456") {
		t.Fatal("dev code should work when no code is stored and fallback is allowed")
	}
	if s.Verify(ctx, "nobody", "123456", "") {
		t.Fatal("dev code must not work when fallback is disabled")
	}
	noRedis := New(nil, policy)
	if noRedis.Verify(ctx, "x", "123456", "") || !noRedis.Verify(ctx, "x", "123456", "123456") {
		t.Fatal("nil Redis must only accept the dev code when allowed")
	}
}

func TestThrottle(t *testing.T) {
	ctx := context.Background()
	s, server := store(t)
	if err := s.Throttle(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Throttle(ctx, "u1"); !errors.Is(err, ErrCooldown) {
		t.Fatalf("want cooldown, got %v", err)
	}
	for i := 2; i <= int(policy.MaxPerHour); i++ {
		server.FastForward(policy.Cooldown + time.Second)
		if err := s.Throttle(ctx, "u1"); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	server.FastForward(policy.Cooldown + time.Second)
	if err := s.Throttle(ctx, "u1"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("want rate limit, got %v", err)
	}
}
