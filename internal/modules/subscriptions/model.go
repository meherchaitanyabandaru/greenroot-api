package subscriptions

import (
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/lifecycle"
)

type ActorContext = authctx.ActorContext

const (
	actionInsert = "INSERT"
	actionUpdate = "UPDATE"

	statusActive    = "ACTIVE"
	statusPaused    = "PAUSED"
	statusCancelled = "CANCELLED"
	statusExpired   = "EXPIRED"
)

type SubscriptionPlan struct {
	ID            int64          `json:"id"`
	PlanID        int64          `json:"plan_id"`
	Code          string         `json:"plan_code"`
	PlanType      string         `json:"plan_type"`
	Name          string         `json:"plan_name"`
	Description   *string        `json:"description,omitempty"`
	SixMonthPrice *float64       `json:"six_month_price,omitempty"`
	YearlyPrice   *float64       `json:"yearly_price,omitempty"`
	MaxManagers   *int           `json:"max_managers,omitempty"`
	MaxNurseries  *int           `json:"max_nurseries,omitempty"`
	IsActive      bool           `json:"is_active"`
	Features      map[string]any `json:"features,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     *time.Time     `json:"updated_at,omitempty"`
	// keep for backward compat with existing subscription create/renew logic
	MonthlyPrice *float64 `json:"-"`
}

type UserSubscription struct {
	ID               int64                           `json:"id"`
	SubscriptionCode string                          `json:"subscription_code"`
	UserID           int64                           `json:"user_id"`
	PlanID           int64                           `json:"plan_id"`
	PlanCode         string                          `json:"plan_code"`
	PlanName         string                          `json:"plan_name"`
	StartDate        time.Time                       `json:"start_date"`
	EndDate          *time.Time                      `json:"end_date,omitempty"`
	Status           string                          `json:"subscription_status"`
	AutoRenew        bool                            `json:"auto_renew"`
	DaysRemaining    *int                            `json:"days_remaining,omitempty"`
	CreatedAt        time.Time                       `json:"created_at"`
	UpdatedAt        *time.Time                      `json:"updated_at,omitempty"`
	LatestPayment    *Payment                        `json:"latest_payment,omitempty"`
	Lifecycle        *lifecycle.SubscriptionDisplays `json:"lifecycle,omitempty"`
	Capabilities     *SubscriptionCapabilities       `json:"capabilities,omitempty"`
	Summary          *SubscriptionSummary            `json:"summary,omitempty"`
}

type SubscriptionCapabilities struct {
	CanRenew        bool `json:"can_renew"`
	CanCancel       bool `json:"can_cancel"`
	CanPause        bool `json:"can_pause"`
	CanResume       bool `json:"can_resume"`
	CanChangePlan   bool `json:"can_change_plan"`
	CanRetryPayment bool `json:"can_retry_payment"`
}

type SubscriptionSummary struct {
	IsActive       bool   `json:"is_active"`
	IsExpired      bool   `json:"is_expired"`
	IsExpiringSoon bool   `json:"is_expiring_soon"`
	PaymentStatus  string `json:"payment_status,omitempty"`
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

type SubscriptionPromo struct {
	ID               int64      `json:"id"`
	PromoCode        string     `json:"promo_code"`
	Name             string     `json:"name"`
	Description      *string    `json:"description,omitempty"`
	DiscountType     string     `json:"discount_type"`
	DiscountValue    float64    `json:"discount_value"`
	MaxDiscountCap   *float64   `json:"max_discount_cap,omitempty"`
	ApplicablePlans  []string   `json:"applicable_plans"`
	ApplicableCycles []string   `json:"applicable_cycles"`
	ValidFrom        string     `json:"valid_from"`
	ValidUntil       string     `json:"valid_until"`
	IsActive         bool       `json:"is_active"`
	MaxUses          *int       `json:"max_uses,omitempty"`
	UsedCount        int        `json:"used_count"`
	CreatedBy        *int64     `json:"created_by,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        *time.Time `json:"updated_at,omitempty"`
}

type PromoValidation struct {
	Valid         bool    `json:"valid"`
	PromoCode     string  `json:"promo_code"`
	DiscountType  string  `json:"discount_type"`
	DiscountValue float64 `json:"discount_value"`
	BasePrice     float64 `json:"base_price"`
	Savings       float64 `json:"savings"`
	FinalPrice    float64 `json:"final_price"`
	Message       string  `json:"message,omitempty"`
}

type BlastTarget struct {
	UserID   int64  `json:"user_id"`
	FullName string `json:"full_name"`
}
