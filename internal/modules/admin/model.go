package admin

type Summary struct {
	Users              int64   `json:"users"`
	Nurseries          int64   `json:"nurseries"`
	PendingNurseries   int64   `json:"pending_nurseries"`
	ApprovedNurseries  int64   `json:"approved_nurseries"`
	SuspendedNurseries int64   `json:"suspended_nurseries"`
	Plants             int64   `json:"plants"`
	InventoryItems     int64   `json:"inventory_items"`
	PlantRequests      int64   `json:"plant_requests"`
	Orders             int64   `json:"orders"`
	ActiveOrders       int64   `json:"active_orders"`
	Payments           int64   `json:"payments"`
	Dispatches         int64   `json:"dispatches"`
	Notifications      int64   `json:"notifications"`
	ActiveDrivers      int64   `json:"active_drivers"`
	Revenue            float64 `json:"revenue"`
}

type User struct {
	ID             int64    `json:"id"`
	UserCode       string   `json:"user_code"`
	FirstName      string   `json:"first_name"`
	LastName       *string  `json:"last_name,omitempty"`
	Mobile         string   `json:"mobile"`
	Email          *string  `json:"email,omitempty"`
	Status         string   `json:"status"`
	MobileVerified bool     `json:"mobile_verified"`
	EmailVerified  bool     `json:"email_verified"`
	LastLoginAt    *string  `json:"last_login_at,omitempty"`
	CreatedAt      *string  `json:"created_at,omitempty"`
	Roles          []string `json:"roles"`
	SessionCount   int64    `json:"session_count"`
}

type ActorContext struct {
	UserID    int64
	Roles     []string
	IPAddress string
	UserAgent string
}

type UpdateUserStatusRequest struct {
	Status string `json:"status"` // ACTIVE | SUSPENDED | DELETED
	Reason string `json:"reason"`
}

type UpdateNurseryStatusRequest struct {
	Status string `json:"status"` // ACTIVE | SUSPENDED
	Reason string `json:"reason"`
}
