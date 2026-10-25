package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/platform/razorpay"
)

// Razorpay Standard Checkout for subscription payments:
//  1. POST /subscriptions/payment-order prices the plan here (never trusting a
//     client amount) and creates a Razorpay order tagged with who/what it pays for.
//  2. The app opens checkout with that order and gets payment id + signature.
//  3. POST /subscriptions (or /{id}/renew) with provider "razorpay" and those
//     ids: signature, order (who/what/amount) and payment (captured; captured
//     here if only authorized) are checked BEFORE anything is extended, and the
//     change + payment record are written in one transaction.
//  4. The webhook (payment.captured / order.paid) applies the same order if
//     the app never got to step 3. Steps 3 and 4 are idempotent.

const providerRazorpay = "razorpay"

var (
	ErrGatewayDisabled = errors.New("payment gateway not configured")
	ErrPaymentRequired = errors.New("payment required")
	ErrPaymentInvalid  = errors.New("payment verification failed")
	ErrPaymentUsed     = errors.New("payment already used")
)

// PaymentGateway is what the service needs from Razorpay (faked in tests).
type PaymentGateway interface {
	KeyID() string
	CreateOrder(ctx context.Context, amountPaise int64, receipt string, notes razorpay.Notes) (razorpay.Order, error)
	FetchOrder(ctx context.Context, orderID string) (razorpay.Order, error)
	VerifySignature(orderID, paymentID, signature string) bool
	FetchPayment(ctx context.Context, paymentID string) (razorpay.Payment, error)
	CapturePayment(ctx context.Context, paymentID string, amountPaise int64) (razorpay.Payment, error)
	RefundPayment(ctx context.Context, paymentID string, amountPaise int64, notes razorpay.Notes) (razorpay.Refund, error)
}

// SetGateway turns on real payments: owners must then pay through Razorpay.
// webhookSecret verifies Razorpay webhooks ("" = webhook refused).
func (s *Service) SetGateway(g PaymentGateway, webhookSecret string) {
	s.gateway, s.webhookSecret = g, webhookSecret
}

// verifiedPayment is a checkout result whose signature, order and capture checked out.
type verifiedPayment struct {
	OrderID, PaymentID, Signature string
	Method                        string // the method actually used, from Razorpay
}

// paymentMethods maps Razorpay's method to ours (payments.payment_method).
var paymentMethods = map[string]string{"card": "CARD", "upi": "UPI", "netbanking": "NET_BANKING", "wallet": "WALLET"}

func toPaise(amount float64) int64 { return int64(math.Round(amount * 100)) }

// orderNotes bind an order to what it pays for. promo_code is kept so the
// webhook can re-price; the amount check covers it, so it is not compared.
func orderNotes(userID int64, subscription string, planID int64, cycle string) razorpay.Notes {
	return razorpay.Notes{
		"user_id":       strconv.FormatInt(userID, 10),
		"subscription":  subscription, // "new" or the subscription id being renewed
		"plan_id":       strconv.FormatInt(planID, 10),
		"billing_cycle": cycle,
	}
}

// CreatePaymentOrder creates the Razorpay order for a new subscription
// (SubscriptionID empty) or a renewal, at the price the API computes.
func (s *Service) CreatePaymentOrder(ctx context.Context, actor ActorContext, req PaymentOrderRequest) (PaymentOrder, error) {
	if s.gateway == nil {
		return PaymentOrder{}, ErrGatewayDisabled
	}
	userID, subscription, planID := actor.UserID, "new", int64(0)
	if req.SubscriptionID != nil && *req.SubscriptionID > 0 {
		current, err := s.repository.FindByID(ctx, *req.SubscriptionID)
		if err != nil {
			return PaymentOrder{}, err
		}
		if err := s.canAccess(actor, *current); err != nil {
			return PaymentOrder{}, err
		}
		if current.Status == statusPaused && !actor.HasRole("ADMIN") {
			return PaymentOrder{}, ErrPaused
		}
		if paidTooFarAhead(*current) {
			return PaymentOrder{}, ErrPaidAhead
		}
		userID, subscription, planID = current.UserID, strconv.FormatInt(current.ID, 10), current.PlanID
	} else if _, err := s.repository.FindActiveByUser(ctx, userID); err == nil {
		return PaymentOrder{}, ErrConflict // don't take money for a subscription Create will refuse
	} else if !errors.Is(err, ErrNotFound) {
		return PaymentOrder{}, err
	}
	if req.PlanID != nil && *req.PlanID > 0 {
		planID = *req.PlanID
	}
	if planID <= 0 {
		return PaymentOrder{}, ErrInvalidInput
	}
	plan, err := s.repository.FindPlan(ctx, planID)
	if err != nil {
		return PaymentOrder{}, err
	}
	if !plan.IsActive || isFreePlan(plan.Code) {
		return PaymentOrder{}, ErrInvalidInput
	}
	cycle := strings.ToUpper(strings.TrimSpace(req.BillingCycle))
	_, amount, err := cycleEndAndAmount(time.Now(), cycle, *plan)
	if err != nil {
		return PaymentOrder{}, ErrInvalidInput
	}
	finalAmount, _, err := s.applyPromoDiscount(ctx, req.PromoCode, plan.Code, cycle, amount)
	if err != nil {
		return PaymentOrder{}, ErrInvalidInput
	}
	paise := toPaise(finalAmount)
	if paise < razorpay.MinAmountPaise {
		return PaymentOrder{}, ErrInvalidInput
	}
	receipt := fmt.Sprintf("sub-%s-u%d-%d", subscription, userID, time.Now().Unix()) // ≤ 40 chars
	notes := orderNotes(userID, subscription, planID, cycle)
	notes["promo_code"] = strings.TrimSpace(stringOrEmpty(req.PromoCode))
	order, err := s.gateway.CreateOrder(ctx, paise, receipt, notes)
	if err != nil {
		return PaymentOrder{}, err
	}
	return PaymentOrder{OrderID: order.ID, Amount: order.Amount, Currency: order.Currency, KeyID: s.gateway.KeyID()}, nil
}

// appliedSubscription: if this exact Razorpay payment already paid for this
// subscription (a retried request, or the webhook got there first), the
// subscription it paid for; nil otherwise. Lets create/renew answer a retry
// with success instead of "already used".
func (s *Service) appliedSubscription(ctx context.Context, userID int64, subscription string, provider, orderID, paymentID *string) (*UserSubscription, error) {
	if s.gateway == nil || !strings.EqualFold(strings.TrimSpace(stringOrEmpty(provider)), providerRazorpay) {
		return nil, nil
	}
	o, p := strings.TrimSpace(stringOrEmpty(orderID)), strings.TrimSpace(stringOrEmpty(paymentID))
	if o == "" || p == "" {
		return nil, nil
	}
	subID, err := s.repository.GatewayPaymentSubscription(ctx, providerRazorpay, o, p)
	if err != nil || subID == 0 {
		return nil, err
	}
	sub, err := s.repository.FindByID(ctx, subID)
	if err != nil {
		return nil, err
	}
	same := sub.UserID == userID && (subscription == "new" || subscription == strconv.FormatInt(subID, 10))
	if !same {
		return nil, ErrPaymentUsed
	}
	return sub, nil
}

// checkPayment decides whether a create/renew may go ahead. nil, nil = no
// gateway payment (mock mode, admin recording a manual payment, or nothing to pay).
// trusted: the payment came from a verified webhook (no checkout signature).
func (s *Service) checkPayment(ctx context.Context, actor ActorContext, userID int64, subscription string, planID int64, cycle string, amount float64, provider, orderID, paymentID, signature *string, trusted bool) (*verifiedPayment, error) {
	if !strings.EqualFold(strings.TrimSpace(stringOrEmpty(provider)), providerRazorpay) {
		if s.gateway != nil && amount > 0 && !actor.HasRole("ADMIN") {
			return nil, ErrPaymentRequired
		}
		return nil, nil
	}
	if s.gateway == nil {
		return nil, ErrGatewayDisabled
	}
	p := verifiedPayment{
		OrderID:   strings.TrimSpace(stringOrEmpty(orderID)),
		PaymentID: strings.TrimSpace(stringOrEmpty(paymentID)),
		Signature: strings.TrimSpace(stringOrEmpty(signature)),
	}
	if p.OrderID == "" || p.PaymentID == "" || (p.Signature == "" && !trusted) {
		return nil, ErrInvalidInput
	}
	if !trusted && !s.gateway.VerifySignature(p.OrderID, p.PaymentID, p.Signature) {
		return nil, ErrPaymentInvalid
	}
	// A valid signature proves payment for that order; the order proves what for.
	order, err := s.gateway.FetchOrder(ctx, p.OrderID)
	if err != nil {
		return nil, err
	}
	for k, v := range orderNotes(userID, subscription, planID, cycle) {
		if order.Notes[k] != v {
			return nil, ErrPaymentInvalid
		}
	}
	if order.Amount != toPaise(amount) {
		return nil, ErrPaymentInvalid
	}
	used, err := s.repository.GatewayPaymentSubscription(ctx, providerRazorpay, p.OrderID, p.PaymentID)
	if err != nil {
		return nil, err
	}
	if used != 0 {
		return nil, ErrPaymentUsed
	}
	// The signature only proves the payment was authorized. Make sure the money
	// is captured: an authorized-only payment is refunded by Razorpay in days.
	payment, err := s.gateway.FetchPayment(ctx, p.PaymentID)
	if err != nil {
		return nil, err
	}
	if payment.OrderID != p.OrderID || payment.Amount != order.Amount {
		return nil, ErrPaymentInvalid
	}
	switch payment.Status {
	case "captured":
	case "authorized":
		if payment, err = s.gateway.CapturePayment(ctx, p.PaymentID, order.Amount); err != nil {
			return nil, err
		}
		if payment.Status != "captured" {
			return nil, ErrPaymentInvalid
		}
	default: // created, failed, refunded
		return nil, ErrPaymentInvalid
	}
	p.Method = paymentMethods[payment.Method]
	return &p, nil
}

// GatewayApply is the outcome of applying a webhook payment.
type GatewayApply struct {
	Applied bool
	// Refusal is set when a business rule refuses a captured payment (e.g.
	// paused since checkout): the money must be refunded or applied by hand.
	Refusal     error
	Ours        bool // the order was made by CreatePaymentOrder
	UserID      int64
	AmountPaise int64
}

// ApplyGatewayPayment applies a captured Razorpay payment for an order made by
// CreatePaymentOrder, as its owner would have (webhook path). Already applied
// = done. err means try again later (database, Razorpay or lock trouble).
func (s *Service) ApplyGatewayPayment(ctx context.Context, orderID, paymentID string) (GatewayApply, error) {
	if s.gateway == nil {
		return GatewayApply{}, ErrGatewayDisabled
	}
	order, err := s.gateway.FetchOrder(ctx, orderID)
	if err != nil {
		return GatewayApply{}, err
	}
	userID, _ := strconv.ParseInt(order.Notes["user_id"], 10, 64)
	planID, _ := strconv.ParseInt(order.Notes["plan_id"], 10, 64)
	subscription := order.Notes["subscription"]
	if userID <= 0 || planID <= 0 || subscription == "" {
		return GatewayApply{}, nil // not an order we created
	}
	out := GatewayApply{Ours: true, UserID: userID, AmountPaise: order.Amount}
	var promo *string
	if code := order.Notes["promo_code"]; code != "" {
		promo = &code
	}
	actor := ActorContext{UserID: userID}
	provider := providerRazorpay
	if subscription == "new" {
		_, err = s.Create(ctx, actor, CreateSubscriptionRequest{
			PlanID: planID, BillingCycle: order.Notes["billing_cycle"], PromoCode: promo,
			Provider: &provider, ProviderRef: &orderID, ProviderPaymentID: &paymentID, trusted: true,
		})
	} else {
		subID, _ := strconv.ParseInt(subscription, 10, 64)
		_, err = s.Renew(ctx, actor, subID, RenewSubscriptionRequest{
			PlanID: &planID, BillingCycle: order.Notes["billing_cycle"], PromoCode: promo,
			Provider: &provider, ProviderRef: &orderID, ProviderPaymentID: &paymentID, trusted: true,
		})
	}
	if err == nil {
		out.Applied = true
		return out, nil
	}
	for _, refusal := range []error{ErrPaused, ErrPaidAhead, ErrConflict, ErrInvalidInput, ErrForbidden,
		ErrNotFound, ErrPaymentInvalid, ErrPaymentUsed, ErrPaymentRequired} {
		if errors.Is(err, refusal) {
			out.Refusal = err // a business rule refuses it (e.g. paused since the order was made)
			return out, nil
		}
	}
	return GatewayApply{}, err
}
