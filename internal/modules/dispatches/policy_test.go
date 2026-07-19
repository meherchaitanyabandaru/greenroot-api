package dispatches

import "testing"

func TestIsTerminal(t *testing.T) {
	for _, tc := range []struct {
		status string
		want   bool
	}{
		{"DELIVERED", true},
		{"CANCELLED", true},
		{"PENDING", false},
		{"ACCEPTED", false},
		{"DISPATCHED", false},
		{"IN_TRANSIT", false},
	} {
		if got := IsTerminal(tc.status); got != tc.want {
			t.Errorf("IsTerminal(%q) = %v, want %v", tc.status, got, tc.want)
		}
	}
}

func TestIsActive(t *testing.T) {
	for _, tc := range []struct {
		status string
		want   bool
	}{
		{"ACCEPTED", true},
		{"DISPATCHED", true},
		{"IN_TRANSIT", true},
		{"PENDING", false},
		{"DELIVERED", false},
		{"CANCELLED", false},
	} {
		if got := IsActive(tc.status); got != tc.want {
			t.Errorf("IsActive(%q) = %v, want %v", tc.status, got, tc.want)
		}
	}
}

func TestCanTransition(t *testing.T) {
	allowed := [][2]string{
		{"PENDING", "DISPATCHED"},
		{"PENDING", "CANCELLED"},
		{"ACCEPTED", "DISPATCHED"},
		{"ACCEPTED", "CANCELLED"},
		{"DISPATCHED", "IN_TRANSIT"},
		{"IN_TRANSIT", "DELIVERED"},
	}
	denied := [][2]string{
		{"PENDING", "ACCEPTED"}, // only via AcceptDispatch
		{"PENDING", "IN_TRANSIT"},
		{"DELIVERED", "CANCELLED"},
		{"CANCELLED", "PENDING"},
		{"IN_TRANSIT", "DISPATCHED"},
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

func TestBuildCapabilities(t *testing.T) {
	driver := ActorContext{UserID: 1, Roles: []string{"DRIVER"}}
	owner := ActorContext{UserID: 2, Roles: []string{"NURSERY_OWNER"}}
	mgr := ActorContext{UserID: 3, Roles: []string{"MANAGER"}}

	t.Run("driver_pending", func(t *testing.T) {
		caps := BuildCapabilities(driver, "PENDING")
		if !caps.CanAccept {
			t.Error("driver should CanAccept at PENDING")
		}
		if caps.CanStartTrip || caps.CanMarkDelivered || caps.CanMarkDispatched {
			t.Error("driver at PENDING should not have trip/dispatch caps")
		}
	})

	t.Run("driver_dispatched", func(t *testing.T) {
		caps := BuildCapabilities(driver, "DISPATCHED")
		if !caps.CanStartTrip {
			t.Error("driver should CanStartTrip at DISPATCHED")
		}
		if caps.CanAccept || caps.CanMarkDelivered {
			t.Error("driver at DISPATCHED should not have accept/deliver caps")
		}
	})

	t.Run("driver_in_transit", func(t *testing.T) {
		caps := BuildCapabilities(driver, "IN_TRANSIT")
		if !caps.CanMarkDelivered {
			t.Error("driver should CanMarkDelivered at IN_TRANSIT")
		}
	})

	t.Run("owner_pending", func(t *testing.T) {
		caps := BuildCapabilities(owner, "PENDING")
		if !caps.CanMarkDispatched {
			t.Error("owner should CanMarkDispatched at PENDING")
		}
		if !caps.CanCancel {
			t.Error("owner should CanCancel at PENDING")
		}
		if caps.CanAccept {
			t.Error("owner should not CanAccept")
		}
	})

	t.Run("owner_delivered_terminal", func(t *testing.T) {
		caps := BuildCapabilities(owner, "DELIVERED")
		if caps.CanMarkDispatched || caps.CanCancel || caps.CanAddItems {
			t.Error("no action caps should be set at terminal DELIVERED")
		}
	})

	t.Run("manager_accepted", func(t *testing.T) {
		caps := BuildCapabilities(mgr, "ACCEPTED")
		if !caps.CanMarkDispatched {
			t.Error("manager should CanMarkDispatched at ACCEPTED")
		}
	})

	t.Run("operator_in_transit", func(t *testing.T) {
		caps := BuildCapabilities(owner, "IN_TRANSIT")
		if !caps.CanMarkDelivered {
			t.Error("operator should CanMarkDelivered at IN_TRANSIT")
		}
	})
}
