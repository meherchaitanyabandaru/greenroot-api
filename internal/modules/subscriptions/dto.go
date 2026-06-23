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
}

type UpdateStatusRequest struct {
	Status string `json:"subscription_status"`
}

type RenewSubscriptionRequest struct {
	BillingCycle  string  `json:"billing_cycle"`
	PaymentMethod *string `json:"payment_method"`
	Provider      *string `json:"provider"`
	ProviderRef   *string `json:"provider_order_id"`
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

type MessageResponse struct {
	Message string `json:"message"`
}
