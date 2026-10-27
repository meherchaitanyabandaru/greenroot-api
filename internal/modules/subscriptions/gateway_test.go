package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/meherchaitanyabandaru/greenroot-api/platform/razorpay"
)

const testSecret = "test-secret"

// fakeGateway is Razorpay in memory: orders it created, real HMAC signatures.
type fakeGateway struct {
	orders    map[string]razorpay.Order
	lastOrder string
	payments  map[string]razorpay.Payment // explicit ones; others: captured, for the last order
	captures  []string
	refunds   []string
}

func newFakeGateway() *fakeGateway {
	return &fakeGateway{orders: map[string]razorpay.Order{}, payments: map[string]razorpay.Payment{}}
}

func (g *fakeGateway) FetchPayment(_ context.Context, id string) (razorpay.Payment, error) {
	if p, ok := g.payments[id]; ok {
		return p, nil
	}
	o := g.orders[g.lastOrder]
	return razorpay.Payment{ID: id, OrderID: o.ID, Amount: o.Amount, Status: "captured", Method: "upi"}, nil
}

func (g *fakeGateway) CapturePayment(_ context.Context, id string, paise int64) (razorpay.Payment, error) {
	g.captures = append(g.captures, id)
	p := g.payments[id]
	p.Status = "captured"
	g.payments[id] = p
	return p, nil
}

func (g *fakeGateway) KeyID() string { return "rzp_test_key" }

func (g *fakeGateway) CreateOrder(_ context.Context, paise int64, receipt string, notes razorpay.Notes) (razorpay.Order, error) {
	o := razorpay.Order{ID: fmt.Sprintf("order_%d", len(g.orders)+1), Amount: paise, Currency: "INR", Receipt: receipt, Status: "created", Notes: notes}
	g.orders[o.ID] = o
	g.lastOrder = o.ID
	return o, nil
}

func (g *fakeGateway) FetchOrder(_ context.Context, id string) (razorpay.Order, error) {
	o, ok := g.orders[id]
	if id == "order_invalid" { // Razorpay answers 4xx: "... is not a valid id"
		return razorpay.Order{}, fmt.Errorf("%w: invalid is not a valid id", razorpay.ErrRejected)
	}
	if !ok {
		return razorpay.Order{}, razorpay.ErrGateway
	}
	return o, nil
}

func (g *fakeGateway) VerifySignature(orderID, paymentID, sig string) bool {
	return razorpay.Signature(testSecret, orderID, paymentID) == sig
}

func (g *fakeGateway) RefundPayment(_ context.Context, paymentID string, paise int64, _ razorpay.Notes) (razorpay.Refund, error) {
	g.refunds = append(g.refunds, paymentID)
	return razorpay.Refund{ID: "rfnd_" + paymentID, PaymentID: paymentID, Amount: paise, Status: "processed"}, nil
}

func gatewaySvc(repo *mockRepo) (*Service, *fakeGateway) {
	g := newFakeGateway()
	s := svc(repo)
	s.SetGateway(g, "wh-secret")
	return s, g
}

// paidRenew is what the app sends after checkout succeeds.
func paidRenew(orderID, paymentID string) RenewSubscriptionRequest {
	return RenewSubscriptionRequest{
		BillingCycle:      "YEARLY",
		PaymentMethod:     ptr("UPI"),
		Provider:          ptr("razorpay"),
		ProviderRef:       ptr(orderID),
		ProviderPaymentID: ptr(paymentID),
		ProviderSignature: ptr(razorpay.Signature(testSecret, orderID, paymentID)),
	}
}

func TestPaymentOrder_PricedByServer(t *testing.T) {
	repo := newMock()
	p := repo.seedPlan("STANDARD", 1000, 10000)
	repo.seedSub(101, 10, p.ID, "ACTIVE")
	s, _ := gatewaySvc(repo)

	order, err := s.CreatePaymentOrder(context.Background(), ownerActor(10), PaymentOrderRequest{SubscriptionID: ptr(int64(101)), BillingCycle: "YEARLY"})
	if err != nil {
		t.Fatal(err)
	}
	if order.Amount != 1_203_600 || order.KeyID != "rzp_test_key" || order.OrderID == "" {
		t.Fatalf("got %+v, want ₹1000 × 12 − 15%% = ₹10,200 + 18%% GST = ₹12,036 = 1203600 paise", order)
	}
	// Someone else's subscription.
	if _, err := s.CreatePaymentOrder(context.Background(), ownerActor(11), PaymentOrderRequest{SubscriptionID: ptr(int64(101)), BillingCycle: "YEARLY"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("want forbidden, got %v", err)
	}
	// Mock mode: no gateway.
	if _, err := svc(repo).CreatePaymentOrder(context.Background(), ownerActor(10), PaymentOrderRequest{SubscriptionID: ptr(int64(101)), BillingCycle: "YEARLY"}); !errors.Is(err, ErrGatewayDisabled) {
		t.Fatalf("want gateway disabled, got %v", err)
	}
}

func TestPaymentOrder_BelowMinimum(t *testing.T) {
	repo := newMock()
	p := repo.seedPlan("STANDARD", 0.5, 0.5) // 50 paise
	s, _ := gatewaySvc(repo)
	if _, err := s.CreatePaymentOrder(context.Background(), ownerActor(10), PaymentOrderRequest{PlanID: ptr(p.ID), BillingCycle: "MONTHLY"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("want invalid input under 100 paise, got %v", err)
	}
}

func TestRenew_VerifiedRazorpayPayment(t *testing.T) {
	repo := newMock()
	p := repo.seedPlan("STANDARD", 1000, 10000)
	repo.seedSub(101, 10, p.ID, "ACTIVE")
	s, _ := gatewaySvc(repo)
	ctx := context.Background()
	order, err := s.CreatePaymentOrder(ctx, ownerActor(10), PaymentOrderRequest{SubscriptionID: ptr(int64(101)), BillingCycle: "YEARLY"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.Renew(ctx, ownerActor(10), 101, paidRenew(order.OrderID, "pay_1")); err != nil {
		t.Fatalf("verified payment rejected: %v", err)
	}
	got := repo.lastPayment
	if got == nil || got.Status != "SUCCESS" || *got.Provider != "razorpay" || *got.ProviderPaymentID != "pay_1" || *got.ProviderOrderID != order.OrderID {
		t.Fatalf("payment not recorded as verified: %+v", got)
	}
}

func TestRenew_RejectsBadPayments(t *testing.T) {
	ctx := context.Background()
	setup := func() (*Service, *mockRepo, string) {
		repo := newMock()
		p := repo.seedPlan("STANDARD", 1000, 10000)
		repo.seedSub(101, 10, p.ID, "ACTIVE")
		s, _ := gatewaySvc(repo)
		order, err := s.CreatePaymentOrder(ctx, ownerActor(10), PaymentOrderRequest{SubscriptionID: ptr(int64(101)), BillingCycle: "YEARLY"})
		if err != nil {
			t.Fatal(err)
		}
		return s, repo, order.OrderID
	}

	t.Run("tampered signature", func(t *testing.T) {
		s, _, orderID := setup()
		req := paidRenew(orderID, "pay_1")
		req.ProviderSignature = ptr("deadbeef")
		if _, err := s.Renew(ctx, ownerActor(10), 101, req); !errors.Is(err, ErrPaymentInvalid) {
			t.Fatalf("want verification failure, got %v", err)
		}
	})
	t.Run("missing fields", func(t *testing.T) {
		s, _, orderID := setup()
		req := paidRenew(orderID, "pay_1")
		req.ProviderPaymentID = nil
		if _, err := s.Renew(ctx, ownerActor(10), 101, req); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("want invalid input, got %v", err)
		}
	})
	t.Run("paid for a cheaper cycle", func(t *testing.T) {
		s, _, _ := setup()
		cheap, _ := s.CreatePaymentOrder(ctx, ownerActor(10), PaymentOrderRequest{SubscriptionID: ptr(int64(101)), BillingCycle: "SIX_MONTH"})
		if _, err := s.Renew(ctx, ownerActor(10), 101, paidRenew(cheap.OrderID, "pay_2")); !errors.Is(err, ErrPaymentInvalid) {
			t.Fatalf("six-month order must not renew a year, got %v", err)
		}
	})
	t.Run("replayed payment", func(t *testing.T) {
		s, repo, orderID := setup()
		repo.seedSub(202, 11, repo.subs[101].PlanID, "ACTIVE") // another owner's
		repo.gatewayUsedSub = 202
		if _, err := s.Renew(ctx, ownerActor(10), 101, paidRenew(orderID, "pay_1")); !errors.Is(err, ErrPaymentUsed) {
			t.Fatalf("want already used, got %v", err)
		}
	})
	t.Run("owner skips payment", func(t *testing.T) {
		s, repo, _ := setup()
		req := RenewSubscriptionRequest{BillingCycle: "YEARLY", PaymentMethod: ptr("UPI"), Provider: ptr("razorpay_mock"), ProviderRef: ptr("MOCK-1")}
		if _, err := s.Renew(ctx, ownerActor(10), 101, req); !errors.Is(err, ErrPaymentRequired) {
			t.Fatalf("want payment required, got %v", err)
		}
		if repo.lastPayment != nil {
			t.Fatal("nothing may be recorded for an unpaid renewal")
		}
	})
	t.Run("admin records a manual payment", func(t *testing.T) {
		s, _, _ := setup()
		req := RenewSubscriptionRequest{BillingCycle: "YEARLY", PaymentMethod: ptr("CASH")}
		if _, err := s.Renew(ctx, adminActor(1), 101, req); err != nil {
			t.Fatalf("admin manual renewal: %v", err)
		}
	})
}

func TestCreate_VerifiedRazorpayPayment(t *testing.T) {
	repo := newMock()
	p := repo.seedPlan("STANDARD", 1000, 10000)
	s, _ := gatewaySvc(repo)
	ctx := context.Background()
	order, err := s.CreatePaymentOrder(ctx, ownerActor(10), PaymentOrderRequest{PlanID: ptr(p.ID), BillingCycle: "YEARLY"})
	if err != nil {
		t.Fatal(err)
	}
	// Another owner can't use it.
	other := CreateSubscriptionRequest{PlanID: p.ID, BillingCycle: "YEARLY", Provider: ptr("razorpay"), ProviderRef: ptr(order.OrderID), ProviderPaymentID: ptr("pay_9"), ProviderSignature: ptr(razorpay.Signature(testSecret, order.OrderID, "pay_9"))}
	if _, err := s.Create(ctx, ownerActor(11), other); !errors.Is(err, ErrPaymentInvalid) {
		t.Fatalf("order for user 10 used by 11: got %v", err)
	}
	if _, err := s.Create(ctx, ownerActor(10), other); err != nil {
		t.Fatalf("own verified payment rejected: %v", err)
	}
}
