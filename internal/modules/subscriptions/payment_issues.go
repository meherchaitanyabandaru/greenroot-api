package subscriptions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
)

// Payment issues: Razorpay money that needs a human. The webhook records one
// when a captured payment cannot be applied (a rule refuses it, e.g. the
// subscription was paused after checkout) or when Razorpay reports a refund
// failed. Razorpay gets 200 for both (a retry cannot fix them), so this queue,
// shown in the admin dashboard, is where they are settled.

const (
	IssuePaidNotApplied = "PAID_NOT_APPLIED"
	IssueRefundFailed   = "REFUND_FAILED"

	IssueOpen     = "OPEN"
	IssueResolved = "RESOLVED"

	// logMarker is matched by the CloudWatch alarm (greenroot-infra).
	logMarker = "PAYMENT_ISSUE"
)

type PaymentIssue struct {
	ID                int64      `json:"id"`
	IssueType         string     `json:"issue_type"`
	Provider          string     `json:"provider"`
	ProviderOrderID   *string    `json:"provider_order_id,omitempty"`
	ProviderPaymentID string     `json:"provider_payment_id"`
	ProviderRefundID  *string    `json:"provider_refund_id,omitempty"`
	UserID            *int64     `json:"user_id,omitempty"`
	Amount            *float64   `json:"amount,omitempty"`
	Reason            string     `json:"reason"`
	Status            string     `json:"status"`
	ResolutionNote    *string    `json:"resolution_note,omitempty"`
	ResolvedByUserID  *int64     `json:"resolved_by_user_id,omitempty"`
	ResolvedAt        *time.Time `json:"resolved_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

type PaymentIssuesResponse struct {
	Issues []PaymentIssue `json:"issues"`
}

type PaymentIssueResponse struct {
	Issue PaymentIssue `json:"issue"`
}

type ResolvePaymentIssueRequest struct {
	Note string `json:"note"`
}

// recordIssue stores the issue (idempotent per payment and kind) and logs the
// alarm marker. A failure to store is returned so the webhook asks Razorpay
// to retry rather than lose the record.
func (s *Service) recordIssue(ctx context.Context, issue PaymentIssue) error {
	slog.Error(logMarker+": Razorpay money needs a manual action", "issue_type", issue.IssueType,
		"order_id", stringOrEmpty(issue.ProviderOrderID), "payment_id", issue.ProviderPaymentID,
		"refund_id", stringOrEmpty(issue.ProviderRefundID), "reason", issue.Reason)
	return s.repository.RecordPaymentIssue(ctx, issue)
}

func (s *Service) ListPaymentIssues(ctx context.Context, actor ActorContext, status string) ([]PaymentIssue, error) {
	if !actor.HasRole("ADMIN") {
		return nil, ErrForbidden
	}
	status = strings.ToUpper(strings.TrimSpace(status))
	if status != "" && status != IssueOpen && status != IssueResolved {
		return nil, ErrInvalidInput
	}
	return s.repository.ListPaymentIssues(ctx, status)
}

// ResolvePaymentIssue closes an issue after the admin refunded or applied the
// payment by hand; the note says which.
func (s *Service) ResolvePaymentIssue(ctx context.Context, actor ActorContext, id int64, req ResolvePaymentIssueRequest) (PaymentIssue, error) {
	if !actor.HasRole("ADMIN") {
		return PaymentIssue{}, ErrForbidden
	}
	note := strings.TrimSpace(req.Note)
	if len([]rune(note)) < 5 || len([]rune(note)) > 1000 {
		return PaymentIssue{}, ErrInvalidInput
	}
	issue, err := s.repository.ResolvePaymentIssue(ctx, id, actor.UserID, note)
	if err != nil {
		return PaymentIssue{}, err
	}
	s.audit(ctx, actor, id, actionUpdate, map[string]any{"payment_issue": "RESOLVED", "note": note})
	return *issue, nil
}

func (h *Handler) ListPaymentIssues(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	issues, err := h.service.ListPaymentIssues(r.Context(), actor, r.URL.Query().Get("status"))
	if err != nil {
		writeSubscriptionsError(w, err)
		return
	}
	response.OK(w, PaymentIssuesResponse{Issues: issues})
}

func (h *Handler) ResolvePaymentIssue(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req ResolvePaymentIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}
	issue, err := h.service.ResolvePaymentIssue(r.Context(), actor, id, req)
	if err != nil {
		writeSubscriptionsError(w, err)
		return
	}
	response.OK(w, PaymentIssueResponse{Issue: issue})
}

// ── Postgres ──────────────────────────────────────────────────────────────

const paymentIssueColumns = `payment_issue_id, issue_type, provider, provider_order_id, provider_payment_id,
	provider_refund_id, user_id, amount, reason, status, resolution_note, resolved_by_user_id, resolved_at, created_at`

func (r *PostgresRepository) RecordPaymentIssue(ctx context.Context, i PaymentIssue) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO public.payment_issues (issue_type, provider, provider_order_id, provider_payment_id, provider_refund_id, user_id, amount, reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (provider, provider_payment_id, issue_type) DO UPDATE
		SET reason = EXCLUDED.reason, provider_refund_id = COALESCE(EXCLUDED.provider_refund_id, payment_issues.provider_refund_id),
			updated_at = CURRENT_TIMESTAMP`,
		i.IssueType, i.Provider, i.ProviderOrderID, i.ProviderPaymentID, i.ProviderRefundID, i.UserID, i.Amount, i.Reason)
	return err
}

func (r *PostgresRepository) ListPaymentIssues(ctx context.Context, status string) ([]PaymentIssue, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+paymentIssueColumns+` FROM public.payment_issues
		WHERE ($1::varchar = '' OR status = $1::varchar) ORDER BY created_at DESC LIMIT 200`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]PaymentIssue, 0)
	for rows.Next() {
		i, err := scanPaymentIssue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) ResolvePaymentIssue(ctx context.Context, id, userID int64, note string) (*PaymentIssue, error) {
	i, err := scanPaymentIssue(r.db.QueryRowContext(ctx, `
		UPDATE public.payment_issues
		SET status = 'RESOLVED', resolution_note = $3, resolved_by_user_id = $2, resolved_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE payment_issue_id = $1 AND status = 'OPEN'
		RETURNING `+paymentIssueColumns, id, userID, note))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &i, nil
}

func scanPaymentIssue(row interface{ Scan(...any) error }) (PaymentIssue, error) {
	var i PaymentIssue
	var orderID, refundID, note sql.NullString
	var userID, resolvedBy sql.NullInt64
	var amount sql.NullFloat64
	var resolvedAt sql.NullTime
	if err := row.Scan(&i.ID, &i.IssueType, &i.Provider, &orderID, &i.ProviderPaymentID, &refundID, &userID, &amount,
		&i.Reason, &i.Status, &note, &resolvedBy, &resolvedAt, &i.CreatedAt); err != nil {
		return PaymentIssue{}, err
	}
	if orderID.Valid {
		i.ProviderOrderID = &orderID.String
	}
	if refundID.Valid {
		i.ProviderRefundID = &refundID.String
	}
	if userID.Valid {
		i.UserID = &userID.Int64
	}
	if amount.Valid {
		i.Amount = &amount.Float64
	}
	if note.Valid {
		i.ResolutionNote = &note.String
	}
	if resolvedBy.Valid {
		i.ResolvedByUserID = &resolvedBy.Int64
	}
	if resolvedAt.Valid {
		i.ResolvedAt = &resolvedAt.Time
	}
	return i, nil
}
