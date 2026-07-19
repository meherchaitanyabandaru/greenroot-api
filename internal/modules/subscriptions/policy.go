package subscriptions

import "strings"

// policy.go — single source of truth for subscription business rules.
// All functions are pure, stateless, and take no database parameters.

// IsTerminal returns true when the subscription has reached a final state.
func IsTerminal(status string) bool {
	return status == statusCancelled || status == statusExpired
}

// BuildCapabilities computes the capability set for the given actor and subscription.
// No database access — struct fields and actor roles are the only inputs.
func BuildCapabilities(actor ActorContext, sub UserSubscription) SubscriptionCapabilities {
	status := strings.ToUpper(strings.TrimSpace(sub.Status))
	isAdmin := actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN")
	isOwner := sub.UserID == actor.UserID

	canRetryPayment := false
	if sub.LatestPayment != nil {
		ps := strings.ToUpper(strings.TrimSpace(sub.LatestPayment.Status))
		canRetryPayment = ps == "FAILED" || ps == "PENDING"
	}

	return SubscriptionCapabilities{
		CanRenew:        isOwner || isAdmin,
		CanCancel:       (isOwner || isAdmin) && (status == statusActive || status == statusPaused),
		CanPause:        isAdmin && status == statusActive,
		CanResume:       isAdmin && status == statusPaused,
		CanChangePlan:   isOwner || isAdmin,
		CanRetryPayment: (isOwner || isAdmin) && canRetryPayment,
	}
}
