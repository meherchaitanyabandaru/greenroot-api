package consent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
)

// Steps that need a confirmation.
const (
	SellerLoading        = "SELLER_LOADING"
	DriverDispatch       = "DRIVER_DISPATCH"
	BuyerQuotationAccept = "BUYER_QUOTATION_ACCEPT"
	BuyerDelivery        = "BUYER_DELIVERY"
)

// Template is the current wording of one step's confirmation.
type Template struct {
	Type       string   `json:"consent_type"`
	Version    int      `json:"consent_version"`
	Title      string   `json:"title"`
	Statements []string `json:"statements"`
}

// Templates are the current versions. Changing a statement means a new
// Version: earlier records keep the words that were agreed.
var Templates = map[string]Template{
	SellerLoading: {SellerLoading, 1, "Before the plants leave the nursery", []string{
		"The plant varieties loaded match the order.",
		"The quantities loaded are correct; any shortfall is entered as the loaded quantity.",
		"I checked the condition and quality of the plants before loading.",
		"The loading details and photos are accurate, and the lorry carries no prohibited items.",
		"Payment for this order is between the buyer and my nursery, outside GreenRoot, unless it is paid through a GreenRoot payment service.",
	}},
	DriverDispatch: {DriverDispatch, 1, "Before you start the trip", []string{
		"I have seen the loaded plants: the quantity and their visible condition match this trip.",
		"The lorry details on this trip are correct.",
		"The pickup and delivery details of this trip are correct.",
		"I accept this transport job and will take reasonable care of the plants from loading until delivery.",
	}},
	BuyerQuotationAccept: {BuyerQuotationAccept, 1, "Before you accept", []string{
		"I have checked the plants, quantities and prices in this quotation.",
		"Payment is agreed directly with the nursery; GreenRoot does not hold or guarantee it unless I pay through a GreenRoot payment service.",
		"The nursery's terms in this quotation (validity, delivery) apply. My rights under consumer law are not affected.",
	}},
	BuyerDelivery: {BuyerDelivery, 1, "Confirming delivery", []string{
		"I received the plants of this delivery.",
		"I checked the quantity and visible condition at delivery. A problem that was not visible can still be reported through GreenRoot's complaint process.",
	}},
}

// Given is a confirmation as an app sends it with an action.
type Given struct {
	Version  int      `json:"consent_version"`
	Accepted bool     `json:"accepted"`
	Evidence []string `json:"evidence"` // photo links or attachment URLs the party relied on
}

var (
	// ErrRequired: the step needs a confirmation that was not given.
	ErrRequired = errors.New("confirmation required")
	// ErrOutdated: the app showed older wording; it must show the current one.
	ErrOutdated = errors.New("confirmation wording has changed")
)

// Check says whether g confirms the current version of step.
func Check(step string, g *Given) error {
	if g == nil || !g.Accepted {
		return ErrRequired
	}
	if g.Version != Templates[step].Version {
		return ErrOutdated
	}
	return nil
}

// Record is one confirmation to store.
type Record struct {
	Type        string
	UserID      int64
	UserRole    string
	OrderID     *int64
	DispatchID  *int64
	QuotationID *int64
	Evidence    []string
	IPAddress   string
	UserAgent   string
}

// Store writes and reads consent_records.
type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Save records r with the current wording of its step. Saving the same
// confirmation again (a retry) is not an error and adds nothing.
func (s *Store) Save(ctx context.Context, r Record) error {
	t := Templates[r.Type]
	statements, err := json.Marshal(t.Statements)
	if err != nil {
		return err
	}
	if r.Evidence == nil {
		r.Evidence = []string{}
	}
	evidence, err := json.Marshal(r.Evidence)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO public.consent_records (consent_type, consent_version, statements, user_id, user_role,
			order_id, dispatch_id, quotation_id, evidence, ip_address, user_agent)
		VALUES ($1, $2, $3::jsonb, $4, $5, $6, $7, $8, $9::jsonb, NULLIF($10, ''), NULLIF($11, ''))
		ON CONFLICT DO NOTHING`,
		r.Type, t.Version, string(statements), r.UserID, r.UserRole, r.OrderID, r.DispatchID, r.QuotationID,
		string(evidence), truncate(r.IPAddress, 64), truncate(r.UserAgent, 255))
	return err
}

// Given reports whether userID already confirmed step for the dispatch.
func (s *Store) GivenForDispatch(ctx context.Context, step string, dispatchID, userID int64) (bool, error) {
	var ok bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM public.consent_records
		WHERE consent_type = $1 AND dispatch_id = $2 AND user_id = $3)`, step, dispatchID, userID).Scan(&ok)
	return ok, err
}

// Entry is a stored confirmation as the parties and admins see it.
type Entry struct {
	ID          int64           `json:"id"`
	Type        string          `json:"consent_type"`
	Version     int             `json:"consent_version"`
	Statements  []string        `json:"statements"`
	UserID      int64           `json:"user_id"`
	UserName    *string         `json:"user_name,omitempty"`
	UserRole    string          `json:"user_role"`
	OrderID     *int64          `json:"order_id,omitempty"`
	DispatchID  *int64          `json:"dispatch_id,omitempty"`
	QuotationID *int64          `json:"quotation_id,omitempty"`
	Evidence    json.RawMessage `json:"evidence"`
	CreatedAt   string          `json:"created_at"`
}

// ForOrder lists the confirmations of an order: its quotation, loading,
// trips and delivery. Callers check who may see the order.
func (s *Store) ForOrder(ctx context.Context, orderID int64) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.consent_record_id, c.consent_type, c.consent_version, c.statements, c.user_id,
			NULLIF(btrim(CONCAT_WS(' ', u.first_name, u.last_name)), ''), c.user_role,
			c.order_id, c.dispatch_id, c.quotation_id, c.evidence, to_char(c.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		FROM public.consent_records c
		LEFT JOIN public.users u ON u.user_id = c.user_id
		WHERE c.order_id = $1
		   OR c.dispatch_id IN (SELECT dispatch_id FROM public.dispatches WHERE order_id = $1)
		   OR c.quotation_id = (SELECT quotation_id FROM public.orders WHERE order_id = $1)
		ORDER BY c.created_at, c.consent_record_id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Entry, 0)
	for rows.Next() {
		var e Entry
		var statements []byte
		if err := rows.Scan(&e.ID, &e.Type, &e.Version, &statements, &e.UserID, &e.UserName, &e.UserRole,
			&e.OrderID, &e.DispatchID, &e.QuotationID, &e.Evidence, &e.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(statements, &e.Statements); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}

// WriteError answers a consent error (422 consent_required, 409
// consent_outdated) and reports whether err was one.
func WriteError(w http.ResponseWriter, err error, step string) bool {
	t := Templates[step]
	switch {
	case errors.Is(err, ErrRequired):
		response.Error(w, http.StatusUnprocessableEntity, "consent_required", t.Title+": please read and confirm each statement (version "+strconv.Itoa(t.Version)+").")
	case errors.Is(err, ErrOutdated):
		response.Error(w, http.StatusConflict, "consent_outdated", "The confirmation wording has changed. Update the app or reopen this screen and confirm again.")
	default:
		return false
	}
	return true
}

// TemplatesHandler serves GET /consents/templates: the wording the apps show
// and the version they send back.
func TemplatesHandler(w http.ResponseWriter, _ *http.Request) {
	out := make([]Template, 0, len(Templates))
	for _, step := range []string{SellerLoading, DriverDispatch, BuyerQuotationAccept, BuyerDelivery} {
		out = append(out, Templates[step])
	}
	response.OK(w, map[string]any{"templates": out})
}
