package payments

import (
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"time"
)

type ActorContext = authctx.ActorContext

const (
	actionInsert = "INSERT"
	actionUpdate = "UPDATE"

	paymentForOrder        = "ORDER"
	paymentForSubscription = "SUBSCRIPTION"
)

type Payment struct {
	ID                   int64      `json:"id"`
	PaymentCode          string     `json:"payment_code"`
	PaymentFor           string     `json:"payment_for"`
	OrderID              *int64     `json:"order_id,omitempty"`
	OrderNumber          *string    `json:"order_number,omitempty"`
	UserSubscriptionID   *int64     `json:"user_subscription_id,omitempty"`
	SubscriptionPlan     *string    `json:"subscription_plan,omitempty"`
	PayerUserID          *int64     `json:"payer_user_id,omitempty"`
	PayerName            *string    `json:"payer_name,omitempty"`
	Amount               float64    `json:"amount"`
	PaymentMethod        *string    `json:"payment_method,omitempty"`
	TransactionReference *string    `json:"transaction_reference,omitempty"`
	Status               string     `json:"payment_status"`
	PaymentDate          *time.Time `json:"payment_date,omitempty"`
	Notes                *string    `json:"notes,omitempty"`
	Provider             *string    `json:"provider,omitempty"`
	ProviderPaymentID    *string    `json:"provider_payment_id,omitempty"`
	ProviderOrderID      *string    `json:"provider_order_id,omitempty"`
	ProviderSignature    *string    `json:"provider_signature,omitempty"`
	RawResponse          *string    `json:"raw_response,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}
