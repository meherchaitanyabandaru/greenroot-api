package authctx

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
)

// RequireActiveSubscription checks the actor's subscription level and:
//   - SubActive  → proceeds normally
//   - SubGrace   → proceeds but sets X-Subscription-Warning header
//   - SubExpired → writes 402 and returns false
//   - SubNone    → writes 402 and returns false (nursery-owner/manager only)
//
// Buyers and drivers are never blocked (SubNone returns true for them).
// Call this at the top of any handler that creates/modifies business data.
func RequireActiveSubscription(w http.ResponseWriter, actor Actor) bool {
	// Non-nursery roles (buyer, driver, admin) are never subscription-gated.
	if actor.NurseryID == 0 || actor.HasRole("ADMIN", "SUPER_ADMIN") {
		return true
	}

	level := actor.SubLevel()
	switch level {
	case SubActive:
		return true

	case SubGrace:
		daysLeft := graceDaysLeft(actor.SubExpEpoch)
		w.Header().Set("X-Subscription-Warning",
			fmt.Sprintf("subscription_expired;grace_days_remaining=%d", daysLeft))
		return true

	case SubExpired:
		response.Error(w, http.StatusPaymentRequired, "subscription_expired",
			"your subscription has expired — renew to continue creating data")
		return false

	default: // SubNone
		response.Error(w, http.StatusPaymentRequired, "subscription_required",
			"an active subscription is required for this action")
		return false
	}
}

// RequireActiveNursery rejects requests when the actor's nursery is suspended.
// Call this in handlers that operate on nursery-owned resources.
func RequireActiveNursery(w http.ResponseWriter, actor Actor) bool {
	if actor.NurseryID == 0 {
		return true // not nursery-scoped
	}
	if strings.EqualFold(actor.NurseryStatus, "SUSPENDED") {
		response.Error(w, http.StatusForbidden, "nursery_suspended",
			"this nursery account is suspended — contact support")
		return false
	}
	return true
}

func graceDaysLeft(expEpoch int64) int {
	graceEnd := time.Unix(expEpoch+int64(gracePeriodDays*24*60*60), 0)
	d := time.Until(graceEnd)
	if d <= 0 {
		return 0
	}
	return int(d.Hours() / 24)
}
