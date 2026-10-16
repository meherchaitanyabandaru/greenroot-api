package payments

import (
	"context"
	"testing"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/sms"
)

type sentSMS struct {
	userID int64
	req    sms.SendRequest
}

type fakeSMS struct{ sent chan sentSMS }

func newFakeSMS() *fakeSMS { return &fakeSMS{sent: make(chan sentSMS, 4)} }

func (f *fakeSMS) SendToUser(_ context.Context, userID int64, req sms.SendRequest) (sms.SendResult, error) {
	f.sent <- sentSMS{userID: userID, req: req}
	return sms.SendResult{Status: "MOCKED"}, nil
}

func (f *fakeSMS) expect(t *testing.T) sentSMS {
	t.Helper()
	select {
	case s := <-f.sent:
		return s
	case <-time.After(2 * time.Second):
		t.Fatal("expected a payment SMS")
		return sentSMS{}
	}
}

func (f *fakeSMS) expectNone(t *testing.T) {
	t.Helper()
	select {
	case s := <-f.sent:
		t.Fatalf("unexpected SMS %+v", s.req)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestPaymentSMS_Templates(t *testing.T) {
	payer := int64(20)
	order := Payment{ID: 1, PaymentCode: "PAY-20261016-0012", PaymentFor: paymentForOrder, OrderCode: ptr("ORD-20261015-0008"), PayerUserID: &payer, Amount: 1250.5, Status: "SUCCESS"}

	req, ok := paymentSMS(order)
	if !ok || req.TemplateCode != sms.TemplatePaymentReceivedOrder {
		t.Fatalf("order success: %+v ok=%v", req, ok)
	}
	want := map[string]string{"amount_rupees": "1250", "amount_paise": "50", "order_date": "20261015", "order_seq": "0008", "payment_date": "20261016", "payment_seq": "0012"}
	for k, v := range want {
		if req.Values[k] != v {
			t.Errorf("%s = %q, want %q", k, req.Values[k], v)
		}
	}

	sub := order
	sub.PaymentFor, sub.OrderCode = paymentForSubscription, nil
	if req, ok := paymentSMS(sub); !ok || req.TemplateCode != sms.TemplatePaymentReceivedSubscription {
		t.Fatalf("subscription success: %+v", req)
	}

	failed := order
	failed.Status = "FAILED"
	if req, ok := paymentSMS(failed); !ok || req.TemplateCode != sms.TemplatePaymentFailed {
		t.Fatalf("failed: %+v", req)
	}

	for name, p := range map[string]Payment{
		"pending":  {PaymentCode: order.PaymentCode, PaymentFor: paymentForOrder, OrderCode: order.OrderCode, PayerUserID: &payer, Status: "PENDING"},
		"refunded": {PaymentCode: order.PaymentCode, PaymentFor: paymentForOrder, OrderCode: order.OrderCode, PayerUserID: &payer, Status: "REFUNDED"},
		"no payer": {PaymentCode: order.PaymentCode, PaymentFor: paymentForOrder, OrderCode: order.OrderCode, Status: "SUCCESS"},
		"no code":  {PaymentFor: paymentForOrder, OrderCode: order.OrderCode, PayerUserID: &payer, Status: "SUCCESS"},
	} {
		if _, ok := paymentSMS(p); ok {
			t.Errorf("%s: no SMS expected", name)
		}
	}
}

func TestUpdateStatus_SendsSMSOnlyOnTransition(t *testing.T) {
	repo := newMock()
	orderID, payerID := int64(10), int64(20)
	repo.payments[1] = &Payment{ID: 1, PaymentCode: "PAY-20261016-0001", PaymentFor: "ORDER", OrderID: &orderID, OrderCode: ptr("ORD-20261016-0001"), PayerUserID: &payerID, Amount: 500, Status: "PENDING"}
	repo.seedOrder(10, &payerID, nil)
	fake := newFakeSMS()
	s := NewService(repo, nil).WithSMS(fake)

	if _, err := s.UpdateStatus(context.Background(), adminActor(1), 1, UpdateStatusRequest{Status: "SUCCESS"}); err != nil {
		t.Fatal(err)
	}
	if got := fake.expect(t); got.userID != payerID || got.req.TemplateCode != sms.TemplatePaymentReceivedOrder {
		t.Fatalf("sms = %+v", got)
	}

	// Re-saving SUCCESS (e.g. adding a reference) must not re-send.
	if _, err := s.UpdateStatus(context.Background(), adminActor(1), 1, UpdateStatusRequest{Status: "SUCCESS"}); err != nil {
		t.Fatal(err)
	}
	fake.expectNone(t)
}
