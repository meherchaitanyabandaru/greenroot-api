package subscriptions

import "testing"

func TestSubscriptionIsTerminal(t *testing.T) {
	terminal := []string{"CANCELLED", "EXPIRED"}
	active := []string{"ACTIVE", "PAUSED"}
	for _, s := range terminal {
		if !IsTerminal(s) {
			t.Errorf("IsTerminal(%s): want true", s)
		}
	}
	for _, s := range active {
		if IsTerminal(s) {
			t.Errorf("IsTerminal(%s): want false", s)
		}
	}
}

func TestSubscriptionBuildCapabilities(t *testing.T) {
	owner := ActorContext{UserID: 10, Roles: []string{"NURSERY_OWNER"}}
	admin := ActorContext{UserID: 99, Roles: []string{"ADMIN"}}
	stranger := ActorContext{UserID: 55, Roles: []string{"BUYER"}}

	activeSub := UserSubscription{UserID: 10, Status: "ACTIVE"}
	pausedSub := UserSubscription{UserID: 10, Status: "PAUSED"}
	cancelledSub := UserSubscription{UserID: 10, Status: "CANCELLED"}

	t.Run("owner_active", func(t *testing.T) {
		caps := BuildCapabilities(owner, activeSub)
		if !caps.CanRenew || !caps.CanCancel || !caps.CanChangePlan {
			t.Error("owner should have renew/cancel/change-plan on active sub")
		}
		if caps.CanPause {
			t.Error("non-admin should not CanPause")
		}
	})

	t.Run("admin_active", func(t *testing.T) {
		caps := BuildCapabilities(admin, activeSub)
		if !caps.CanPause {
			t.Error("admin should CanPause active sub")
		}
		if caps.CanResume {
			t.Error("admin should not CanResume at ACTIVE")
		}
	})

	t.Run("admin_paused", func(t *testing.T) {
		caps := BuildCapabilities(admin, pausedSub)
		if !caps.CanResume {
			t.Error("admin should CanResume paused sub")
		}
		if caps.CanPause {
			t.Error("admin should not CanPause at PAUSED")
		}
	})

	t.Run("owner_cancelled", func(t *testing.T) {
		caps := BuildCapabilities(owner, cancelledSub)
		if caps.CanCancel {
			t.Error("owner should not CanCancel cancelled sub")
		}
	})

	t.Run("stranger_no_caps", func(t *testing.T) {
		caps := BuildCapabilities(stranger, activeSub)
		if caps.CanRenew || caps.CanCancel || caps.CanChangePlan {
			t.Error("stranger should have no caps on another user's sub")
		}
	})

	t.Run("retry_payment_on_failed", func(t *testing.T) {
		failedPayment := &Payment{Status: "FAILED"}
		sub := UserSubscription{UserID: 10, Status: "ACTIVE", LatestPayment: failedPayment}
		caps := BuildCapabilities(owner, sub)
		if !caps.CanRetryPayment {
			t.Error("owner should CanRetryPayment when latest payment is FAILED")
		}
	})
}
