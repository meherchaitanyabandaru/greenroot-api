package subscriptions

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// Charged at Razorpay, but the subscription was paused after checkout: the
// money must land in the admin queue, once, not only in a log line.
func TestWebhook_PaidButRefusedBecomesAPaymentIssue(t *testing.T) {
	s, _, repo, orderID := renewSetup(t)
	ctx := context.Background()
	repo.subs[101].Status = statusPaused

	body := webhookBody("payment.captured", orderID, "pay_paused")
	for i := 0; i < 2; i++ { // Razorpay redelivers: still one issue
		if got := s.HandleRazorpayWebhook(ctx, body, sign(body, "wh-secret")); got != http.StatusOK {
			t.Fatalf("want 200 (a retry cannot help), got %d", got)
		}
	}
	if len(repo.recorded) != 0 {
		t.Fatal("a refused payment must not be applied")
	}
	if len(repo.issues) != 1 {
		t.Fatalf("want one payment issue, got %+v", repo.issues)
	}
	is := repo.issues[0]
	if is.IssueType != IssuePaidNotApplied || is.ProviderPaymentID != "pay_paused" || *is.UserID != 10 || *is.Amount != 3999 || is.Reason == "" {
		t.Fatalf("issue: %+v", is)
	}
}

func TestWebhook_FailedRefundBecomesAPaymentIssue(t *testing.T) {
	s, _, repo, _ := renewSetup(t)
	body := []byte(`{"event":"refund.failed","payload":{"refund":{"entity":{"id":"rfnd_9","payment_id":"pay_9","status":"failed"}}}}`)
	if got := s.HandleRazorpayWebhook(context.Background(), body, sign(body, "wh-secret")); got != http.StatusOK {
		t.Fatalf("got %d", got)
	}
	if len(repo.issues) != 1 || repo.issues[0].IssueType != IssueRefundFailed || *repo.issues[0].ProviderRefundID != "rfnd_9" {
		t.Fatalf("issues: %+v", repo.issues)
	}
}

func TestPaymentIssues_AdminOnlyListAndResolve(t *testing.T) {
	s, _, repo, _ := renewSetup(t)
	ctx := context.Background()
	_ = repo.RecordPaymentIssue(ctx, PaymentIssue{IssueType: IssuePaidNotApplied, ProviderPaymentID: "pay_1", Reason: "paused"})

	if _, err := s.ListPaymentIssues(ctx, ownerActor(10), ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("owner list: %v", err)
	}
	if _, err := s.ResolvePaymentIssue(ctx, ownerActor(10), 1, ResolvePaymentIssueRequest{Note: "refunded"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("owner resolve: %v", err)
	}
	if _, err := s.ResolvePaymentIssue(ctx, adminActor(1), 1, ResolvePaymentIssueRequest{Note: "ok"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a resolution needs a real note: %v", err)
	}
	open, err := s.ListPaymentIssues(ctx, adminActor(1), "open")
	if err != nil || len(open) != 1 {
		t.Fatalf("admin open list: %v %v", open, err)
	}
	got, err := s.ResolvePaymentIssue(ctx, adminActor(1), 1, ResolvePaymentIssueRequest{Note: "Refunded in the Razorpay dashboard, rfnd_42"})
	if err != nil || got.Status != IssueResolved || *got.ResolvedByUserID != 1 {
		t.Fatalf("resolve: %+v %v", got, err)
	}
	if _, err := s.ResolvePaymentIssue(ctx, adminActor(1), 1, ResolvePaymentIssueRequest{Note: "again, twice"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("resolving twice: %v", err)
	}
	if open, _ := s.ListPaymentIssues(ctx, adminActor(1), "OPEN"); len(open) != 0 {
		t.Fatalf("still open: %v", open)
	}
}
