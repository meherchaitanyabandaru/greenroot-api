package app

import (
	"context"
	"strconv"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/redis/go-redis/v9"
)

func runRedisExpiryJobs(ctx context.Context, deps Dependencies) {
	if deps.Redis == nil {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	processRedisExpiries(ctx, deps)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			processRedisExpiries(ctx, deps)
		}
	}
}

func processRedisExpiries(ctx context.Context, deps Dependencies) {
	_ = processDueExpirySet(ctx, deps, redisutil.KeyQuotationExpiry, expireQuotation)
	_ = processDueExpirySet(ctx, deps, redisutil.KeySubscriptionExpiry, expireSubscription)
}

func processDueExpirySet(ctx context.Context, deps Dependencies, key string, expire func(context.Context, Dependencies, int64) error) error {
	now := strconv.FormatInt(time.Now().Unix(), 10)
	for {
		ids, err := deps.Redis.ZRangeByScore(ctx, key, &redis.ZRangeBy{
			Min:   "0",
			Max:   now,
			Count: 100,
		}).Result()
		if err != nil {
			deps.Logger.Warn("redis expiry read failed", "key", key, "error", err)
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		for _, rawID := range ids {
			id, err := strconv.ParseInt(rawID, 10, 64)
			if err != nil {
				deps.Logger.Warn("redis expiry invalid id", "key", key, "id", rawID, "error", err)
				_ = deps.Redis.ZRem(ctx, key, rawID).Err()
				continue
			}
			if err := expire(ctx, deps, id); err != nil {
				deps.Logger.Warn("redis expiry postgres update failed", "key", key, "id", id, "error", err)
				continue
			}
			if err := deps.Redis.ZRem(ctx, key, rawID).Err(); err != nil {
				deps.Logger.Warn("redis expiry remove failed", "key", key, "id", id, "error", err)
			}
		}
	}
}

func expireQuotation(ctx context.Context, deps Dependencies, id int64) error {
	result, err := deps.DB.ExecContext(ctx,
		`UPDATE public.quotations
		 SET status = 'EXPIRED', updated_at = CURRENT_TIMESTAMP
		 WHERE quotation_id = $1
		   AND converted_order_id IS NULL
		   AND status NOT IN ('EXPIRED', 'CUSTOMER_ACCEPTED', 'CUSTOMER_REJECTED', 'CONVERTED')`,
		id,
	)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows > 0 {
		deps.Logger.Info("quotation expired", "quotation_id", id)
	}
	return nil
}

func expireSubscription(ctx context.Context, deps Dependencies, id int64) error {
	result, err := deps.DB.ExecContext(ctx,
		`UPDATE public.user_subscriptions
		 SET subscription_status = 'EXPIRED', auto_renew = false, updated_at = CURRENT_TIMESTAMP
		 WHERE user_subscription_id = $1
		   AND subscription_status IN ('ACTIVE', 'TRIAL')`,
		id,
	)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows > 0 {
		deps.Logger.Info("subscription expired", "subscription_id", id)
	}
	return nil
}
