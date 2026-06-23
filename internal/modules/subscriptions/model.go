package subscriptions

import "time"

const (
	actionInsert = "INSERT"
	actionUpdate = "UPDATE"

	statusActive    = "ACTIVE"
	statusPaused    = "PAUSED"
	statusCancelled = "CANCELLED"
	statusExpired   = "EXPIRED"
)

type SubscriptionPlan struct {
	ID           int64      `json:"id"`
	Code         string     `json:"plan_code"`
	Name         string     `json:"plan_name"`
	Description  *string    `json:"description,omitempty"`
	MonthlyPrice *float64   `json:"monthly_price,omitempty"`
	YearlyPrice  *float64   `json:"yearly_price,omitempty"`
	MaxUsers     *int       `json:"max_users,omitempty"`
	MaxNurseries *int       `json:"max_nurseries,omitempty"`
	IsActive     bool       `json:"is_active"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    *time.Time `json:"updated_at,omitempty"`
}

type UserSubscription struct {
	ID               int64      `json:"id"`
	SubscriptionCode string     `json:"subscription_code"`
	UserID           int64      `json:"user_id"`
	PlanID           int64      `json:"plan_id"`
	PlanCode         string     `json:"plan_code"`
	PlanName         string     `json:"plan_name"`
	StartDate        time.Time  `json:"start_date"`
	EndDate          *time.Time `json:"end_date,omitempty"`
	Status           string     `json:"subscription_status"`
	AutoRenew        bool       `json:"auto_renew"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        *time.Time `json:"updated_at,omitempty"`
	LatestPayment    *Payment   `json:"latest_payment,omitempty"`
}

type Payment struct {
	ID                   int64      `json:"id"`
	PaymentCode          string     `json:"payment_code"`
	Amount               float64    `json:"amount"`
	PaymentMethod        *string    `json:"payment_method,omitempty"`
	TransactionReference *string    `json:"transaction_reference,omitempty"`
	Status               string     `json:"payment_status"`
	PaymentDate          *time.Time `json:"payment_date,omitempty"`
	Provider             *string    `json:"provider,omitempty"`
	ProviderPaymentID    *string    `json:"provider_payment_id,omitempty"`
	ProviderOrderID      *string    `json:"provider_order_id,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
}

type ActorContext struct {
	UserID    int64
	Roles     []string
	IPAddress string
	UserAgent string
}
