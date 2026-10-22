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
//     ids: the signature and the order are verified BEFORE anything is extended.

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
	RefundPayment(ctx context.Context, paymentID string, amountPaise int64, notes razorpay.Notes) (razorpay.Refund, error)
}

// SetGateway turns on real payments: owners must then pay through Razorpay.
func (s *Service) SetGateway(g PaymentGateway) { s.gateway = g }

// verifiedPayment is a checkout result whose signature and order checked out.
type verifiedPayment struct {
	OrderID, PaymentID, Signature string
}

func toPaise(amount float64) int64 { return int64(math.Round(amount * 100)) }

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
	order, err := s.gateway.CreateOrder(ctx, paise, receipt, orderNotes(userID, subscription, planID, cycle))
	if err != nil {
		return PaymentOrder{}, err
	}
	return PaymentOrder{OrderID: order.ID, Amount: order.Amount, Currency: order.Currency, KeyID: s.gateway.KeyID()}, nil
}

// checkPayment decides whether a create/renew may go ahead. nil, nil = no
// gateway payment (mock mode, admin recording a manual payment, or nothing to pay).
func (s *Service) checkPayment(ctx context.Context, actor ActorContext, userID int64, subscription string, planID int64, cycle string, amount float64, provider, orderID, paymentID, signature *string) (*verifiedPayment, error) {
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
	if p.OrderID == "" || p.PaymentID == "" || p.Signature == "" {
		return nil, ErrInvalidInput
	}
	if !s.gateway.VerifySignature(p.OrderID, p.PaymentID, p.Signature) {
		return nil, ErrPaymentInvalid
	}
	// A valid signature proves payment for that order; the order proves what for.
	order, err := s.gateway.FetchOrder(ctx, p.OrderID)
	if err != nil {
		return nil, err
	}
	want := orderNotes(userID, subscription, planID, cycle)
	for k, v := range want {
		if order.Notes[k] != v {
			return nil, ErrPaymentInvalid
		}
	}
	if order.Amount != toPaise(amount) {
		return nil, ErrPaymentInvalid
	}
	used, err := s.repository.GatewayPaymentUsed(ctx, providerRazorpay, p.OrderID, p.PaymentID)
	if err != nil {
		return nil, err
	}
	if used {
		return nil, ErrPaymentUsed
	}
	return &p, nil
}
