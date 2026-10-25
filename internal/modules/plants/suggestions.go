package plants

// Community plant suggestions: a user who cannot find a plant suggests it;
// an admin approves (after adding it with the normal plant form), links it to
// an existing plant, or rejects it. Users never write the plants master data.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
)

const (
	suggestionsPerDay   = 10
	SuggestionPending   = "PENDING"
	SuggestionApproved  = "APPROVED" // added to the catalog as a new plant
	SuggestionLinked    = "LINKED"   // another name for an existing plant
	SuggestionRejected  = "REJECTED"
	suggestionNotifType = "PLANT_SUGGESTION"
)

var (
	ErrSuggestionLimit    = errors.New("too many plant suggestions today")
	ErrSuggestionReviewed = errors.New("plant suggestion already reviewed")
)

type PlantSuggestion struct {
	ID              int64      `json:"id"`
	Name            string     `json:"name"`
	ScientificName  *string    `json:"scientific_name,omitempty"`
	Notes           *string    `json:"notes,omitempty"`
	Status          string     `json:"status"`
	SuggestedByID   int64      `json:"suggested_by_user_id"`
	SuggestedByName string     `json:"suggested_by_name,omitempty"`
	SuggestedByMob  string     `json:"suggested_by_mobile,omitempty"`
	PlantID         *int64     `json:"plant_id,omitempty"`
	PlantName       *string    `json:"plant_name,omitempty"`
	ReviewNote      *string    `json:"review_note,omitempty"`
	ReviewedAt      *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type CreateSuggestionRequest struct {
	Name           string `json:"name"`
	ScientificName string `json:"scientific_name"`
	Notes          string `json:"notes"`
}

// Result: SUBMITTED (new), ALREADY_SUGGESTED (an open one has this name) or
// EXISTS (the catalog has it: Plant says which).
type CreateSuggestionResponse struct {
	Result     string           `json:"result"`
	Suggestion *PlantSuggestion `json:"suggestion,omitempty"`
	Plant      *Plant           `json:"plant,omitempty"`
}

type ReviewSuggestionRequest struct {
	Action  string `json:"action"` // APPROVED | LINKED | REJECTED
	PlantID *int64 `json:"plant_id"`
	Note    string `json:"note"`
}

type SuggestionsResponse struct {
	Suggestions []PlantSuggestion `json:"suggestions"`
	Total       int64             `json:"total"`
}

type Suggestions struct {
	db     *sql.DB
	plants *Service
}

func (s *Suggestions) Create(ctx context.Context, actor ActorContext, in CreateSuggestionRequest) (CreateSuggestionResponse, error) {
	name := strings.Join(strings.Fields(in.Name), " ")
	sci := strings.Join(strings.Fields(in.ScientificName), " ")
	notes := strings.TrimSpace(in.Notes)
	if len(name) < 2 || len(name) > 100 || len(sci) > 255 || len(notes) > 500 {
		return CreateSuggestionResponse{}, ErrInvalidInput
	}

	// Already in the catalog: nothing for an admin to review.
	var plantID int64
	err := s.db.QueryRowContext(ctx, `
		SELECT plant_id FROM public.plants
		WHERE is_active AND (lower(scientific_name) = lower($1) OR lower(common_name) = lower($1)
			OR ($2 <> '' AND lower(scientific_name) = lower($2)))
		ORDER BY plant_id LIMIT 1`, name, sci).Scan(&plantID)
	if err == nil {
		plant, err := s.plants.repository.FindByID(ctx, plantID)
		if err != nil {
			return CreateSuggestionResponse{}, err
		}
		return CreateSuggestionResponse{Result: "EXISTS", Plant: plant}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return CreateSuggestionResponse{}, err
	}

	var today int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM public.plant_suggestions
		WHERE suggested_by_user_id = $1 AND created_at > now() - interval '1 day'`, actor.UserID).Scan(&today); err != nil {
		return CreateSuggestionResponse{}, err
	}
	if today >= suggestionsPerDay {
		return CreateSuggestionResponse{}, ErrSuggestionLimit
	}

	var id int64
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO public.plant_suggestions (name, scientific_name, notes, suggested_by_user_id)
		VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), $4)
		ON CONFLICT (lower(name)) WHERE status = 'PENDING' DO NOTHING
		RETURNING suggestion_id`, name, sci, notes, actor.UserID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return CreateSuggestionResponse{Result: "ALREADY_SUGGESTED"}, nil
	}
	if err != nil {
		return CreateSuggestionResponse{}, err
	}
	sg, err := s.get(ctx, id)
	if err != nil {
		return CreateSuggestionResponse{}, err
	}
	return CreateSuggestionResponse{Result: "SUBMITTED", Suggestion: &sg}, nil
}

const suggestionSelect = `
	SELECT s.suggestion_id, s.name, s.scientific_name, s.notes, s.status, s.suggested_by_user_id,
		COALESCE(NULLIF(TRIM(CONCAT_WS(' ', u.first_name, u.last_name)), ''), ''), COALESCE(u.mobile, ''),
		s.plant_id, COALESCE(p.common_name, p.scientific_name), s.review_note, s.reviewed_at, s.created_at
	FROM public.plant_suggestions s
	JOIN public.users u ON u.user_id = s.suggested_by_user_id
	LEFT JOIN public.plants p ON p.plant_id = s.plant_id`

func scanSuggestion(row interface{ Scan(...any) error }) (PlantSuggestion, error) {
	var sg PlantSuggestion
	err := row.Scan(&sg.ID, &sg.Name, &sg.ScientificName, &sg.Notes, &sg.Status, &sg.SuggestedByID,
		&sg.SuggestedByName, &sg.SuggestedByMob, &sg.PlantID, &sg.PlantName, &sg.ReviewNote, &sg.ReviewedAt, &sg.CreatedAt)
	return sg, err
}

func (s *Suggestions) get(ctx context.Context, id int64) (PlantSuggestion, error) {
	sg, err := scanSuggestion(s.db.QueryRowContext(ctx, suggestionSelect+` WHERE s.suggestion_id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return sg, ErrNotFound
	}
	return sg, err
}

// List is the admin review queue (status "" = all), newest first.
func (s *Suggestions) List(ctx context.Context, actor ActorContext, status string, page, perPage int) (SuggestionsResponse, error) {
	if !canManagePlants(actor) {
		return SuggestionsResponse{}, ErrForbidden
	}
	status = strings.ToUpper(strings.TrimSpace(status))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 50
	}
	out := SuggestionsResponse{Suggestions: []PlantSuggestion{}}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM public.plant_suggestions WHERE $1 = '' OR status = $1`, status).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := s.db.QueryContext(ctx, suggestionSelect+`
		WHERE $1 = '' OR s.status = $1
		ORDER BY s.created_at DESC, s.suggestion_id DESC LIMIT $2 OFFSET $3`, status, perPage, (page-1)*perPage)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		sg, err := scanSuggestion(rows)
		if err != nil {
			return out, err
		}
		out.Suggestions = append(out.Suggestions, sg)
	}
	return out, rows.Err()
}

// Review closes a pending suggestion and tells the person who sent it.
// APPROVED/LINKED name the plant; a plant with no common name gets the
// suggested one. REJECTED needs a reason.
func (s *Suggestions) Review(ctx context.Context, actor ActorContext, id int64, in ReviewSuggestionRequest) (PlantSuggestion, error) {
	if !canManagePlants(actor) {
		return PlantSuggestion{}, ErrForbidden
	}
	action := strings.ToUpper(strings.TrimSpace(in.Action))
	note := strings.TrimSpace(in.Note)
	switch action {
	case SuggestionApproved, SuggestionLinked:
		if in.PlantID == nil || *in.PlantID <= 0 {
			return PlantSuggestion{}, ErrInvalidInput
		}
	case SuggestionRejected:
		if note == "" {
			return PlantSuggestion{}, ErrInvalidInput
		}
		in.PlantID = nil
	default:
		return PlantSuggestion{}, ErrInvalidInput
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PlantSuggestion{}, err
	}
	defer tx.Rollback()

	var name, status string
	err = tx.QueryRowContext(ctx, `SELECT name, status FROM public.plant_suggestions WHERE suggestion_id = $1 FOR UPDATE`, id).Scan(&name, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return PlantSuggestion{}, ErrNotFound
	}
	if err != nil {
		return PlantSuggestion{}, err
	}
	if status != SuggestionPending {
		return PlantSuggestion{}, ErrSuggestionReviewed
	}

	var plantName string
	if in.PlantID != nil {
		err := tx.QueryRowContext(ctx, `SELECT COALESCE(NULLIF(common_name, ''), scientific_name) FROM public.plants WHERE plant_id = $1 AND is_active`, *in.PlantID).Scan(&plantName)
		if errors.Is(err, sql.ErrNoRows) {
			return PlantSuggestion{}, ErrNotFound
		}
		if err != nil {
			return PlantSuggestion{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE public.plants SET common_name = $2, updated_at = CURRENT_TIMESTAMP
			WHERE plant_id = $1 AND COALESCE(common_name, '') = ''`, *in.PlantID, name); err != nil {
			return PlantSuggestion{}, err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE public.plant_suggestions
		SET status = $2, plant_id = $3, review_note = NULLIF($4, ''), reviewed_by_user_id = $5,
			reviewed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE suggestion_id = $1`, id, action, in.PlantID, note, actor.UserID); err != nil {
		return PlantSuggestion{}, err
	}

	var suggester int64
	if err := tx.QueryRowContext(ctx, `SELECT suggested_by_user_id FROM public.plant_suggestions WHERE suggestion_id = $1`, id).Scan(&suggester); err != nil {
		return PlantSuggestion{}, err
	}
	title, msg := "Thanks for your plant suggestion", fmt.Sprintf("%q is now in GreenRoot as %s.", name, plantName)
	if action == SuggestionRejected {
		title, msg = "About your plant suggestion", fmt.Sprintf("%q was not added: %s", name, note)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO public.notifications (user_id, notification_type, title, message, channel, status)
		VALUES ($1, $2, $3, $4, 'IN_APP', 'PENDING')`, suggester, suggestionNotifType, title, msg); err != nil {
		return PlantSuggestion{}, err
	}
	if err := tx.Commit(); err != nil {
		return PlantSuggestion{}, err
	}

	if in.PlantID != nil {
		s.plants.invalidateSearch(ctx)
	}
	s.plants.auditSvc.Log(ctx, auditlog.Entry{
		UserID: actor.UserID, Module: auditlog.ModulePlants, EntityType: auditlog.EntityPlantSuggest, EntityID: id,
		Action: auditlog.ActionUpdate, Description: fmt.Sprintf("Plant suggestion %q %s", name, strings.ToLower(action)),
		NewValue: map[string]any{"status": action, "plant_id": in.PlantID}, IPAddress: actor.IPAddress, DeviceInfo: actor.UserAgent,
	})
	return s.get(ctx, id)
}

// ── HTTP ─────────────────────────────────────────────────────────────────────

func (h *Handler) CreateSuggestion(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req CreateSuggestionRequest
	if !response.DecodeJSON(w, r, &req) {
		return
	}
	out, err := h.suggestions.Create(r.Context(), actor, req)
	if err != nil {
		writeSuggestionError(w, err)
		return
	}
	status := http.StatusOK
	if out.Result == "SUBMITTED" {
		status = http.StatusCreated
	}
	response.JSON(w, status, out)
}

func (h *Handler) ListSuggestions(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	out, err := h.suggestions.List(r.Context(), actor, q.Get("status"), intQuery(q.Get("page")), intQuery(q.Get("per_page")))
	if err != nil {
		writeSuggestionError(w, err)
		return
	}
	response.OK(w, out)
}

func (h *Handler) ReviewSuggestion(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "suggestionId")
	if !ok {
		return
	}
	var req ReviewSuggestionRequest
	if !response.DecodeJSON(w, r, &req) {
		return
	}
	sg, err := h.suggestions.Review(r.Context(), actor, id, req)
	if err != nil {
		writeSuggestionError(w, err)
		return
	}
	response.OK(w, map[string]any{"suggestion": sg})
}

func writeSuggestionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrSuggestionLimit):
		response.Error(w, http.StatusTooManyRequests, "suggestion_limit", fmt.Sprintf("you can suggest up to %d plants a day — try again tomorrow", suggestionsPerDay))
	case errors.Is(err, ErrSuggestionReviewed):
		response.Error(w, http.StatusConflict, "suggestion_reviewed", "this suggestion was already reviewed")
	case errors.Is(err, ErrInvalidInput):
		response.Error(w, http.StatusBadRequest, "invalid_input", "invalid plant suggestion")
	default:
		writePlantsError(w, err)
	}
}
