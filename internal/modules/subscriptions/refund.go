package subscriptions

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/meherchaitanyabandaru/greenroot-api/platform/razorpay"
)

// Refund policy (admin only, full refunds):
//   - a successful Razorpay payment (cash/bank payments are refunded outside the app);
//   - the subscription's latest successful payment, so the period to undo is unambiguous;
//   - within RefundWindow of payment.
//
// The paid period is removed: end_date goes back to the day before that
// period started; if that day has passed the subscription expires. When the
// refunded payment was the only one, the time before it was the free trial, so
// the trial plan comes back (a refunded first Gold month must not leave a Gold
// badge). After earlier payments the plan is kept (it is not recorded per
// payment), so an upgrade refunded mid-period keeps the new plan until then.
const RefundWindow = 7 * 24 * time.Hour

// RefundError says why a payment can't be refunded; Reason is shown to the admin.
type RefundError struct{ Reason string }

func (e RefundError) Error() string { return "refund not allowed: " + e.Reason }

type RefundRequest struct {
	Reason string `json:"reason"`
}

type ApplyRefundInput struct {
	SubscriptionID int64
	PaymentID      int64
	Note           string
	Raw            []byte // Razorpay refund JSON, kept in payments.raw_response
	StartDate      time.Time
	EndDate        time.Time
	Status         string
	PlanID         *int64 // set: the plan to go back to
}

// refundable returns why p can't be refunded, or "" when it can.
func refundable(p Payment, latestSuccessID int64, now time.Time) string {
	switch {
	case p.Status != "SUCCESS":
		return fmt.Sprintf("only successful payments can be refunded (this one is %s)", p.Status)
	case !strings.EqualFold(stringOrEmpty(p.Provider), providerRazorpay) || stringOrEmpty(p.ProviderPaymentID) == "":
		return "only Razorpay payments can be refunded here; refund cash or bank payments outside the app"
	case p.ID != latestSuccessID:
		return "only the latest payment can be refunded"
	case p.PaymentDate == nil || now.Sub(*p.PaymentDate) > RefundWindow:
		return fmt.Sprintf("the %d-day refund window has passed", int(RefundWindow.Hours()/24))
	}
	return ""
}

// RefundPayment refunds a subscription payment in full through Razorpay and
// removes the period it paid for.
func (s *Service) RefundPayment(ctx context.Context, actor ActorContext, subscriptionID, paymentID int64, req RefundRequest) (UserSubscription, error) {
	if !actor.HasRole("ADMIN") {
		return UserSubscription{}, ErrForbidden
	}
	reason := strings.TrimSpace(req.Reason)
	if len(reason) < 5 {
		return UserSubscription{}, ErrInvalidInput
	}
	if s.gateway == nil {
		return UserSubscription{}, ErrGatewayDisabled
	}
	lock, err := redisutil.AcquireLock(ctx, s.redis, nil, "subscriptions", subscriptionID)
	if err != nil {
		return UserSubscription{}, err
	}
	defer lock.Release(ctx)

	current, err := s.repository.FindByID(ctx, subscriptionID)
	if err != nil {
		return UserSubscription{}, err
	}
	payments, err := s.repository.ListPaymentsBySubscription(ctx, subscriptionID)
	if err != nil {
		return UserSubscription{}, err
	}
	var target, previous *Payment // previous: the success before the latest one
	var latestSuccessID int64
	for i := range payments { // newest first
		if payments[i].Status == "SUCCESS" {
			if latestSuccessID == 0 {
				latestSuccessID = payments[i].ID
			} else if previous == nil {
				previous = &payments[i]
			}
		}
		if payments[i].ID == paymentID {
			target = &payments[i]
		}
	}
	if target == nil {
		return UserSubscription{}, ErrNotFound
	}
	if why := refundable(*target, latestSuccessID, time.Now()); why != "" {
		return UserSubscription{}, RefundError{Reason: why}
	}

	refund, err := s.gateway.RefundPayment(ctx, *target.ProviderPaymentID, toPaise(target.Amount), razorpay.Notes{
		"subscription_id": strconv.FormatInt(subscriptionID, 10),
		"payment_code":    target.PaymentCode,
		"refunded_by":     strconv.FormatInt(actor.UserID, 10),
		"reason":          reason,
	})
	if err != nil {
		return UserSubscription{}, err
	}

	today := businessDay(time.Now())
	endDate := current.StartDate.AddDate(0, 0, -1)
	// The earlier period's start isn't stored (renew overwrites it): use when
	// it was paid for, or when the subscription began; never after the end.
	startDate := businessDay(current.CreatedAt)
	if previous != nil && previous.PaymentDate != nil {
		startDate = businessDay(*previous.PaymentDate)
	}
	if startDate.After(endDate) {
		startDate = endDate
	}
	var planID *int64
	if previous == nil { // nothing paid before: back to the free trial
		if trial, err := s.repository.FindPlanByCode(ctx, "TRIAL"); err == nil {
			planID = &trial.ID
		}
	}
	status := current.Status
	if endDate.Before(today) && status == statusActive {
		status = statusExpired
	}
	raw, _ := json.Marshal(map[string]any{"refund": refund, "reason": reason, "refunded_by": actor.UserID})
	if err := s.repository.ApplyRefund(ctx, ApplyRefundInput{
		SubscriptionID: subscriptionID,
		PaymentID:      paymentID,
		Note:           fmt.Sprintf("Refunded %s: %s", refund.ID, reason),
		Raw:            raw,
		StartDate:      startDate,
		EndDate:        endDate,
		Status:         status,
		PlanID:         planID,
	}); err != nil {
		// Money already went back; make the mismatch easy to find and fix by hand.
		slog.Error("refund done at Razorpay but not recorded", "subscription_id", subscriptionID, "payment_id", paymentID, "refund_id", refund.ID, "error", err)
		return UserSubscription{}, err
	}
	subscription, err := s.repository.FindByID(ctx, subscriptionID)
	if err != nil {
		return UserSubscription{}, err
	}
	s.scheduleSubscriptionExpiry(ctx, subscription)
	s.claimsChanged(ctx, subscription.UserID)
	s.audit(ctx, actor, subscriptionID, actionUpdate, map[string]any{
		"refund_id": refund.ID, "payment_code": target.PaymentCode, "amount": target.Amount,
		"reason": reason, "end_date": endDate.Format(time.DateOnly), "subscription_status": status,
	})
	return enrichSubscription(actor, *subscription), nil
}
