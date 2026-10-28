package notifications

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
)

func TestQueueWorker_RecreatesGroupAfterRedisFlush(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	hasGroup := func() bool {
		groups, err := rdb.XInfoGroups(ctx, redisutil.KeyNotifications).Result()
		return err == nil && len(groups) == 1 && groups[0].Name == notificationWorkerGroup
	}
	waitFor := func(what string) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			if hasGroup() {
				return
			}
		}
		t.Fatalf("no consumer group %s", what)
	}

	// The queue is empty, so the database is never touched.
	go StartQueueWorker(ctx, &sql.DB{}, rdb, MockSender{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	waitFor("at start")

	mr.FlushAll()
	waitFor("after the flush")
}
