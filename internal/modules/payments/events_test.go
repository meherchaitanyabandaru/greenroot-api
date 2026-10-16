package payments

import (
	"context"
	"testing"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/messaging"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/sms"
)

type fakeEvents struct{ sent chan messaging.Event }

func newFakeEvents() *fakeEvents { return &fakeEvents{sent: make(chan messaging.Event, 4)} }

func (f *fakeEvents) NotifyAsync(ev messaging.Event) { f.sent <- ev }

func (f *fakeEvents) expect(t *testing.T) messaging.Event {
	t.Helper()
	select {
	case ev := <-f.sent:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("expected a payment event")
		return messaging.Event{}
	}
}

func (f *fakeEvents) expectNone(t *testing.T) {
	t.Helper()
	select {
	case ev := <-f.sent:
		t.Fatalf("unexpected event %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestPaymentEvent_Codes(t *testing.T) {
	payer := int64(20)
	order := Payment{ID: 1, PaymentCode: "PAY-20261016-0012", PaymentFor: paymentForOrder, OrderCode: ptr("ORD-20261015-0008"), PayerUserID: &payer, Amount: 1250.5, Status: "SUCCESS"}

	ev, ok := paymentEvent(order)
	if !ok || ev.Code != sms.TemplatePaymentReceivedOrder || ev.UserID != payer {
		t.Fatalf("order success: %+v ok=%v", ev, ok)
	}
	if ev.Values["amount"] != "1250.50" || ev.Values["order_code"] != "ORD-20261015-0008" || ev.Values["payment_code"] != "PAY-20261016-0012" {
		t.Fatalf("values = %v", ev.Values)
	}
	// The expanded values must cover every variable the SMS template declares.
	expanded := messaging.ExpandValues(ev.Values)
	for _, name := range []string{"amount_rupees", "amount_paise", "order_date", "order_seq", "payment_date", "payment_seq"} {
		if expanded[name] == "" {
			t.Errorf("expanded values missing %s: %v", name, expanded)
		}
	}

	sub := order
	sub.PaymentFor, sub.OrderCode = paymentForSubscription, nil
	if ev, ok := paymentEvent(sub); !ok || ev.Code != sms.TemplatePaymentReceivedSubscription {
		t.Fatalf("subscription success: %+v", ev)
	}
	failed := order
	failed.Status = "FAILED"
	if ev, ok := paymentEvent(failed); !ok || ev.Code != sms.TemplatePaymentFailed {
		t.Fatalf("failed: %+v", ev)
	}

	for name, p := range map[string]Payment{
		"pending":  {PaymentCode: order.PaymentCode, PaymentFor: paymentForOrder, OrderCode: order.OrderCode, PayerUserID: &payer, Status: "PENDING"},
		"refunded": {PaymentCode: order.PaymentCode, PaymentFor: paymentForOrder, OrderCode: order.OrderCode, PayerUserID: &payer, Status: "REFUNDED"},
		"no payer": {PaymentCode: order.PaymentCode, PaymentFor: paymentForOrder, OrderCode: order.OrderCode, Status: "SUCCESS"},
		"no code":  {PaymentFor: paymentForOrder, OrderCode: order.OrderCode, PayerUserID: &payer, Status: "SUCCESS"},
	} {
		if _, ok := paymentEvent(p); ok {
			t.Errorf("%s: no event expected", name)
		}
	}
}

func TestUpdateStatus_RaisesEventOnlyOnTransition(t *testing.T) {
	repo := newMock()
	orderID, payerID := int64(10), int64(20)
	repo.payments[1] = &Payment{ID: 1, PaymentCode: "PAY-20261016-0001", PaymentFor: "ORDER", OrderID: &orderID, OrderCode: ptr("ORD-20261016-0001"), PayerUserID: &payerID, Amount: 500, Status: "PENDING"}
	repo.seedOrder(10, &payerID, nil)
	events := newFakeEvents()
	s := NewService(repo, nil).WithEvents(events)

	if _, err := s.UpdateStatus(context.Background(), adminActor(1), 1, UpdateStatusRequest{Status: "SUCCESS"}); err != nil {
		t.Fatal(err)
	}
	if ev := events.expect(t); ev.UserID != payerID || ev.Code != sms.TemplatePaymentReceivedOrder {
		t.Fatalf("event = %+v", ev)
	}
	// Re-saving SUCCESS (e.g. adding a reference) must not re-send.
	if _, err := s.UpdateStatus(context.Background(), adminActor(1), 1, UpdateStatusRequest{Status: "SUCCESS"}); err != nil {
		t.Fatal(err)
	}
	events.expectNone(t)
}
