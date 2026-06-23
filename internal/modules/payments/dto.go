package payments

type ListPaymentsRequest struct {
	Page           int
	PerPage        int
	Search         string
	PaymentFor     string
	OrderID        int64
	SubscriptionID int64
	PayerUserID    int64
	Status         string
	Method         string
	SortBy         string
	SortOrder      string
}

type ManualPaymentRequest struct {
	PaymentFor           string         `json:"payment_for"`
	OrderID              *int64         `json:"order_id"`
	UserSubscriptionID   *int64         `json:"user_subscription_id"`
	PayerUserID          *int64         `json:"payer_user_id"`
	Amount               float64        `json:"amount"`
	PaymentMethod        string         `json:"payment_method"`
	TransactionReference *string        `json:"transaction_reference"`
	Status               string         `json:"payment_status"`
	Notes                *string        `json:"notes"`
	Provider             *string        `json:"provider"`
	ProviderPaymentID    *string        `json:"provider_payment_id"`
	ProviderOrderID      *string        `json:"provider_order_id"`
	ProviderSignature    *string        `json:"provider_signature"`
	RawResponse          map[string]any `json:"raw_response"`
}

type UpdateStatusRequest struct {
	Status               string         `json:"payment_status"`
	TransactionReference *string        `json:"transaction_reference"`
	Notes                *string        `json:"notes"`
	Provider             *string        `json:"provider"`
	ProviderPaymentID    *string        `json:"provider_payment_id"`
	ProviderOrderID      *string        `json:"provider_order_id"`
	ProviderSignature    *string        `json:"provider_signature"`
	RawResponse          map[string]any `json:"raw_response"`
}

type Pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type PaymentsResponse struct {
	Payments   []Payment  `json:"payments"`
	Pagination Pagination `json:"pagination"`
}

type PaymentResponse struct {
	Payment Payment `json:"payment"`
}
