package subscriptions

import (
	"context"
	"errors"
	"testing"
	"time"
)

// refundSetup: owner 10's subscription 101 started `startedDaysAgo` ago, with
// a Razorpay payment (newest) and an older cash payment.
func refundSetup(t *testing.T, startedDaysAgo int, paidAgo time.Duration) (*Service, *fakeGateway, *mockRepo) {
	t.Helper()
	repo := newMock()
	p := repo.seedPlan("GROWTH", 2499, 3999)
	sub := repo.seedSub(101, 10, p.ID, "ACTIVE")
	sub.StartDate = businessDay(time.Now()).AddDate(0, 0, -startedDaysAgo)
	end := sub.StartDate.AddDate(0, 6, -1)
	sub.EndDate = &end
	paid := time.Now().Add(-paidAgo)
	older := time.Now().AddDate(0, -6, 0)
	repo.payments[101] = []Payment{
		{ID: 2, PaymentCode: "PAY-2", Amount: 2499, Status: "SUCCESS", PaymentDate: &paid, Provider: ptr("razorpay"), ProviderPaymentID: ptr("pay_2")},
		{ID: 1, PaymentCode: "PAY-1", Amount: 2499, Status: "SUCCESS", PaymentDate: &older, PaymentMethod: ptr("CASH")},
	}
	s, g := gatewaySvc(repo)
	return s, g, repo
}

// A trial owner's first paid Gold period, refunded: back to the trial plan,
// so no Gold badge without paying. With an earlier payment the plan is kept.
func TestRefund_FirstPaymentGoesBackToTheTrialPlan(t *testing.T) {
	ctx := context.Background()
	s, _, repo := refundSetup(t, -30, time.Hour)
	trial := repo.seedPlan("TRIAL", 0, 0)
	repo.payments[101] = repo.payments[101][:1] // only the Razorpay payment
	if _, err := s.RefundPayment(ctx, adminActor(1), 101, 2, RefundRequest{Reason: "Owner changed mind"}); err != nil {
		t.Fatal(err)
	}
	if repo.lastRefund.PlanID == nil || *repo.lastRefund.PlanID != trial.ID || repo.subs[101].PlanID != trial.ID {
		t.Fatalf("want back on the trial plan %d, got %+v", trial.ID, repo.lastRefund.PlanID)
	}

	s, _, repo = refundSetup(t, -30, time.Hour) // an older cash payment exists
	if _, err := s.RefundPayment(ctx, adminActor(1), 101, 2, RefundRequest{Reason: "Owner changed mind"}); err != nil {
		t.Fatal(err)
	}
	if repo.lastRefund.PlanID != nil {
		t.Fatalf("after earlier payments the plan is kept, got %v", *repo.lastRefund.PlanID)
	}
}

func TestRefund_LatestRazorpayPayment(t *testing.T) {
	ctx := context.Background()

	t.Run("period already running: subscription expires", func(t *testing.T) {
		s, g, repo := refundSetup(t, 2, time.Hour)
		sub, err := s.RefundPayment(ctx, adminActor(1), 101, 2, RefundRequest{Reason: "Charged twice by mistake"})
		if err != nil {
			t.Fatal(err)
		}
		if len(g.refunds) != 1 || g.refunds[0] != "pay_2" {
			t.Fatalf("Razorpay refund not called for pay_2: %v", g.refunds)
		}
		if sub.Status != statusExpired || repo.lastRefund == nil || repo.lastRefund.PaymentID != 2 {
			t.Fatalf("want EXPIRED and payment 2 refunded, got %s %+v", sub.Status, repo.lastRefund)
		}
	})

	t.Run("early renewal: back to the earlier period, still active", func(t *testing.T) {
		s, _, repo := refundSetup(t, -30, time.Hour) // paid period starts in 30 days
		sub, err := s.RefundPayment(ctx, adminActor(1), 101, 2, RefundRequest{Reason: "Customer request"})
		if err != nil {
			t.Fatal(err)
		}
		want := businessDay(time.Now()).AddDate(0, 0, 29)
		if sub.Status != statusActive || !repo.lastRefund.EndDate.Equal(want) {
			t.Fatalf("want ACTIVE until %s, got %s until %s", want, sub.Status, repo.lastRefund.EndDate)
		}
		// Back to the period the older (cash) payment bought: it starts when that was paid.
		older := businessDay(*repo.payments[101][1].PaymentDate)
		if !repo.lastRefund.StartDate.Equal(older) || repo.lastRefund.StartDate.After(repo.lastRefund.EndDate) {
			t.Fatalf("start should roll back to %s, got %s", older, repo.lastRefund.StartDate)
		}
	})
}

func TestRefund_Conditions(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name      string
		actor     ActorContext
		paymentID int64
		reason    string
		paidAgo   time.Duration
		want      error
	}{
		{"owner can't refund", ownerActor(10), 2, "Charged twice", time.Hour, ErrForbidden},
		{"reason required", adminActor(1), 2, "  ", time.Hour, ErrInvalidInput},
		{"after the 7-day window", adminActor(1), 2, "Charged twice", 8 * 24 * time.Hour, RefundError{}},
		{"not the latest / not Razorpay", adminActor(1), 1, "Charged twice", time.Hour, RefundError{}},
		{"unknown payment", adminActor(1), 99, "Charged twice", time.Hour, ErrNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, g, repo := refundSetup(t, 2, c.paidAgo)
			_, err := s.RefundPayment(ctx, c.actor, 101, c.paymentID, RefundRequest{Reason: c.reason})
			var re RefundError
			if _, isRefundErr := c.want.(RefundError); isRefundErr {
				if !errors.As(err, &re) || re.Reason == "" {
					t.Fatalf("want RefundError with a reason, got %v", err)
				}
			} else if !errors.Is(err, c.want) {
				t.Fatalf("want %v, got %v", c.want, err)
			}
			if len(g.refunds) != 0 || repo.lastRefund != nil {
				t.Fatal("a refused refund must not touch Razorpay or the database")
			}
		})
	}

	t.Run("already refunded", func(t *testing.T) {
		s, _, repo := refundSetup(t, 2, time.Hour)
		repo.payments[101][0].Status = "REFUNDED"
		if _, err := s.RefundPayment(ctx, adminActor(1), 101, 2, RefundRequest{Reason: "Again please"}); !errors.As(err, new(RefundError)) {
			t.Fatalf("want RefundError, got %v", err)
		}
	})
	t.Run("mock mode", func(t *testing.T) {
		_, _, repo := refundSetup(t, 2, time.Hour)
		if _, err := svc(repo).RefundPayment(ctx, adminActor(1), 101, 2, RefundRequest{Reason: "Charged twice"}); !errors.Is(err, ErrGatewayDisabled) {
			t.Fatalf("want gateway disabled, got %v", err)
		}
	})
}

func TestRenew_RefusedWhenPaidMoreThanAYearAhead(t *testing.T) {
	repo := newMock()
	p := repo.seedPlan("GROWTH", 2499, 3999)
	sub := repo.seedSub(101, 10, p.ID, "ACTIVE")
	end := time.Now().AddDate(1, 2, 0) // 14 months left
	sub.EndDate = &end
	req := RenewSubscriptionRequest{BillingCycle: "YEARLY", PaymentMethod: ptr("CASH")}
	for _, actor := range []ActorContext{ownerActor(10), adminActor(1)} {
		if _, err := svc(repo).Renew(context.Background(), actor, 101, req); !errors.Is(err, ErrPaidAhead) {
			t.Fatalf("%v: want ErrPaidAhead, got %v", actor.Roles, err)
		}
	}
	if repo.lastPayment != nil {
		t.Fatal("nothing may be recorded")
	}
	s, _ := gatewaySvc(repo)
	if _, err := s.CreatePaymentOrder(context.Background(), ownerActor(10), PaymentOrderRequest{SubscriptionID: ptr(int64(101)), BillingCycle: "YEARLY"}); !errors.Is(err, ErrPaidAhead) {
		t.Fatalf("no money may be taken for a refused renewal, got %v", err)
	}

	// 11 months left: renewing is fine.
	end = time.Now().AddDate(0, 11, 0)
	if _, err := svc(repo).Renew(context.Background(), ownerActor(10), 101, req); err != nil {
		t.Fatalf("renewal within a year should work: %v", err)
	}
}
