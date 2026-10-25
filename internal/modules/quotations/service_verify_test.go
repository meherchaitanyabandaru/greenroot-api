package quotations

import (
	"testing"
	"time"
)

func TestPublicQuotationStatus(t *testing.T) {
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	cases := []struct {
		q    Quotation
		want string
	}{
		{Quotation{Status: "CUSTOMER_SENT", ValidUntil: &future}, "ACTIVE"},
		{Quotation{Status: "CUSTOMER_SENT"}, "ACTIVE"},
		{Quotation{Status: "CUSTOMER_SENT", ValidUntil: &past}, "EXPIRED"},
		{Quotation{Status: "CUSTOMER_ACCEPTED", ValidUntil: &past}, "ACTIVE"},
		{Quotation{Status: "CUSTOMER_REJECTED"}, "CANCELLED"},
		{Quotation{Status: "INTERNAL_DRAFT"}, "CANCELLED"},
		{Quotation{Status: "CUSTOMER_DRAFT"}, "CANCELLED"},
		{Quotation{Status: "CONVERTED"}, "CONVERTED"},
		{Quotation{Status: "SOMETHING_NEW"}, "CANCELLED"},
	}
	for _, c := range cases {
		if got := publicQuotationStatus(c.q); got != c.want {
			t.Errorf("%s (valid_until %v): got %s, want %s", c.q.Status, c.q.ValidUntil, got, c.want)
		}
	}
}

func TestVerifyTokensAreUniqueHex(t *testing.T) {
	a, err := generateToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := generateToken()
	if len(a) != 64 || a == b {
		t.Errorf("want two different 64-char tokens, got %q and %q", a, b)
	}
}
