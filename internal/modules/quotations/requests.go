package quotations

// Customer-initiated quotation requests, inside the normal quotation
// lifecycle: a customer lists the plants (catalog or their own names) and
// quantities for one nursery → CUSTOMER_REQUESTED; the nursery prices the
// lines (never swapping a plant for another) and sends it → CUSTOMER_SENT;
// from there accept, reject and convert-to-order work as for any quotation.
// The nursery may decline; the customer may cancel while it is unanswered.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/notifyqueue"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/publiccode"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/database/pgerr"
)

const (
	// requestWindow: how long the nursery has to send a first quotation.
	requestWindow   = 7 * 24 * time.Hour
	requestsPerDay  = 20
	maxRequestItems = 50
	// Notification types (mobile opens the quotation from the link).
	notifQuotationRequested = "QUOTATION_REQUESTED"
	notifRequestDeclined    = "QUOTATION_REQUEST_DECLINED"
	notifRequestCancelled   = "QUOTATION_REQUEST_CANCELLED"
)

var (
	ErrRequestExpired     = fmt.Errorf("%w: the nursery's time to answer this request is over", ErrInvalidTransition)
	ErrRequestNotPriced   = errors.New("every line needs a price before sending")
	ErrDuplicateRequest   = errors.New("an identical request to this nursery is already open")
	ErrTooManyRequests    = errors.New("too many quotation requests today")
	ErrNurseryUnavailable = errors.New("this nursery is not taking requests")
	ErrOwnNursery         = errors.New("you cannot request a quotation from your own nursery")
)

// CreateRequestInput is what a customer sends to ask a nursery for prices.
type CreateRequestInput struct {
	NurseryID int64                  `json:"nursery_id"`
	Note      *string                `json:"note"` // the customer's message, both sides see it
	Items     []QuotationItemRequest `json:"items"`
}

// PriceRequestInput prices some or all lines of a request. Quantity may be
// changed (what the nursery can supply); the plant of a line never changes.
type PriceRequestInput struct {
	Items      []PriceLine `json:"items"`
	ValidUntil *time.Time  `json:"valid_until"`
}

type PriceLine struct {
	ItemID      int64    `json:"item_id"`
	UnitPrice   float64  `json:"unit_price"`
	Quantity    *float64 `json:"quantity"`
	Description *string  `json:"description"` // the nursery's note on the line
}

type ReasonRequest struct {
	Reason string `json:"reason"`
}

// requestTarget is the nursery a customer asks.
type requestTarget struct {
	OwnerID     int64
	OwnerName   string
	NurseryName *string
	Phone       *string
	Approved    bool
}

// CreateRequest records a customer's request and tells the nursery owner.
func (s *Service) CreateRequest(ctx context.Context, actor ActorContext, in CreateRequestInput) (Quotation, error) {
	if actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN") || (actor.HasRole("DRIVER") && !actor.HasRole("BUYER")) {
		return Quotation{}, ErrForbidden // admins and drivers do not buy
	}
	if in.NurseryID <= 0 || len(in.Items) == 0 || len(in.Items) > maxRequestItems {
		return Quotation{}, ErrInvalidInput
	}
	for i := range in.Items {
		if err := normalizeItem(&in.Items[i], false); err != nil {
			return Quotation{}, err
		}
	}
	if in.Note != nil {
		note := strings.TrimSpace(*in.Note)
		if len([]rune(note)) > 1000 {
			return Quotation{}, ErrInvalidInput
		}
		in.Note = &note
		if note == "" {
			in.Note = nil
		}
	}
	target, err := s.repository.RequestTarget(ctx, in.NurseryID)
	if err != nil {
		return Quotation{}, err
	}
	if !target.Approved {
		return Quotation{}, ErrNurseryUnavailable
	}
	if member, err := s.repository.IsNurseryMember(ctx, in.NurseryID, actor.UserID); err != nil {
		return Quotation{}, err
	} else if member {
		return Quotation{}, ErrOwnNursery
	}
	if n, err := s.repository.CountRecentRequests(ctx, actor.UserID); err != nil {
		return Quotation{}, err
	} else if n >= requestsPerDay {
		return Quotation{}, ErrTooManyRequests
	}
	q, err := s.repository.CreateRequest(ctx, actor.UserID, in, target, requestFingerprint(in))
	if err != nil {
		return Quotation{}, err
	}
	s.audit(ctx, actor, auditlog.EntityQuotation, q.ID, actionInsert, map[string]any{"status": "CUSTOMER_REQUESTED", "nursery_id": in.NurseryID, "lines": len(in.Items)})
	s.enqueueNotification(ctx, target.OwnerID, notifQuotationRequested,
		"New quotation request",
		fmt.Sprintf("A customer asked for prices on %d plant(s) (%s). Price and send it within 7 days.", len(q.Items), q.QuotationCode),
		notifyqueue.Link("quotation", q.ID))
	return enrichQuotation(actor, *q), nil
}

// requestFingerprint identifies the same request sent twice: the nursery and
// every line (plant or the normal form of the custom name, size, quantity).
func requestFingerprint(in CreateRequestInput) string {
	lines := make([]string, 0, len(in.Items))
	val := func(p *string) string {
		if p == nil {
			return ""
		}
		return strings.ToLower(strings.Join(strings.Fields(*p), " "))
	}
	for _, it := range in.Items {
		lines = append(lines, fmt.Sprintf("%d|%s|%s|%s|%s|%g", it.PlantID, val(it.CustomPlantName), val(it.Variety),
			val(it.SizeDescription), val(it.Grade), it.Quantity))
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\n%s", in.NurseryID, strings.Join(lines, "\n"))))
	return hex.EncodeToString(sum[:])
}

// PriceRequest sets prices (and supplied quantities) on a request's lines.
func (s *Service) PriceRequest(ctx context.Context, actor ActorContext, id int64, in PriceRequestInput) (Quotation, error) {
	if len(in.Items) == 0 {
		return Quotation{}, ErrInvalidInput
	}
	for _, l := range in.Items {
		if l.ItemID <= 0 || l.UnitPrice < 0 || (l.Quantity != nil && *l.Quantity <= 0) {
			return Quotation{}, ErrInvalidInput
		}
	}
	if in.ValidUntil != nil && (!in.ValidUntil.After(time.Now()) || in.ValidUntil.After(time.Now().AddDate(0, 0, 90))) {
		return Quotation{}, ErrInvalidInput
	}
	q, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return Quotation{}, err
	}
	if err := s.canManage(ctx, actor, *q); err != nil {
		return Quotation{}, err
	}
	if q.Status != "CUSTOMER_REQUESTED" {
		return Quotation{}, ErrInvalidTransition
	}
	if !requestOpen(*q) {
		return Quotation{}, ErrRequestExpired
	}
	if q.AssignedManagerUserID != nil && *q.AssignedManagerUserID != actor.UserID && !actor.HasRole("ADMIN") && !actor.HasRole("SUPER_ADMIN") {
		return Quotation{}, ErrForbidden // exclusive editor, as for drafts
	}
	owned := map[int64]bool{}
	for _, it := range q.Items {
		owned[it.ID] = true
	}
	for _, l := range in.Items {
		if !owned[l.ItemID] {
			return Quotation{}, ErrInvalidInput
		}
	}
	updated, err := s.repository.PriceRequest(ctx, id, in)
	if err != nil {
		return Quotation{}, err
	}
	s.audit(ctx, actor, auditlog.EntityQuotation, id, actionUpdate, map[string]any{"priced_lines": len(in.Items), "valid_until": in.ValidUntil})
	return enrichQuotation(actor, *updated), nil
}

// DeclineRequest: the nursery cannot quote; the customer is told why.
func (s *Service) DeclineRequest(ctx context.Context, actor ActorContext, id int64, reason string) (Quotation, error) {
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) < 5 || len([]rune(reason)) > 500 {
		return Quotation{}, ErrInvalidInput
	}
	q, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return Quotation{}, err
	}
	if err := s.canManage(ctx, actor, *q); err != nil {
		return Quotation{}, err
	}
	updated, err := s.repository.CloseRequest(ctx, id, "REQUEST_DECLINED", &reason)
	if err != nil {
		return Quotation{}, err
	}
	s.audit(ctx, actor, auditlog.EntityQuotation, id, actionUpdate, map[string]any{"status": "REQUEST_DECLINED", "reason": reason})
	if updated.RequestedByUserID != nil {
		s.enqueueNotification(ctx, *updated.RequestedByUserID, notifRequestDeclined, "Quotation request declined",
			fmt.Sprintf("%s cannot quote %s: %s", textOr(updated.NurseryName, "The nursery"), updated.QuotationCode, reason),
			notifyqueue.Link("quotation", id))
	}
	return enrichQuotation(actor, *updated), nil
}

// CancelRequest: the customer withdraws a request the nursery has not answered.
func (s *Service) CancelRequest(ctx context.Context, actor ActorContext, id int64) (Quotation, error) {
	q, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return Quotation{}, err
	}
	if q.RequestedByUserID == nil || *q.RequestedByUserID != actor.UserID {
		return Quotation{}, ErrForbidden
	}
	updated, err := s.repository.CloseRequest(ctx, id, "REQUEST_CANCELLED", nil)
	if err != nil {
		return Quotation{}, err
	}
	s.audit(ctx, actor, auditlog.EntityQuotation, id, actionUpdate, map[string]any{"status": "REQUEST_CANCELLED"})
	s.enqueueNotification(ctx, updated.CreatedByUserID, notifRequestCancelled, "Quotation request cancelled",
		fmt.Sprintf("The customer cancelled request %s.", updated.QuotationCode), notifyqueue.Link("quotation", id))
	return enrichQuotation(actor, *updated), nil
}

// ─── repository ──────────────────────────────────────────────────────────────

func (r *PostgresRepository) RequestTarget(ctx context.Context, nurseryID int64) (requestTarget, error) {
	var t requestTarget
	var status sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT n.owner_user_id, COALESCE(NULLIF(btrim(CONCAT_WS(' ', u.first_name, u.last_name)), ''), 'Nursery owner'),
			n.nursery_name, n.mobile, n.status::text
		FROM public.nurseries n
		JOIN public.users u ON u.user_id = n.owner_user_id
		WHERE n.nursery_id = $1`, nurseryID).Scan(&t.OwnerID, &t.OwnerName, &t.NurseryName, &t.Phone, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	t.Approved = status.String == "APPROVED"
	return t, err
}

func (r *PostgresRepository) CountRecentRequests(ctx context.Context, userID int64) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM public.quotations
		WHERE requested_by_user_id = $1 AND created_at > CURRENT_TIMESTAMP - INTERVAL '1 day'`, userID).Scan(&n)
	return n, err
}

// CreateRequest stores the request and its lines in one transaction. The
// nursery owner is the quotation's author (the nursery issues it); the
// customer is its recipient and requester.
func (r *PostgresRepository) CreateRequest(ctx context.Context, customerID int64, in CreateRequestInput, t requestTarget, fingerprint string) (*Quotation, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	code, err := publiccode.Next(ctx, tx, publiccode.Quotations, time.Now())
	if err != nil {
		return nil, err
	}
	var id int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO public.quotations (
			quotation_code, created_by_user_id, created_by_name, nursery_id, nursery_name, nursery_phone,
			quotation_type, customer_user_id, recipient_name, recipient_mobile,
			requested_by_user_id, request_note, request_expires_at, request_fingerprint,
			total_amount, status, created_at, updated_at
		)
		SELECT $1, $2, $3, $4, $5, $6, 'CUSTOMER', u.user_id,
			NULLIF(btrim(CONCAT_WS(' ', u.first_name, u.last_name)), ''), u.mobile,
			u.user_id, $8, CURRENT_TIMESTAMP + $9::interval, $10,
			0, 'CUSTOMER_REQUESTED', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		FROM public.users u WHERE u.user_id = $7
		RETURNING quotation_id`,
		code, t.OwnerID, t.OwnerName, in.NurseryID, t.NurseryName, t.Phone, customerID,
		in.Note, fmt.Sprintf("%d seconds", int(requestWindow.Seconds())), fingerprint).Scan(&id)
	if pgerr.Constraint(err) == "uq_quotations_open_request" {
		return nil, ErrDuplicateRequest
	}
	if err != nil {
		return nil, err
	}
	for _, item := range in.Items {
		if err := r.createItemTx(ctx, tx, id, customerID, item, false); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, id)
}

func (r *PostgresRepository) PriceRequest(ctx context.Context, id int64, in PriceRequestInput) (*Quotation, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var locked string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM public.quotations WHERE quotation_id = $1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&locked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if locked != "CUSTOMER_REQUESTED" {
		return nil, ErrInvalidTransition // sent, declined or cancelled meanwhile
	}
	for _, l := range in.Items {
		if _, err := tx.ExecContext(ctx, `
			UPDATE public.quotation_items
			SET unit_price = $3, quantity = COALESCE($4, quantity), total_price = round(COALESCE($4, quantity) * $3, 2),
				description = COALESCE($5, description)
			WHERE quotation_item_id = $2 AND quotation_id = $1`, id, l.ItemID, l.UnitPrice, l.Quantity, l.Description); err != nil {
			return nil, pgerr.Map(err)
		}
	}
	if in.ValidUntil != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE public.quotations SET valid_until = $2 WHERE quotation_id = $1`, id, *in.ValidUntil); err != nil {
			return nil, err
		}
	}
	if err := r.refreshTotalTx(ctx, tx, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, id)
}

// CloseRequest declines or cancels a request still waiting for prices.
func (r *PostgresRepository) CloseRequest(ctx context.Context, id int64, status string, reason *string) (*Quotation, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE public.quotations
		SET status = $2::varchar, decline_reason = $3, updated_at = CURRENT_TIMESTAMP,
			declined_at = CASE WHEN $2::varchar = 'REQUEST_DECLINED' THEN CURRENT_TIMESTAMP END,
			cancelled_at = CASE WHEN $2::varchar = 'REQUEST_CANCELLED' THEN CURRENT_TIMESTAMP END
		WHERE quotation_id = $1 AND status = 'CUSTOMER_REQUESTED' AND deleted_at IS NULL`, id, status, reason)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := r.FindByID(ctx, id); err != nil {
			return nil, err
		}
		return nil, ErrInvalidTransition
	}
	return r.FindByID(ctx, id)
}

// ─── HTTP ────────────────────────────────────────────────────────────────────

// CreateRequest handles POST /quotations/requests (a customer). Send an
// Idempotency-Key: a retry replays the first answer.
func (h *Handler) CreateRequest(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var in CreateRequestInput
	if !response.DecodeJSON(w, r, &in) {
		return
	}
	q, err := h.service.CreateRequest(r.Context(), actor, in)
	if err != nil {
		writeRequestError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, QuotationResponse{Quotation: q})
}

func (h *Handler) PriceRequest(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var in PriceRequestInput
	if !response.DecodeJSON(w, r, &in) {
		return
	}
	q, err := h.service.PriceRequest(r.Context(), actor, id, in)
	if err != nil {
		writeRequestError(w, err)
		return
	}
	response.OK(w, QuotationResponse{Quotation: q})
}

func (h *Handler) DeclineRequest(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var in ReasonRequest
	if !response.DecodeJSON(w, r, &in) {
		return
	}
	q, err := h.service.DeclineRequest(r.Context(), actor, id, in.Reason)
	if err != nil {
		writeRequestError(w, err)
		return
	}
	response.OK(w, QuotationResponse{Quotation: q})
}

func (h *Handler) CancelRequest(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	q, err := h.service.CancelRequest(r.Context(), actor, id)
	if err != nil {
		writeRequestError(w, err)
		return
	}
	response.OK(w, QuotationResponse{Quotation: q})
}

func writeRequestError(w http.ResponseWriter, err error) { writeError(w, err) }

// requestError writes the request lifecycle's errors; false for any other.
func requestError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, ErrDuplicateRequest):
		response.Error(w, http.StatusConflict, "duplicate_request", "You already sent this exact request to this nursery. Wait for its answer or cancel it first.")
	case errors.Is(err, ErrTooManyRequests):
		response.Error(w, http.StatusTooManyRequests, "too_many_requests", "You have sent many quotation requests today. Try again tomorrow.")
	case errors.Is(err, ErrNurseryUnavailable):
		response.Error(w, http.StatusConflict, "nursery_unavailable", "This nursery is not taking requests right now.")
	case errors.Is(err, ErrOwnNursery):
		response.Error(w, http.StatusConflict, "own_nursery", "You cannot ask your own nursery for a quotation.")
	case errors.Is(err, ErrRequestExpired):
		response.Error(w, http.StatusConflict, "request_expired", "The time to answer this request is over. The customer can send a new one.")
	case errors.Is(err, ErrRequestNotPriced):
		response.Error(w, http.StatusUnprocessableEntity, "request_not_priced", "Give every line a price before sending.")
	default:
		return false
	}
	return true
}
