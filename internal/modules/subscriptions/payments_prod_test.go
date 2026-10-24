package subscriptions

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/meherchaitanyabandaru/greenroot-api/platform/razorpay"
)

// renewSetup: owner 10's ACTIVE subscription 101 and a Razorpay order to renew it yearly.
func renewSetup(t *testing.T) (*Service, *fakeGateway, *mockRepo, string) {
	t.Helper()
	repo := newMock()
	p := repo.seedPlan("GROWTH", 2499, 3999)
	repo.seedSub(101, 10, p.ID, "ACTIVE")
	s, g := gatewaySvc(repo)
	order, err := s.CreatePaymentOrder(context.Background(), ownerActor(10), PaymentOrderRequest{SubscriptionID: ptr(int64(101)), BillingCycle: "YEARLY"})
	if err != nil {
		t.Fatal(err)
	}
	return s, g, repo, order.OrderID
}

func TestCheckout_AuthorizedPaymentIsCapturedFirst(t *testing.T) {
	s, g, repo, orderID := renewSetup(t)
	g.payments["pay_1"] = razorpay.Payment{ID: "pay_1", OrderID: orderID, Amount: 399900, Status: "authorized", Method: "card"}
	if _, err := s.Renew(context.Background(), ownerActor(10), 101, paidRenew(orderID, "pay_1")); err != nil {
		t.Fatal(err)
	}
	if len(g.captures) != 1 || g.captures[0] != "pay_1" {
		t.Fatalf("authorized payment must be captured, captures=%v", g.captures)
	}
	if repo.lastPayment.Method != "CARD" {
		t.Fatalf("method should come from Razorpay (card), got %s", repo.lastPayment.Method)
	}
}

func TestCheckout_FailedOrForeignPaymentRefused(t *testing.T) {
	for name, p := range map[string]razorpay.Payment{
		"failed":            {Status: "failed"},
		"refunded":          {Status: "refunded"},
		"for another order": {Status: "captured", OrderID: "order_other"},
		"wrong amount":      {Status: "captured", Amount: 100},
	} {
		t.Run(name, func(t *testing.T) {
			s, g, repo, orderID := renewSetup(t)
			p.ID = "pay_1"
			if p.OrderID == "" {
				p.OrderID = orderID
			}
			if p.Amount == 0 {
				p.Amount = 399900
			}
			g.payments["pay_1"] = p
			if _, err := s.Renew(context.Background(), ownerActor(10), 101, paidRenew(orderID, "pay_1")); !errors.Is(err, ErrPaymentInvalid) {
				t.Fatalf("want ErrPaymentInvalid, got %v", err)
			}
			if repo.lastPayment != nil || len(g.captures) != 0 {
				t.Fatal("nothing may be recorded or captured")
			}
		})
	}
}

func TestCheckout_RetryIsIdempotent(t *testing.T) {
	s, _, repo, orderID := renewSetup(t)
	ctx := context.Background()
	first, err := s.Renew(ctx, ownerActor(10), 101, paidRenew(orderID, "pay_1"))
	if err != nil {
		t.Fatal(err)
	}
	// The app's confirm timed out and it sends the same result again.
	again, err := s.Renew(ctx, ownerActor(10), 101, paidRenew(orderID, "pay_1"))
	if err != nil {
		t.Fatalf("retry must succeed, got %v", err)
	}
	if len(repo.recorded) != 1 || !again.EndDate.Equal(*first.EndDate) {
		t.Fatalf("retry must not pay or extend twice: %d payments, end %v vs %v", len(repo.recorded), again.EndDate, first.EndDate)
	}
}

func webhookBody(event, orderID, paymentID string) []byte {
	return []byte(fmt.Sprintf(`{"event":%q,"payload":{"payment":{"entity":{"id":%q,"order_id":%q}}}}`, event, paymentID, orderID))
}

func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestWebhook_AppliesPaymentTheAppNeverConfirmed(t *testing.T) {
	s, _, repo, orderID := renewSetup(t)
	ctx := context.Background()
	body := webhookBody("payment.captured", orderID, "pay_w")

	if got := s.HandleRazorpayWebhook(ctx, body, sign(body, "wrong")); got != http.StatusBadRequest {
		t.Fatalf("forged webhook: want 400, got %d", got)
	}
	if got := s.HandleRazorpayWebhook(ctx, body, sign(body, "wh-secret")); got != http.StatusOK {
		t.Fatalf("want 200, got %d", got)
	}
	if len(repo.recorded) != 1 || *repo.recorded[0].ProviderPaymentID != "pay_w" || repo.recorded[0].Status != "SUCCESS" {
		t.Fatalf("payment not applied: %+v", repo.recorded)
	}
	// Razorpay redelivers, and order.paid follows: nothing more happens.
	paid := webhookBody("order.paid", orderID, "pay_w")
	for _, b := range [][]byte{body, paid} {
		if got := s.HandleRazorpayWebhook(ctx, b, sign(b, "wh-secret")); got != http.StatusOK || len(repo.recorded) != 1 {
			t.Fatalf("duplicate event: status %d, %d payments", got, len(repo.recorded))
		}
	}
	// And the app confirming late gets success, not "already used".
	if _, err := s.Renew(ctx, ownerActor(10), 101, paidRenew(orderID, "pay_w")); err != nil {
		t.Fatalf("late app confirm: %v", err)
	}
}

func TestWebhook_NewSubscription(t *testing.T) {
	repo := newMock()
	p := repo.seedPlan("GROWTH", 2499, 3999)
	s, _ := gatewaySvc(repo)
	ctx := context.Background()
	order, err := s.CreatePaymentOrder(ctx, ownerActor(10), PaymentOrderRequest{PlanID: ptr(p.ID), BillingCycle: "SIX_MONTH"})
	if err != nil {
		t.Fatal(err)
	}
	body := webhookBody("payment.captured", order.OrderID, "pay_n")
	if got := s.HandleRazorpayWebhook(ctx, body, sign(body, "wh-secret")); got != http.StatusOK {
		t.Fatalf("want 200, got %d", got)
	}
	if len(repo.recorded) != 1 || repo.recorded[0].PayerUserID != 10 {
		t.Fatalf("subscription not created for user 10: %+v", repo.recorded)
	}
}

func TestWebhook_IgnoresForeignOrdersAndTracksRefunds(t *testing.T) {
	s, g, repo, _ := renewSetup(t)
	ctx := context.Background()
	g.orders["order_x"] = razorpay.Order{ID: "order_x", Amount: 500} // not ours: no notes
	body := webhookBody("payment.captured", "order_x", "pay_x")
	if got := s.HandleRazorpayWebhook(ctx, body, sign(body, "wh-secret")); got != http.StatusOK || len(repo.recorded) != 0 {
		t.Fatalf("foreign order: status %d, %d payments", got, len(repo.recorded))
	}
	refund := []byte(`{"event":"refund.processed","payload":{"refund":{"entity":{"id":"rfnd_1","payment_id":"pay_1","status":"processed"}}}}`)
	if got := s.HandleRazorpayWebhook(ctx, refund, sign(refund, "wh-secret")); got != http.StatusOK || repo.refundStatus["rfnd_1"] != "processed" {
		t.Fatalf("refund status not recorded: %d %v", got, repo.refundStatus)
	}
	// Razorpay rejects the lookup (4xx): acknowledged, not retried for days.
	bad := webhookBody("payment.captured", "order_invalid", "pay_bad")
	if got := s.HandleRazorpayWebhook(ctx, bad, sign(bad, "wh-secret")); got != http.StatusOK || len(repo.recorded) != 0 {
		t.Fatalf("rejected lookup: status %d, %d payments; want 200 and nothing recorded", got, len(repo.recorded))
	}
	// Razorpay unreachable (gateway error): 500 so Razorpay retries.
	down := webhookBody("payment.captured", "order_unknown", "pay_down")
	if got := s.HandleRazorpayWebhook(ctx, down, sign(down, "wh-secret")); got != http.StatusInternalServerError {
		t.Fatalf("gateway error: status %d, want 500 (retry)", got)
	}
	// Without a gateway (mock mode) the webhook is off.
	if got := svc(repo).HandleRazorpayWebhook(ctx, refund, sign(refund, "wh-secret")); got != http.StatusServiceUnavailable {
		t.Fatalf("mock mode: want 503, got %d", got)
	}
}
