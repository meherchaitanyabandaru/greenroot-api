package razorpay

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSignature_KnownVector(t *testing.T) {
	// printf 'order_1|pay_1' | openssl dgst -sha256 -hmac test-secret
	const want = "ba2a3986f33d5a6e148e445a747b407633361cc2fbc1d2faadd70ca5e101984e"
	if got := Signature("test-secret", "order_1", "pay_1"); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	c := New("rzp_test_x", "test-secret")
	if !c.VerifySignature("order_1", "pay_1", want) || c.VerifySignature("order_1", "pay_2", want) {
		t.Fatal("VerifySignature must accept only the matching payment")
	}
}

func TestNew_NilWithoutKeys(t *testing.T) {
	if New("", "s") != nil || New("k", "") != nil {
		t.Fatal("missing keys must disable the client")
	}
}

func TestOrders_AgainstFakeAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id, secret, _ := r.BasicAuth(); id != "rzp_test_x" || secret != "good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/orders":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "order_1", "amount": body["amount"], "currency": body["currency"], "status": "created", "notes": body["notes"]})
		case r.Method == http.MethodPost && r.URL.Path == "/payments/pay_1/refund":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "rfnd_1", "payment_id": "pay_1", "amount": body["amount"], "status": "processed"})
		case r.Method == http.MethodPost && r.URL.Path == "/payments/pay_done/refund":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"BAD_REQUEST_ERROR","description":"The payment has been fully refunded already"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/orders/order_2":
			_, _ = w.Write([]byte(`{"id":"order_2","amount":500,"currency":"INR","status":"paid","notes":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	c := New("rzp_test_x", "good")
	c.baseURL = srv.URL

	o, err := c.CreateOrder(ctx, 1000, "r1", Notes{"user_id": "10"})
	if err != nil || o.ID != "order_1" || o.Amount != 1000 || o.Currency != "INR" || o.Notes["user_id"] != "10" {
		t.Fatalf("create: %+v %v", o, err)
	}
	if _, err := c.CreateOrder(ctx, 99, "r2", nil); err == nil {
		t.Fatal("below 100 paise must be refused before calling Razorpay")
	}
	o, err = c.FetchOrder(ctx, "order_2")
	if err != nil || o.Status != "paid" || len(o.Notes) != 0 {
		t.Fatalf("fetch with empty notes []: %+v %v", o, err)
	}
	if _, err := c.FetchOrder(ctx, "missing"); !errors.Is(err, ErrGateway) {
		t.Fatalf("want ErrGateway, got %v", err)
	}
	rf, err := c.RefundPayment(ctx, "pay_1", 1000, Notes{"reason": "duplicate"})
	if err != nil || rf.ID != "rfnd_1" || rf.Amount != 1000 || rf.Status != "processed" {
		t.Fatalf("refund: %+v %v", rf, err)
	}
	if _, err := c.RefundPayment(ctx, "pay_done", 1000, nil); !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "fully refunded") {
		t.Fatalf("want ErrRejected with Razorpay's reason, got %v", err)
	}
	bad := New("rzp_test_x", "wrong")
	bad.baseURL = srv.URL
	if _, err := bad.CreateOrder(ctx, 1000, "r3", nil); !errors.Is(err, ErrAuth) {
		t.Fatalf("want ErrAuth, got %v", err)
	}
}
