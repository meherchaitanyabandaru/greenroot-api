// Package razorpay is a minimal client for Razorpay Standard Checkout: create
// an order, fetch it back, and verify the checkout signature. Stdlib only.
// Docs: https://razorpay.com/docs/payments/payment-gateway/web-integration/standard/integration-steps/
package razorpay

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const defaultBaseURL = "https://api.razorpay.com/v1"

// MinAmountPaise is Razorpay's smallest order (₹1).
const MinAmountPaise = 100

var (
	// ErrAuth means Razorpay rejected the key pair (wrong or revoked keys).
	ErrAuth = errors.New("razorpay: authentication failed")
	// ErrGateway is any other failure talking to Razorpay.
	ErrGateway = errors.New("razorpay: request failed")
	// ErrRejected: Razorpay refused the request (e.g. payment already refunded);
	// the wrapped message is Razorpay's own description, safe to show an admin.
	ErrRejected = errors.New("razorpay rejected the request")
)

type Order struct {
	ID         string `json:"id"`
	Amount     int64  `json:"amount"` // paise
	AmountPaid int64  `json:"amount_paid"`
	Currency   string `json:"currency"`
	Receipt    string `json:"receipt"`
	Status     string `json:"status"` // created | attempted | paid
	Notes      Notes  `json:"notes"`
}

// Notes are the key/values set on the order. Razorpay sends [] when there are none.
type Notes map[string]string

func (n *Notes) UnmarshalJSON(b []byte) error {
	if bytes.HasPrefix(bytes.TrimSpace(b), []byte("[")) {
		*n = Notes{}
		return nil
	}
	m := map[string]string{}
	err := json.Unmarshal(b, &m)
	*n = m
	return err
}

type Client struct {
	keyID     string
	keySecret string
	baseURL   string
	http      *http.Client
}

// New returns nil when either key is missing: payments then stay in mock mode.
func New(keyID, keySecret string) *Client {
	if keyID == "" || keySecret == "" {
		return nil
	}
	return &Client{keyID: keyID, keySecret: keySecret, baseURL: defaultBaseURL, http: &http.Client{Timeout: 15 * time.Second}}
}

// KeyID is public: the app needs it to open checkout. The secret never leaves the API.
func (c *Client) KeyID() string { return c.keyID }

func (c *Client) CreateOrder(ctx context.Context, amountPaise int64, receipt string, notes Notes) (Order, error) {
	if amountPaise < MinAmountPaise {
		return Order{}, fmt.Errorf("razorpay: amount %d paise is below the %d minimum", amountPaise, MinAmountPaise)
	}
	body, _ := json.Marshal(map[string]any{"amount": amountPaise, "currency": "INR", "receipt": receipt, "notes": notes})
	var order Order
	err := c.do(ctx, http.MethodPost, "/orders", body, &order)
	return order, err
}

func (c *Client) FetchOrder(ctx context.Context, orderID string) (Order, error) {
	var order Order
	err := c.do(ctx, http.MethodGet, "/orders/"+url.PathEscape(orderID), nil, &order)
	return order, err
}

// Refund is a Razorpay refund. Status: pending | processed | failed.
type Refund struct {
	ID        string `json:"id"`
	PaymentID string `json:"payment_id"`
	Amount    int64  `json:"amount"` // paise
	Status    string `json:"status"`
}

// RefundPayment refunds amountPaise of a captured payment ("normal" speed, 5–7 days to the customer).
func (c *Client) RefundPayment(ctx context.Context, paymentID string, amountPaise int64, notes Notes) (Refund, error) {
	body, _ := json.Marshal(map[string]any{"amount": amountPaise, "speed": "normal", "notes": notes})
	var refund Refund
	err := c.do(ctx, http.MethodPost, "/payments/"+url.PathEscape(paymentID)+"/refund", body, &refund)
	return refund, err
}

// VerifySignature checks HMAC-SHA256(order_id + "|" + payment_id, key_secret)
// against the signature checkout returned, in constant time.
func (c *Client) VerifySignature(orderID, paymentID, signature string) bool {
	return hmac.Equal([]byte(Signature(c.keySecret, orderID, paymentID)), []byte(signature))
}

// Signature is the value Razorpay checkout returns as razorpay_signature.
func Signature(keySecret, orderID, paymentID string) string {
	mac := hmac.New(sha256.New, []byte(keySecret))
	mac.Write([]byte(orderID + "|" + paymentID))
	return hex.EncodeToString(mac.Sum(nil))
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.keyID, c.keySecret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrGateway, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrAuth
	}
	if resp.StatusCode == http.StatusBadRequest {
		var e struct {
			Error struct {
				Description string `json:"description"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Error.Description != "" {
			return fmt.Errorf("%w: %s", ErrRejected, e.Error.Description)
		}
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%w: %s %s: %d %s", ErrGateway, method, path, resp.StatusCode, raw)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%w: decode %s: %v", ErrGateway, path, err)
	}
	return nil
}
