package redisutil

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"
)

// ReconcileSuspensions seeds Redis from the current PostgreSQL suspended state.
// Must be called once at startup — idempotent, safe to run against a populated Redis.
// PostgreSQL is the source of truth; this only brings Redis in sync.
// Returns immediately when rdb is nil.
func ReconcileSuspensions(ctx context.Context, db *sql.DB, rdb redis.Cmdable, log *slog.Logger) {
	if rdb == nil {
		return
	}
	l := logger(log)

	userIDs, err := querySuspendedIDs(ctx, db,
		"SELECT user_id FROM public.users WHERE status='SUSPENDED' AND deleted_at IS NULL")
	if err != nil {
		l.Error("reconcile: failed to query suspended users", "error", err)
		return
	}

	nurseryIDs, err := querySuspendedIDs(ctx, db,
		"SELECT nursery_id FROM public.nurseries WHERE status='SUSPENDED'")
	if err != nil {
		l.Error("reconcile: failed to query suspended nurseries", "error", err)
		return
	}

	seedSuspensions(ctx, rdb, log, userIDs, nurseryIDs)
	l.Info("suspension reconciliation complete", "users", len(userIDs), "nurseries", len(nurseryIDs))
}

// seedSuspensions writes Redis suspension flags for the given IDs using SetNX
// (set-if-not-exists), so keys already present are left untouched.
// Exported only to the package (lowercase) — called by ReconcileSuspensions and tests.
func seedSuspensions(ctx context.Context, rdb redis.Cmdable, log *slog.Logger, userIDs, nurseryIDs []int64) {
	if rdb == nil {
		return
	}
	l := logger(log)
	for _, id := range userIDs {
		key := fmt.Sprintf("%s%d", KeySuspendedUser, id)
		if err := rdb.SetNX(ctx, key, "1", 0).Err(); err != nil {
			l.Warn("reconcile: user suspension key write failed", "user_id", id, "error", err)
		}
	}
	for _, id := range nurseryIDs {
		key := fmt.Sprintf("%s%d", KeySuspendedNursery, id)
		if err := rdb.SetNX(ctx, key, "1", 0).Err(); err != nil {
			l.Warn("reconcile: nursery suspension key write failed", "nursery_id", id, "error", err)
		}
	}
}

func querySuspendedIDs(ctx context.Context, db *sql.DB, query string) ([]int64, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
