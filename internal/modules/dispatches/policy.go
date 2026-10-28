package dispatches

// policy.go — single source of truth for dispatch business rules.
// All functions are pure, stateless, and take no database parameters.
// The service layer, handler layer, and lifecycle metadata all derive from these.

// ── Status predicates ─────────────────────────────────────────────────────────

// AllowedStatus reports whether s is a recognised dispatch status value.
func AllowedStatus(s string) bool {
	switch s {
	case "PENDING", "ACCEPTED", "DISPATCHED", "IN_TRANSIT", "DELIVERED", "CANCELLED":
		return true
	}
	return false
}

// IsTerminal returns true when no further status transitions are possible.
func IsTerminal(s string) bool {
	return s == "DELIVERED" || s == "CANCELLED"
}

// IsActive returns true for statuses that represent an ongoing driver commitment.
// A driver cannot accept another trip while in one of these states.
func IsActive(s string) bool {
	switch s {
	case "ACCEPTED", "DISPATCHED", "IN_TRANSIT":
		return true
	}
	return false
}

// IsLiveTrackingTerminal returns true when the live GPS position should be removed from Redis.
func IsLiveTrackingTerminal(s string) bool {
	switch s {
	case "DELIVERED", "CANCELLED", "EXPIRED":
		return true
	}
	return false
}

// IsDispatchableOrderStatus returns true when an order is ready for a dispatch to be created.
func IsDispatchableOrderStatus(s string) bool {
	return s == "LOADED" || s == "PARTIALLY_FULFILLED"
}

// CanTransition reports whether the from→to status change is valid.
//
// PENDING → ACCEPTED  is handled exclusively by AcceptDispatch (driver QR scan).
// All other transitions go through UpdateStatus.
//
//	PENDING → DISPATCHED | CANCELLED
//	ACCEPTED → DISPATCHED | IN_TRANSIT | CANCELLED   (the driver may start without a hand-over)
//	DISPATCHED → IN_TRANSIT
//	IN_TRANSIT → DELIVERED
//	DELIVERED / CANCELLED → terminal (no transitions)
func CanTransition(from, to string) bool {
	switch from {
	case "PENDING":
		return to == "DISPATCHED" || to == "CANCELLED"
	case "ACCEPTED":
		return to == "DISPATCHED" || to == "IN_TRANSIT" || to == "CANCELLED"
	case "DISPATCHED":
		return to == "IN_TRANSIT"
	case "IN_TRANSIT":
		return to == "DELIVERED"
	}
	return false
}

// ── Capabilities ──────────────────────────────────────────────────────────────

// DispatchCapabilities is the role-aware boolean gate set returned to UI clients.
// Clients must not re-implement these checks — render solely from these flags.
type DispatchCapabilities struct {
	CanAccept         bool `json:"can_accept"`
	CanMarkDispatched bool `json:"can_mark_dispatched"`
	CanStartTrip      bool `json:"can_start_trip"`
	CanMarkDelivered  bool `json:"can_mark_delivered"`
	CanCancel         bool `json:"can_cancel"`
	CanAddItems       bool `json:"can_add_items"`
	CanViewTracking   bool `json:"can_view_tracking"`
	CanShareCode      bool `json:"can_share_code"`
	// Without the customer's code: staff only, with a reason.
	CanOverrideDelivery bool `json:"can_override_delivery"`
	// The customer's own actions (set in withLifecycle, need the dispatch).
	CanConfirmReceipt bool `json:"can_confirm_receipt"`
	CanReportIssue    bool `json:"can_report_issue"`
}

// How a delivery was confirmed (dispatches.delivery_confirmation).
const (
	ConfirmationCode        = "CUSTOMER_CODE"
	ConfirmationApp         = "CUSTOMER_APP"
	ConfirmationUnconfirmed = "UNCONFIRMED"
)

// BuildCapabilities computes the capability set for the given actor and dispatch status.
// No database access — status string and actor roles are the only inputs.
// buyerSide is true when the actor is this trip's customer: they get no
// operator actions even if they also own or manage a nursery.
func BuildCapabilities(actor ActorContext, status string, buyerSide bool) DispatchCapabilities {
	isDriver := actor.HasRole("DRIVER")
	isOperator := (actor.HasRole("NURSERY_OWNER") || actor.HasRole("MANAGER")) && !buyerSide
	isAdmin := actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN")
	terminal := IsTerminal(status)

	return DispatchCapabilities{
		CanAccept:         isDriver && status == "PENDING",
		CanMarkDispatched: (isOperator || isAdmin) && (status == "PENDING" || status == "ACCEPTED"),
		CanStartTrip:      isDriver && (status == "ACCEPTED" || status == "DISPATCHED"),
		// The lorry may arrive before the driver taps Start: the customer's code is what counts.
		CanMarkDelivered:    (isDriver || isOperator || isAdmin) && !buyerSide && onTheWay(status),
		CanOverrideDelivery: (isOperator || isAdmin) && onTheWay(status),
		CanCancel:           (isOperator || isAdmin) && !terminal,
		CanAddItems:         (isOperator || isAdmin) && !terminal,
		CanViewTracking:     IsActive(status),
		CanShareCode:        (isOperator || isAdmin) && status == "PENDING", // the QR only lets a driver accept
	}
}
