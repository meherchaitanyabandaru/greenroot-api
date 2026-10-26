package redisutil

const (
	KeyLock               = "lock:"
	KeyOTP                = "otp:"
	KeyBlocklist          = "blocklist:"
	KeyWorkspace          = "workspace:"
	KeySubscriptionPlans  = "cache:subscription_plans:v2" // v2: monthly pricing; never read v1 lists
	KeyAdViews            = "ad:views:"
	KeyAdSaves            = "ad:saves:"
	KeyNotifications      = "notifications"
	KeyNotificationsDLQ   = "notifications:dead"
	KeyNotificationRetry  = "notifications:retry:"
	KeyQuotationExpiry    = "expiry:quotations"
	KeySubscriptionExpiry = "expiry:subscriptions"
	KeySuspendedUser      = "suspension:user:"
	KeySuspendedNursery   = "suspension:nursery:"
	// Unix second after which access tokens for a nursery carry stale
	// subscription claims and must be refreshed (see MarkNurseryClaimsStale).
	KeyNurseryClaimsStale = "claims:stale:nursery:"

	// Global Search cache-aside layer (see searchcache.go). PostgreSQL stays
	// authoritative; these are performance-only and safe to flush entirely.
	KeySearchCache      = "cache:search:"     // + {module}:u{userID}:g{gen}:{queryHash}
	KeySearchGeneration = "cache:search:gen:" // + {module}:u{userID} -- INCR'd to invalidate
	KeyRecentSearches   = "search:recent:"    // + u{userID} -- Redis LIST, source of truth (no PG table)
	KeySearchTermFreq   = "search:freq:"      // + {module} -- Redis ZSET, source of truth
)
