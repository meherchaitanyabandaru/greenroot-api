package admin

type Summary struct {
	Users          int64   `json:"users"`
	Nurseries      int64   `json:"nurseries"`
	Plants         int64   `json:"plants"`
	InventoryItems int64   `json:"inventory_items"`
	PlantRequests  int64   `json:"plant_requests"`
	Orders         int64   `json:"orders"`
	Payments       int64   `json:"payments"`
	Dispatches     int64   `json:"dispatches"`
	Notifications  int64   `json:"notifications"`
	Revenue        float64 `json:"revenue"`
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
