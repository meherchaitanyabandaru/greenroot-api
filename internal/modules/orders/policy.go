package orders

// policy.go — single source of truth for order business rules.
// All functions are pure, stateless, and take no database parameters.

// AllowedStatus reports whether s is a recognised order status that can be
// targeted via the generic PUT /orders/{id}/status endpoint.
func AllowedStatus(s string) bool {
	switch s {
	case "PENDING", "CONFIRMED", "PARTIALLY_FULFILLED", "COMPLETED", "CANCELLED":
		return true
	}
	return false
}

// IsTerminal returns true when the order has reached a final state.
func IsTerminal(s string) bool {
	return s == "COMPLETED" || s == "CANCELLED"
}

// IsEditable returns true while order items can still be added/edited/deleted.
func IsEditable(s string) bool {
	switch s {
	case "PENDING", "CONFIRMED", "LOADING":
		return true
	}
	return false
}

// CanTransition enforces the order lifecycle state machine for the generic
// PUT /orders/{id}/status endpoint. Dedicated endpoints (start-loading,
// complete-loading, cancel) enforce their own transitions separately.
//
//	PENDING/DRAFT → CONFIRMED
//	LOADED → COMPLETED
//	PARTIALLY_FULFILLED → COMPLETED
func CanTransition(from, to string) bool {
	switch from {
	case "PENDING", "DRAFT":
		return to == "CONFIRMED"
	case "LOADED":
		return to == "COMPLETED"
	case "PARTIALLY_FULFILLED":
		return to == "COMPLETED"
	}
	return false
}

// CanCancel returns true when the order status allows cancellation.
// Buyer self-cancel (own PENDING order) is an additional constraint checked in the service.
func CanCancel(status string) bool {
	switch status {
	case "CANCELLED", "COMPLETED", "LOADED", "PARTIALLY_FULFILLED":
		return false
	}
	return true
}

// OrderCapabilities is the role-aware boolean gate set returned to UI clients.
// Clients must not re-implement these checks — render solely from these flags.
type OrderCapabilities struct {
	CanEdit            bool `json:"can_edit"`
	CanConfirm         bool `json:"can_confirm"`
	CanCancel          bool `json:"can_cancel"`
	CanDelete          bool `json:"can_delete"`
	CanStartLoading    bool `json:"can_start_loading"`
	CanCompleteLoading bool `json:"can_complete_loading"`
	CanAddItems        bool `json:"can_add_items"`
}

// BuildCapabilities computes the capability set for the given actor and order status.
// No database access — status string and actor roles are the only inputs.
func BuildCapabilities(actor ActorContext, status string) OrderCapabilities {
	isOperator := hasRole(actor, "NURSERY_OWNER") || hasRole(actor, "MANAGER")
	isAdmin := hasRole(actor, "ADMIN") || hasRole(actor, "SUPER_ADMIN")
	isBuyer := hasRole(actor, "BUYER")
	isManage := isOperator || isAdmin
	editable := IsEditable(status)

	// Buyers may cancel their own PENDING order; service enforces ownership.
	buyerCanCancel := isBuyer && status == "PENDING"

	return OrderCapabilities{
		CanEdit:            isManage && editable,
		CanConfirm:         isManage && (status == "PENDING" || status == "DRAFT"),
		CanCancel:          (isManage && CanCancel(status)) || buyerCanCancel,
		CanDelete:          isManage && status == "PENDING",
		CanStartLoading:    isManage && (status == "CONFIRMED" || status == "DRAFT"),
		CanCompleteLoading: isManage && status == "LOADING",
		CanAddItems:        isManage && editable,
	}
}
