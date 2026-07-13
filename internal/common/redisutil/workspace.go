package redisutil

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"
)

const WorkspaceTTLSeconds = 300

func WorkspaceKey(userID int64) string {
	return fmt.Sprintf("%s%d", KeyWorkspace, userID)
}

func InvalidateWorkspaces(ctx context.Context, rdb redis.Cmdable, log *slog.Logger, userIDs ...int64) {
	if rdb == nil || len(userIDs) == 0 {
		return
	}
	keys := make([]string, 0, len(userIDs))
	seen := map[int64]bool{}
	for _, userID := range userIDs {
		if userID <= 0 || seen[userID] {
			continue
		}
		seen[userID] = true
		keys = append(keys, WorkspaceKey(userID))
	}
	if len(keys) == 0 {
		return
	}
	if err := rdb.Del(ctx, keys...).Err(); err != nil {
		logger(log).Warn("redis workspace cache invalidation failed", "keys", keys, "error", err)
	}
}
