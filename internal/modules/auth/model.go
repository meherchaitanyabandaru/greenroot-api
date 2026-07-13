package auth

import "time"

const (
	mockOTP              = "123456"
	otpTTL               = 5 * time.Minute
	defaultUserFirstName = "GreenRoot"
	defaultUserRole      = "BUYER"
	activityLogin        = "LOGIN"
	sessionStatusActive  = "ACTIVE"
	sessionStatusLogout  = "LOGGED_OUT"
)

type User struct {
	ID              int64      `json:"id"`
	UserCode        string     `json:"user_code"`
	FirstName       string     `json:"first_name"`
	LastName        *string    `json:"last_name,omitempty"`
	Mobile          string     `json:"mobile"`
	Email           *string    `json:"email,omitempty"`
	ProfileImageURL *string    `json:"profile_image_url,omitempty"`
	MobileVerified  bool       `json:"mobile_verified"`
	EmailVerified   bool       `json:"email_verified"`
	Status          string     `json:"status"`
	LastLoginAt     *time.Time `json:"last_login_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	Roles           []string   `json:"roles"`
}

type Session struct {
	ID             int64     `json:"id"`
	UserID         int64     `json:"user_id"`
	Status         string    `json:"status"`
	LoginTime      time.Time `json:"login_time"`
	LastActivityAt time.Time `json:"last_activity_at"`
}

// Workspace represents one context a user can operate in after login.
// Types: PERSONAL | OWNED_NURSERY | MANAGER_NURSERY | DRIVER
type Workspace struct {
	Type          string  `json:"type"`
	Role          string  `json:"role"`
	NurseryID     *int64  `json:"nursery_id,omitempty"`
	NurseryName   *string `json:"nursery_name,omitempty"`
	NurseryStatus *string `json:"nursery_status,omitempty"` // only for OWNED_NURSERY
}

// OwnerDashboard aggregates all key metrics for a nursery owner.
type OwnerDashboard struct {
	NurseryID   *int64            `json:"nursery_id,omitempty"`
	NurseryName *string           `json:"nursery_name,omitempty"`
	SellOrders  OrderMetrics      `json:"sell_orders"`
	BuyOrders   OrderMetrics      `json:"buy_orders"`
	SellQuotes  QuoteMetrics      `json:"sell_quotations"`
	BuyQuotes   QuoteMetrics      `json:"buy_quotations"`
	Inventory   InventoryMetrics  `json:"inventory"`
	Connections ConnectionMetrics `json:"connections"`
}

type OrderMetrics struct {
	Total     int `json:"total"`
	Pending   int `json:"pending"`
	Confirmed int `json:"confirmed"`
	Delivered int `json:"delivered"`
	Cancelled int `json:"cancelled"`
}

type QuoteMetrics struct {
	Total    int `json:"total"`
	Pending  int `json:"pending"`
	Approved int `json:"approved"`
	Rejected int `json:"rejected"`
}

type InventoryMetrics struct {
	TotalItems int `json:"total_items"`
	Available  int `json:"available"`
}

type ConnectionMetrics struct {
	Managers  int `json:"managers"`
	Drivers   int `json:"drivers"`
	Customers int `json:"customers"`
}
