package admin

import (
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
)

type ActorContext = authctx.ActorContext

type Summary struct {
	Users              int64   `json:"users"`
	Nurseries          int64   `json:"nurseries"`
	PendingNurseries   int64   `json:"pending_nurseries"`
	ApprovedNurseries  int64   `json:"approved_nurseries"`
	SuspendedNurseries int64   `json:"suspended_nurseries"`
	Plants             int64   `json:"plants"`
	TopItems           int64   `json:"top_items"`
	PlantRequests      int64   `json:"plant_requests"`
	Orders             int64   `json:"orders"`
	ActiveOrders       int64   `json:"active_orders"`
	Payments           int64   `json:"payments"`
	Dispatches         int64   `json:"dispatches"`
	Notifications      int64   `json:"notifications"`
	ActiveDrivers      int64   `json:"active_drivers"`
	Revenue            float64 `json:"revenue"`

	// "Today" is the business day (DB session time zone, Asia/Kolkata).
	OrdersToday         int64       `json:"orders_today"`
	DispatchesToday     int64       `json:"dispatches_today"`
	ActiveTrips         int64       `json:"active_trips"` // ACCEPTED, DISPATCHED, IN_TRANSIT
	PendingPayments     int64       `json:"pending_payments"`
	OrderRevenue        float64     `json:"order_revenue"`        // successful order payments
	SubscriptionRevenue float64     `json:"subscription_revenue"` // successful subscription payments
	FailedLogins24h     int64       `json:"failed_logins_24h"`
	CrashReports24h     int64       `json:"crash_reports_24h"`
	BotanicalNames      int64       `json:"botanical_names"`
	Trend               []DayTotals `json:"trend"` // last 7 business days, oldest first
}

type DayTotals struct {
	Day        string `json:"day"` // YYYY-MM-DD
	Orders     int64  `json:"orders"`
	Dispatches int64  `json:"dispatches"`
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

type UpdateUserStatusRequest struct {
	Status string `json:"status"` // ACTIVE | SUSPENDED | DELETED
	Reason string `json:"reason"`
}

type UpdateNurseryStatusRequest struct {
	Status string `json:"status"` // ACTIVE | SUSPENDED
	Reason string `json:"reason"`
}
