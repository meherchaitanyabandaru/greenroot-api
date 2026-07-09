package subscriptions

type ListSubscriptionsRequest struct {
	Page    int
	PerPage int
	UserID  int64
	Status  string
	Search  string
}

type CreateSubscriptionRequest struct {
	UserID        *int64  `json:"user_id"`
	PlanID        int64   `json:"plan_id"`
	BillingCycle  string  `json:"billing_cycle"`
	StartDate     *string `json:"start_date"`
	AutoRenew     bool    `json:"auto_renew"`
	PaymentMethod *string `json:"payment_method"`
	Provider      *string `json:"provider"`
	ProviderRef   *string `json:"provider_order_id"`
	PromoCode     *string `json:"promo_code"`
}

type UpdateStatusRequest struct {
	Status string `json:"subscription_status"`
}

type RenewSubscriptionRequest struct {
	BillingCycle  string  `json:"billing_cycle"`
	PaymentMethod *string `json:"payment_method"`
	Provider      *string `json:"provider"`
	ProviderRef   *string `json:"provider_order_id"`
	PromoCode     *string `json:"promo_code"`
}

type CreatePromoRequest struct {
	PromoCode        string   `json:"promo_code"`
	Name             string   `json:"name"`
	Description      *string  `json:"description"`
	DiscountType     string   `json:"discount_type"`
	DiscountValue    float64  `json:"discount_value"`
	MaxDiscountCap   *float64 `json:"max_discount_cap"`
	ApplicablePlans  []string `json:"applicable_plans"`
	ApplicableCycles []string `json:"applicable_cycles"`
	ValidFrom        string   `json:"valid_from"`
	ValidUntil       string   `json:"valid_until"`
	MaxUses          *int     `json:"max_uses"`
}

type UpdatePromoRequest struct {
	Name             string   `json:"name"`
	Description      *string  `json:"description"`
	DiscountType     string   `json:"discount_type"`
	DiscountValue    float64  `json:"discount_value"`
	MaxDiscountCap   *float64 `json:"max_discount_cap"`
	ApplicablePlans  []string `json:"applicable_plans"`
	ApplicableCycles []string `json:"applicable_cycles"`
	ValidFrom        string   `json:"valid_from"`
	ValidUntil       string   `json:"valid_until"`
	IsActive         bool     `json:"is_active"`
	MaxUses          *int     `json:"max_uses"`
}

type ValidatePromoRequest struct {
	PromoCode    string `json:"promo_code"`
	PlanCode     string `json:"plan_code"`
	BillingCycle string `json:"billing_cycle"`
}

type PromosResponse struct {
	Promos []SubscriptionPromo `json:"promos"`
}

type PromoResponse struct {
	Promo SubscriptionPromo `json:"promo"`
}

type PromoValidationResponse struct {
	Validation PromoValidation `json:"validation"`
}

type BlastResponse struct {
	SentCount int `json:"sent_count"`
}

type CancelSubscriptionRequest struct {
	CancelImmediately bool    `json:"cancel_immediately"`
	Reason            *string `json:"reason"`
}

type Pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type PlansResponse struct {
	Plans []SubscriptionPlan `json:"plans"`
}

type PlanResponse struct {
	Plan SubscriptionPlan `json:"plan"`
}

type SubscriptionsResponse struct {
	Subscriptions []UserSubscription `json:"subscriptions"`
	Pagination    Pagination         `json:"pagination"`
}

type SubscriptionResponse struct {
	Subscription UserSubscription `json:"subscription"`
}

type PaymentsResponse struct {
	Payments []Payment `json:"payments"`
}

type UpdatePlanRequest struct {
	Name          string         `json:"plan_name"`
	Description   *string        `json:"description"`
	SixMonthPrice float64        `json:"six_month_price"`
	YearlyPrice   float64        `json:"yearly_price"`
	MaxManagers   *int           `json:"max_managers"`
	IsActive      bool           `json:"is_active"`
	Features      map[string]any `json:"features"`
}

type MessageResponse struct {
	Message string `json:"message"`
}
