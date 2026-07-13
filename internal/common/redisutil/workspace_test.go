package redisutil

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestInvalidateWorkspacesDeletesUserKeys(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()

	if err := client.Set(ctx, WorkspaceKey(1), "{}", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.Set(ctx, WorkspaceKey(2), "{}", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}

	InvalidateWorkspaces(ctx, client, nil, 1, 1, 2, 0)

	if server.Exists(WorkspaceKey(1)) || server.Exists(WorkspaceKey(2)) {
		t.Fatal("workspace cache keys should be deleted")
	}
}
