package messaging

import (
	"context"
	"errors"
	"testing"
)

type fakeChannel struct {
	name string
	err  error
	got  []Message
}

func (c *fakeChannel) Name() string { return c.name }
func (c *fakeChannel) Deliver(_ context.Context, msg Message) (Result, error) {
	c.got = append(c.got, msg)
	return Result{Status: "SENT"}, c.err
}

type fakeRecipients map[int64]Recipient

func (f fakeRecipients) Recipient(_ context.Context, id int64) (Recipient, error) {
	r, ok := f[id]
	if !ok {
		return Recipient{}, ErrUnknownUser
	}
	return r, nil
}

func TestExpandValues(t *testing.T) {
	got := ExpandValues(map[string]string{
		"amount": "1250.50", "order_code": "ORD-20261015-0008", "payment_code": "PAY-20261016-0012", "mobile": "9876543210", "name": "Ravi",
	})
	want := map[string]string{
		"amount": "1250.50", "amount_rupees": "1250", "amount_paise": "50",
		"order_code": "ORD-20261015-0008", "order_date": "20261015", "order_seq": "0008",
		"payment_code": "PAY-20261016-0012", "payment_date": "20261016", "payment_seq": "0012",
		"mobile": "9876543210", "mobile_last4": "3210", "name": "Ravi",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("unexpected extra keys: %v", got)
	}
	// Malformed inputs are passed through, never guessed at.
	odd := ExpandValues(map[string]string{"amount": "abc", "promo_code": "DIWALI"})
	if _, ok := odd["amount_rupees"]; ok {
		t.Error("non-numeric amount must not be split")
	}
	if _, ok := odd["promo_date"]; ok {
		t.Error("code without date-seq parts must not be split")
	}
}

func TestAmountParts(t *testing.T) {
	for in, want := range map[float64]string{1250.5: "1250.50", 0.07: "0.07", 19.999: "20.00", 0: "0.00"} {
		if got := FormatAmount(in); got != want {
			t.Errorf("FormatAmount(%v) = %s, want %s", in, got, want)
		}
	}
}

func TestNotify_FansOutAndIsolatesFailures(t *testing.T) {
	sms := &fakeChannel{name: "SMS", err: errors.New("gateway down")}
	email := &fakeChannel{name: "EMAIL", err: ErrSkipped}
	n := NewNotifier(fakeRecipients{7: {UserID: 7, Mobile: "9876543210", Email: "a@b.in"}}, sms, email)

	outcomes, err := n.Notify(context.Background(), Event{Code: "PAYMENT_FAILED", UserID: 7, Values: map[string]string{"amount": "10"}, Sensitive: []string{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 2 || outcomes[0].Channel != "SMS" || outcomes[1].Channel != "EMAIL" {
		t.Fatalf("outcomes = %+v", outcomes)
	}
	if len(email.got) != 1 {
		t.Fatal("a failing SMS channel must not stop email")
	}
	msg := email.got[0]
	if msg.Recipient.Email != "a@b.in" || msg.Values["amount_rupees"] != "10" || msg.Sensitive[0] != "x" {
		t.Fatalf("message = %+v", msg)
	}
}

func TestNotify_UnknownUser(t *testing.T) {
	n := NewNotifier(fakeRecipients{}, &fakeChannel{name: "SMS"})
	if _, err := n.Notify(context.Background(), Event{Code: "X", UserID: 1}); !errors.Is(err, ErrUnknownUser) {
		t.Fatalf("want ErrUnknownUser, got %v", err)
	}
}

func TestNotify_NotOnAppOnly(t *testing.T) {
	sms := &fakeChannel{name: "SMS"}
	n := NewNotifier(fakeRecipients{
		7: {UserID: 7, Mobile: "9876543210", SignedIn: true},
		8: {UserID: 8, Mobile: "9876500000"},
	}, sms)
	ctx := context.Background()
	if out, _ := n.Notify(ctx, Event{Code: "ORDER_PLACED", UserID: 7, NotOnAppOnly: true}); len(out) != 0 {
		t.Errorf("app user (push reaches them): want no SMS, got %+v", out)
	}
	if out, _ := n.Notify(ctx, Event{Code: "ORDER_PLACED", UserID: 8, NotOnAppOnly: true}); len(out) != 1 {
		t.Errorf("never signed in: want the SMS, got %+v", out)
	}
	if out, _ := n.Notify(ctx, Event{Code: "QUOTATION_RECEIVED", Mobile: "9876511111", NotOnAppOnly: true}); len(out) != 1 || sms.got[len(sms.got)-1].Recipient.Mobile != "9876511111" {
		t.Errorf("no account, only a mobile: want the SMS to that mobile, got %+v", out)
	}
}
