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
)
