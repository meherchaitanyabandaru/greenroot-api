package orders

import "testing"

func TestOrderIsTerminal(t *testing.T) {
	for _, tc := range []struct {
		status string
		want   bool
	}{
		{"COMPLETED", true},
		{"CANCELLED", true},
		{"PENDING", false},
		{"CONFIRMED", false},
		{"LOADING", false},
		{"LOADED", false},
		{"PARTIALLY_FULFILLED", false},
	} {
		if got := IsTerminal(tc.status); got != tc.want {
			t.Errorf("IsTerminal(%q) = %v, want %v", tc.status, got, tc.want)
		}
	}
}

func TestOrderIsEditable(t *testing.T) {
	editable := []string{"PENDING", "CONFIRMED", "LOADING"}
	locked := []string{"LOADED", "PARTIALLY_FULFILLED", "COMPLETED", "CANCELLED"}
	for _, s := range editable {
		if !IsEditable(s) {
			t.Errorf("IsEditable(%s): want true", s)
		}
	}
	for _, s := range locked {
		if IsEditable(s) {
			t.Errorf("IsEditable(%s): want false", s)
		}
	}
}

func TestOrderCanTransition(t *testing.T) {
	allowed := [][2]string{
		{"PENDING", "CONFIRMED"},
		{"DRAFT", "CONFIRMED"},
		{"LOADED", "COMPLETED"},
		{"PARTIALLY_FULFILLED", "COMPLETED"},
	}
	denied := [][2]string{
		{"PENDING", "LOADING"},
		{"CONFIRMED", "COMPLETED"},
		{"COMPLETED", "CANCELLED"},
		{"CANCELLED", "PENDING"},
	}
	for _, pair := range allowed {
		if !CanTransition(pair[0], pair[1]) {
			t.Errorf("CanTransition(%q→%q) should be allowed", pair[0], pair[1])
		}
	}
	for _, pair := range denied {
		if CanTransition(pair[0], pair[1]) {
			t.Errorf("CanTransition(%q→%q) should be denied", pair[0], pair[1])
		}
	}
}

func TestOrderBuildCapabilities(t *testing.T) {
	owner := ActorContext{UserID: 1, Roles: []string{"NURSERY_OWNER"}}
	mgr := ActorContext{UserID: 2, Roles: []string{"MANAGER"}}
	buyer := ActorContext{UserID: 3, Roles: []string{"BUYER"}}
	admin := ActorContext{UserID: 4, Roles: []string{"ADMIN"}}

	t.Run("owner_pending", func(t *testing.T) {
		caps := BuildCapabilities(owner, "PENDING")
		if !caps.CanEdit || !caps.CanConfirm || !caps.CanCancel || !caps.CanDelete || !caps.CanAddItems {
			t.Error("owner at PENDING should have full edit caps")
		}
		if caps.CanStartLoading || caps.CanCompleteLoading {
			t.Error("owner at PENDING should not have loading caps")
		}
	})

	t.Run("owner_confirmed", func(t *testing.T) {
		caps := BuildCapabilities(owner, "CONFIRMED")
		if !caps.CanStartLoading {
			t.Error("owner should CanStartLoading at CONFIRMED")
		}
		if caps.CanDelete {
			t.Error("owner should not CanDelete at CONFIRMED")
		}
	})

	t.Run("owner_loading", func(t *testing.T) {
		caps := BuildCapabilities(owner, "LOADING")
		if !caps.CanCompleteLoading {
			t.Error("owner should CanCompleteLoading at LOADING")
		}
	})

	t.Run("owner_completed_terminal", func(t *testing.T) {
		caps := BuildCapabilities(owner, "COMPLETED")
		if caps.CanEdit || caps.CanConfirm || caps.CanCancel || caps.CanDelete {
			t.Error("no action caps at terminal COMPLETED")
		}
	})

	t.Run("manager_pending", func(t *testing.T) {
		caps := BuildCapabilities(mgr, "PENDING")
		if !caps.CanConfirm {
			t.Error("manager should CanConfirm at PENDING")
		}
	})

	t.Run("buyer_pending", func(t *testing.T) {
		caps := BuildCapabilities(buyer, "PENDING")
		if !caps.CanCancel {
			t.Error("buyer should CanCancel own PENDING order")
		}
		if caps.CanEdit || caps.CanConfirm || caps.CanDelete {
			t.Error("buyer should not have manage caps")
		}
	})

	t.Run("buyer_confirmed", func(t *testing.T) {
		caps := BuildCapabilities(buyer, "CONFIRMED")
		if caps.CanCancel {
			t.Error("buyer should not CanCancel at CONFIRMED")
		}
	})

	t.Run("admin_loaded", func(t *testing.T) {
		caps := BuildCapabilities(admin, "LOADED")
		if caps.CanCancel {
			t.Error("admin should not CanCancel at LOADED")
		}
	})
}
