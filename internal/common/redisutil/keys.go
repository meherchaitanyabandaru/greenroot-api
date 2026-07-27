package redisutil

const (
	KeyLock               = "lock:"
	KeyOTP                = "otp:"
	KeyBlocklist          = "blocklist:"
	KeyWorkspace          = "workspace:"
	KeySubscriptionPlans  = "cache:subscription_plans"
	KeyAdViews            = "ad:views:"
	KeyAdSaves            = "ad:saves:"
	KeyNotifications      = "notifications"
	KeyNotificationsDLQ   = "notifications:dead"
	KeyNotificationRetry  = "notifications:retry:"
	KeyQuotationExpiry    = "expiry:quotations"
	KeySubscriptionExpiry = "expiry:subscriptions"
	KeySuspendedUser      = "suspension:user:"
	KeySuspendedNursery   = "suspension:nursery:"

	// Global Search cache-aside layer (see searchcache.go). PostgreSQL stays
	// authoritative; these are performance-only and safe to flush entirely.
	KeySearchCache      = "cache:search:"     // + {module}:u{userID}:g{gen}:{queryHash}
	KeySearchGeneration = "cache:search:gen:" // + {module}:u{userID} -- INCR'd to invalidate
	KeyRecentSearches   = "search:recent:"    // + u{userID} -- Redis LIST, source of truth (no PG table)
	KeySearchTermFreq   = "search:freq:"      // + {module} -- Redis ZSET, source of truth
)
