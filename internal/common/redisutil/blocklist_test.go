package redisutil

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestBlocklistTokenLifecycle(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()

	const jti = "token-id"
	if IsBlocklisted(ctx, client, nil, jti) {
		t.Fatal("token should not start blocklisted")
	}

	BlocklistToken(ctx, client, nil, jti, time.Minute)
	if !IsBlocklisted(ctx, client, nil, jti) {
		t.Fatal("token should be blocklisted")
	}
	if ttl := server.TTL(KeyBlocklist + jti); ttl <= 0 {
		t.Fatalf("expected blocklist ttl, got %s", ttl)
	}
}
