package market

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/redis/go-redis/v9"
)

func StartCounterFlusher(ctx context.Context, db *sql.DB, rdb redis.Cmdable, log *slog.Logger, interval time.Duration) {
	if db == nil || rdb == nil {
		return
	}
	if log == nil {
		log = slog.Default()
	}
	repo := NewRepository(db)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			flushMarketAdCounters(context.Background(), repo, rdb, log)
			return
		case <-ticker.C:
			flushMarketAdCounters(context.Background(), repo, rdb, log)
		}
	}
}

func flushMarketAdCounters(ctx context.Context, repo Repository, rdb redis.Cmdable, log *slog.Logger) {
	flushCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	views, err := collectMarketAdCounters(flushCtx, rdb, redisutil.KeyAdViews, log)
	if err != nil {
		log.Warn("redis market ad view counter collection failed", "error", err)
		return
	}
	saves, err := collectMarketAdCounters(flushCtx, rdb, redisutil.KeyAdSaves, log)
	if err != nil {
		log.Warn("redis market ad save counter collection failed", "error", err)
		restoreMarketAdCounters(flushCtx, rdb, redisutil.KeyAdViews, views, log)
		return
	}
	if len(views) == 0 && len(saves) == 0 {
		return
	}
	if err := repo.FlushAdCounters(flushCtx, views, saves); err != nil {
		log.Warn("market ad counter flush failed", "error", err)
		restoreMarketAdCounters(flushCtx, rdb, redisutil.KeyAdViews, views, log)
		restoreMarketAdCounters(flushCtx, rdb, redisutil.KeyAdSaves, saves, log)
	}
}

func collectMarketAdCounters(ctx context.Context, rdb redis.Cmdable, prefix string, log *slog.Logger) (map[int64]int64, error) {
	counters := map[int64]int64{}
	var cursor uint64
	for {
		keys, nextCursor, err := rdb.Scan(ctx, cursor, prefix+"*", 100).Result()
		if err != nil {
			return counters, err
		}
		for _, key := range keys {
			raw, err := rdb.GetDel(ctx, key).Result()
			if errors.Is(err, redis.Nil) {
				continue
			}
			if err != nil {
				return counters, err
			}
			delta, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				log.Warn("invalid market ad counter value", "key", key, "value", raw, "error", err)
				continue
			}
			id, err := strconv.ParseInt(strings.TrimPrefix(key, prefix), 10, 64)
			if err != nil {
				log.Warn("invalid market ad counter key", "key", key, "error", err)
				continue
			}
			counters[id] += delta
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return counters, nil
}

func restoreMarketAdCounters(ctx context.Context, rdb redis.Cmdable, prefix string, counters map[int64]int64, log *slog.Logger) {
	for adID, delta := range counters {
		if delta == 0 {
			continue
		}
		key := prefix + strconv.FormatInt(adID, 10)
		if err := rdb.IncrBy(ctx, key, delta).Err(); err != nil {
			log.Warn("redis market ad counter restore failed", "key", key, "delta", delta, "error", err)
		}
	}
}
