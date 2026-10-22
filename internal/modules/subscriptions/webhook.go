package subscriptions

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
	"github.com/meherchaitanyabandaru/greenroot-api/platform/razorpay"
)

// Razorpay webhook (Dashboard → Webhooks → https://<api>/api/v1/webhooks/razorpay,
// events payment.captured, order.paid, refund.processed, refund.failed).
//
// 2xx tells Razorpay "done"; anything else makes it retry for 24 h, so we
// answer 500 only for trouble that a retry can fix.

type razorpayEvent struct {
	Event   string `json:"event"`
	Payload struct {
		Payment struct {
			Entity struct {
				ID      string `json:"id"`
				OrderID string `json:"order_id"`
			} `json:"entity"`
		} `json:"payment"`
		Refund struct {
			Entity struct {
				ID        string `json:"id"`
				PaymentID string `json:"payment_id"`
				Status    string `json:"status"`
			} `json:"entity"`
		} `json:"refund"`
	} `json:"payload"`
}

// HandleRazorpayWebhook verifies and applies one webhook delivery; it returns
// the HTTP status to answer with.
func (s *Service) HandleRazorpayWebhook(ctx context.Context, body []byte, signature string) int {
	if s.gateway == nil {
		return http.StatusServiceUnavailable
	}
	if !razorpay.VerifyWebhook(body, signature, s.webhookSecret) {
		slog.Warn("razorpay webhook: bad signature")
		return http.StatusBadRequest
	}
	var ev razorpayEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return http.StatusBadRequest
	}
	switch ev.Event {
	case "payment.captured", "order.paid":
		p := ev.Payload.Payment.Entity
		if p.ID == "" || p.OrderID == "" {
			return http.StatusOK
		}
		ok, err := s.ApplyGatewayPayment(ctx, p.OrderID, p.ID)
		if err != nil {
			slog.Error("razorpay webhook: will retry", "event", ev.Event, "order_id", p.OrderID, "payment_id", p.ID, "error", err)
			return http.StatusInternalServerError
		}
		if !ok {
			// Charged but a rule refuses it (e.g. paused meanwhile): refund by hand.
			slog.Error("razorpay webhook: paid but not applied — refund manually", "order_id", p.OrderID, "payment_id", p.ID)
		}
	case "refund.processed", "refund.failed":
		r := ev.Payload.Refund.Entity
		if r.ID != "" && r.PaymentID != "" {
			if err := s.repository.UpdateRefundStatus(ctx, r.PaymentID, r.ID, r.Status); err != nil {
				return http.StatusInternalServerError
			}
			if ev.Event == "refund.failed" {
				slog.Error("razorpay webhook: refund failed — the customer has not been paid back", "refund_id", r.ID, "payment_id", r.PaymentID)
			}
		}
	}
	return http.StatusOK
}

func (h *Handler) RazorpayWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "invalid_body", "could not read body")
		return
	}
	status := h.service.HandleRazorpayWebhook(r.Context(), body, r.Header.Get("X-Razorpay-Signature"))
	if status != http.StatusOK {
		response.Error(w, status, "webhook_not_processed", http.StatusText(status))
		return
	}
	response.OK(w, map[string]bool{"ok": true})
}
