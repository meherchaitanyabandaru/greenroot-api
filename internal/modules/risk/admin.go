package risk

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/notifyqueue"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
	"github.com/redis/go-redis/v9"
)

type Signal struct {
	ID             int64           `json:"id"`
	SignalCode     string          `json:"signal_code"`
	Description    string          `json:"description"`
	EntityType     string          `json:"entity_type"`
	EntityID       int64           `json:"entity_id"`
	EntityLabel    *string         `json:"entity_label,omitempty"`
	RiskLevel      string          `json:"risk_level"`
	Status         string          `json:"status"`
	Details        json.RawMessage `json:"details"`
	IsHold         bool            `json:"is_hold"`
	ResolutionNote *string         `json:"resolution_note,omitempty"`
	ResolvedAt     *time.Time      `json:"resolved_at,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	Notes          []Note          `json:"notes,omitempty"`
}

type Note struct {
	ID        int64     `json:"id"`
	UserID    *int64    `json:"user_id,omitempty"`
	UserName  *string   `json:"user_name,omitempty"`
	Action    string    `json:"action"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

type Rule struct {
	SignalCode  string    `json:"signal_code"`
	IsEnabled   bool      `json:"is_enabled"`
	Threshold   float64   `json:"threshold"`
	RiskLevel   string    `json:"risk_level"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Actions an admin takes on a signal; each needs a note.
var actions = map[string]bool{
	"REVIEW": true, "HOLD": true, "RELEASE_HOLD": true, "REQUEST_VERIFICATION": true,
	"RESOLVE": true, "DISMISS": true, "CONFIRM_FRAUD": true, "NOTE": true,
}

type Handler struct {
	db    *sql.DB
	jwt   *jwtplatform.Service
	audit *auditlog.Service
	redis redis.Cmdable
}

type Module struct{ h *Handler }

func NewModule(db *sql.DB, jwt *jwtplatform.Service, audit *auditlog.Service, rdb redis.Cmdable) Module {
	return Module{h: &Handler{db: db, jwt: jwt, audit: audit, redis: rdb}}
}

func (m Module) RegisterRoutes(r chi.Router) {
	r.Get("/admin/risk-signals", m.h.List)
	r.Post("/admin/risk-signals/scan", m.h.ScanNow)
	r.Get("/admin/risk-signals/{id}", m.h.Get)
	r.Post("/admin/risk-signals/{id}/actions", m.h.Act)
	r.Get("/admin/risk-rules", m.h.ListRules)
	r.Put("/admin/risk-rules/{code}", m.h.UpdateRule)
}

func (h *Handler) admin(w http.ResponseWriter, r *http.Request) (authctx.ActorContext, bool) {
	raw, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return authctx.ActorContext{}, false
	}
	a := raw.AsActorContext()
	if !a.HasRole("ADMIN") && !a.HasRole("SUPER_ADMIN") {
		response.Error(w, http.StatusForbidden, "forbidden", "admins only")
		return authctx.ActorContext{}, false
	}
	return a, true
}

const signalSelect = `
	SELECT s.risk_signal_id, s.signal_code, COALESCE(r.description, s.signal_code), s.entity_type, s.entity_id,
		CASE s.entity_type
			WHEN 'ORDER' THEN (SELECT order_number FROM public.orders WHERE order_id = s.entity_id)
			WHEN 'NURSERY' THEN (SELECT nursery_name FROM public.nurseries WHERE nursery_id = s.entity_id)
			ELSE (SELECT NULLIF(btrim(CONCAT_WS(' ', first_name, last_name, mobile)), '') FROM public.users WHERE user_id = s.entity_id)
		END,
		s.risk_level, s.status, s.details, s.is_hold, s.resolution_note, s.resolved_at, s.created_at
	FROM public.risk_signals s
	LEFT JOIN public.risk_rules r ON r.signal_code = s.signal_code`

func scanSignal(row interface{ Scan(...any) error }) (Signal, error) {
	var s Signal
	err := row.Scan(&s.ID, &s.SignalCode, &s.Description, &s.EntityType, &s.EntityID, &s.EntityLabel, &s.RiskLevel,
		&s.Status, &s.Details, &s.IsHold, &s.ResolutionNote, &s.ResolvedAt, &s.CreatedAt)
	return s, err
}

// List is the review queue: open first, highest level first.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.admin(w, r); !ok {
		return
	}
	q := r.URL.Query()
	status, level, entity := strings.ToUpper(q.Get("status")), strings.ToUpper(q.Get("risk_level")), strings.ToUpper(q.Get("entity_type"))
	page, _ := strconv.Atoi(q.Get("page"))
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 50
	}
	const where = ` WHERE ($1 = '' OR s.status = ANY(string_to_array($1, ','))) AND ($2 = '' OR s.risk_level = $2) AND ($3 = '' OR s.entity_type = $3)`
	var total int64
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM public.risk_signals s`+where, status, level, entity).Scan(&total); err != nil {
		response.Error(w, http.StatusInternalServerError, "risk_error", "could not read signals")
		return
	}
	rows, err := h.db.QueryContext(r.Context(), signalSelect+where+`
		ORDER BY (s.status IN ('OPEN', 'UNDER_REVIEW')) DESC,
			array_position(ARRAY['CONFIRMED_FRAUD', 'HIGH', 'MEDIUM', 'LOW'], s.risk_level), s.created_at DESC
		LIMIT $4 OFFSET $5`, status, level, entity, perPage, (page-1)*perPage)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "risk_error", "could not read signals")
		return
	}
	defer rows.Close()
	out := make([]Signal, 0)
	for rows.Next() {
		s, err := scanSignal(rows)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "risk_error", "could not read signals")
			return
		}
		out = append(out, s)
	}
	if rows.Err() != nil {
		response.Error(w, http.StatusInternalServerError, "risk_error", "could not read signals")
		return
	}
	response.OK(w, map[string]any{"signals": out, "total": total})
}

func (h *Handler) find(r *http.Request, id int64) (Signal, error) {
	s, err := scanSignal(h.db.QueryRowContext(r.Context(), signalSelect+` WHERE s.risk_signal_id = $1`, id))
	if err != nil {
		return s, err
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT n.risk_signal_note_id, n.user_id, NULLIF(btrim(CONCAT_WS(' ', u.first_name, u.last_name)), ''), n.action, n.note, n.created_at
		FROM public.risk_signal_notes n LEFT JOIN public.users u ON u.user_id = n.user_id
		WHERE n.risk_signal_id = $1 ORDER BY n.created_at, n.risk_signal_note_id`, id)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	s.Notes = []Note{}
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.UserID, &n.UserName, &n.Action, &n.Note, &n.CreatedAt); err != nil {
			return s, err
		}
		s.Notes = append(s.Notes, n)
	}
	return s, rows.Err()
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(w, http.StatusBadRequest, "invalid_id", "invalid id")
		return 0, false
	}
	return id, true
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.admin(w, r); !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s, err := h.find(r, id)
	if errors.Is(err, sql.ErrNoRows) {
		response.Error(w, http.StatusNotFound, "not_found", "signal not found")
		return
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "risk_error", "could not read the signal")
		return
	}
	response.OK(w, map[string]any{"signal": s})
}

type actionRequest struct {
	Action string `json:"action"`
	Note   string `json:"note"`
}

// Act records an admin decision on a signal, with a note.
func (h *Handler) Act(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.admin(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req actionRequest
	if !response.DecodeJSON(w, r, &req) {
		return
	}
	action, note := strings.ToUpper(strings.TrimSpace(req.Action)), strings.TrimSpace(req.Note)
	if !actions[action] || len([]rune(note)) < 5 {
		response.Error(w, http.StatusBadRequest, "invalid_input", "choose an action and write a note (at least 5 characters)")
		return
	}
	if action == "CONFIRM_FRAUD" && !actor.HasRole("SUPER_ADMIN") {
		response.Error(w, http.StatusForbidden, "forbidden", "only a super admin can confirm fraud")
		return
	}
	cur, err := h.find(r, id)
	if errors.Is(err, sql.ErrNoRows) {
		response.Error(w, http.StatusNotFound, "not_found", "signal not found")
		return
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "risk_error", "could not read the signal")
		return
	}
	closed := cur.Status == "RESOLVED" || cur.Status == "DISMISSED"
	if closed && action != "NOTE" && action != "REVIEW" {
		response.Error(w, http.StatusConflict, "signal_closed", "this signal is closed; reopen it with REVIEW first")
		return
	}
	if (action == "HOLD") && cur.EntityType != "ORDER" {
		response.Error(w, http.StatusBadRequest, "invalid_input", "only an order can be held; suspend partners from their own page")
		return
	}
	set := ""
	switch action {
	case "REVIEW", "REQUEST_VERIFICATION":
		set = `status = 'UNDER_REVIEW', resolved_at = NULL, resolved_by_user_id = NULL`
	case "HOLD":
		set = `is_hold = true, status = 'UNDER_REVIEW'`
	case "RELEASE_HOLD":
		set = `is_hold = false`
	case "RESOLVE":
		set = `status = 'RESOLVED', is_hold = false, resolution_note = $3, resolved_by_user_id = $2, resolved_at = CURRENT_TIMESTAMP`
	case "DISMISS":
		set = `status = 'DISMISSED', is_hold = false, resolution_note = $3, resolved_by_user_id = $2, resolved_at = CURRENT_TIMESTAMP`
	case "CONFIRM_FRAUD":
		set = `risk_level = 'CONFIRMED_FRAUD', status = 'UNDER_REVIEW'`
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "risk_error", "could not save")
		return
	}
	defer func() { _ = tx.Rollback() }()
	if set != "" {
		// #nosec G202 -- set is one of the fixed fragments above. $2 and $3 are
		// always bound (some fragments use them), so the WHERE names their types.
		if _, err := tx.ExecContext(r.Context(), `UPDATE public.risk_signals SET `+set+`, updated_at = CURRENT_TIMESTAMP
			WHERE risk_signal_id = $1 AND ($2::bigint IS NOT NULL) AND ($3::text IS NOT NULL)`, id, actor.UserID, note); err != nil {
			response.Error(w, http.StatusInternalServerError, "risk_error", "could not save")
			return
		}
	}
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO public.risk_signal_notes (risk_signal_id, user_id, action, note) VALUES ($1, $2, $3, $4)`,
		id, actor.UserID, action, note); err != nil {
		response.Error(w, http.StatusInternalServerError, "risk_error", "could not save")
		return
	}
	if err := tx.Commit(); err != nil {
		response.Error(w, http.StatusInternalServerError, "risk_error", "could not save")
		return
	}
	if action == "REQUEST_VERIFICATION" {
		h.requestVerification(r, cur)
	}
	h.audit.Log(r.Context(), auditlog.Entry{UserID: actor.UserID, Module: auditlog.ModuleRisk, EntityType: "risk_signal", EntityID: id,
		Action: auditlog.ActionUpdate, Description: fmt.Sprintf("Risk signal %s on %s #%d: %s", cur.SignalCode, cur.EntityType, cur.EntityID, action),
		OldValue: map[string]any{"status": cur.Status, "is_hold": cur.IsHold, "risk_level": cur.RiskLevel}, NewValue: map[string]any{"action": action, "note": note},
		IPAddress: actor.IPAddress, DeviceInfo: actor.UserAgent})
	s, err := h.find(r, id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "risk_error", "saved; could not read it back")
		return
	}
	response.OK(w, map[string]any{"signal": s})
}

// requestVerification asks the partner concerned for documents or details.
func (h *Handler) requestVerification(r *http.Request, s Signal) {
	var userID int64
	var err error
	switch s.EntityType {
	case "USER", "DRIVER":
		userID = s.EntityID
	case "NURSERY":
		err = h.db.QueryRowContext(r.Context(), `SELECT owner_user_id FROM public.nurseries WHERE nursery_id = $1`, s.EntityID).Scan(&userID)
	case "ORDER":
		err = h.db.QueryRowContext(r.Context(), `SELECT n.owner_user_id FROM public.orders o JOIN public.nurseries n ON n.nursery_id = o.seller_nursery_id
			WHERE o.order_id = $1`, s.EntityID).Scan(&userID)
	}
	if err != nil || userID == 0 {
		return
	}
	_ = notifyqueue.EnqueueOrStore(r.Context(), h.redis, h.db, notifyqueue.Event{UserID: userID, Type: "VERIFICATION_REQUESTED",
		Title: "GreenRoot needs a few details", Message: "Please contact GreenRoot support to verify recent activity on your account. Your trades continue meanwhile.",
		Data: map[string]any{"entity": "support"}})
}

// ScanNow runs the detector at once (admins, and tests).
func (h *Handler) ScanNow(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.admin(w, r); !ok {
		return
	}
	n, err := Scan(r.Context(), h.db)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "risk_error", "scan failed")
		return
	}
	response.OK(w, map[string]any{"raised": n})
}

func (h *Handler) ListRules(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.admin(w, r); !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT signal_code, is_enabled, threshold, risk_level, description, updated_at FROM public.risk_rules ORDER BY signal_code`)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "risk_error", "could not read rules")
		return
	}
	defer rows.Close()
	out := make([]Rule, 0)
	for rows.Next() {
		var x Rule
		if err := rows.Scan(&x.SignalCode, &x.IsEnabled, &x.Threshold, &x.RiskLevel, &x.Description, &x.UpdatedAt); err != nil {
			response.Error(w, http.StatusInternalServerError, "risk_error", "could not read rules")
			return
		}
		out = append(out, x)
	}
	if rows.Err() != nil {
		response.Error(w, http.StatusInternalServerError, "risk_error", "could not read rules")
		return
	}
	response.OK(w, map[string]any{"rules": out})
}

type ruleRequest struct {
	IsEnabled bool    `json:"is_enabled"`
	Threshold float64 `json:"threshold"`
	RiskLevel string  `json:"risk_level"`
}

// UpdateRule changes a rule's threshold, level or on/off (super admin).
// A rule never yields CONFIRMED_FRAUD: that is an admin's finding.
func (h *Handler) UpdateRule(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.admin(w, r)
	if !ok {
		return
	}
	if !actor.HasRole("SUPER_ADMIN") {
		response.Error(w, http.StatusForbidden, "forbidden", "only a super admin can change risk rules")
		return
	}
	var req ruleRequest
	if !response.DecodeJSON(w, r, &req) {
		return
	}
	level := strings.ToUpper(req.RiskLevel)
	if req.Threshold <= 0 || (level != "LOW" && level != "MEDIUM" && level != "HIGH") {
		response.Error(w, http.StatusBadRequest, "invalid_input", "threshold must be positive; level LOW, MEDIUM or HIGH")
		return
	}
	code := strings.ToUpper(chi.URLParam(r, "code"))
	if code == "PRICE_OUTLIER" && level != "LOW" {
		response.Error(w, http.StatusBadRequest, "invalid_input", "an unusual price alone stays LOW: it is never evidence of fraud by itself")
		return
	}
	res, err := h.db.ExecContext(r.Context(), `UPDATE public.risk_rules SET is_enabled = $2, threshold = $3, risk_level = $4,
		updated_by_user_id = $5, updated_at = CURRENT_TIMESTAMP WHERE signal_code = $1`, code, req.IsEnabled, req.Threshold, level, actor.UserID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "risk_error", "could not save the rule")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		response.Error(w, http.StatusNotFound, "not_found", "unknown rule")
		return
	}
	h.audit.Log(r.Context(), auditlog.Entry{UserID: actor.UserID, Module: auditlog.ModuleRisk, EntityType: "risk_rule", Action: auditlog.ActionUpdate,
		Description: "Risk rule " + code + " changed", NewValue: req, IPAddress: actor.IPAddress, DeviceInfo: actor.UserAgent})
	response.OK(w, map[string]any{"message": "rule saved"})
}
