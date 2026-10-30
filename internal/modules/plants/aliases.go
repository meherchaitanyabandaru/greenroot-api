package plants

// Other names of catalog plants: local/regional names, common names,
// synonyms, trade names and frequent misspellings. Search matches them; only
// admins maintain them. A name may belong to two plants (local names are not
// unique), so duplicates are shown, never merged automatically.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/database/pgerr"
)

var (
	ErrAliasExists = errors.New("this plant already has that name")
	aliasTypes     = map[string]bool{"COMMON": true, "LOCAL": true, "SYNONYM": true, "TRADE": true, "MISSPELLING": true}
)

type PlantAlias struct {
	ID           int64     `json:"id"`
	PlantID      int64     `json:"plant_id"`
	PlantName    string    `json:"plant_name"`
	Name         string    `json:"name"`
	AliasType    string    `json:"alias_type"`
	Language     *string   `json:"language,omitempty"`
	Region       *string   `json:"region,omitempty"`
	Source       string    `json:"source"`
	SuggestionID *int64    `json:"plant_suggestion_id,omitempty"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	// OtherPlants: other plants that have an active alias with the same name.
	OtherPlants []int64 `json:"other_plant_ids"`
}

type AliasRequest struct {
	Name      string  `json:"name"`
	AliasType string  `json:"alias_type"`
	Language  *string `json:"language"`
	Region    *string `json:"region"`
	IsActive  *bool   `json:"is_active"` // update only: false retires a wrong or duplicate alias
}

type Aliases struct {
	db     *sql.DB
	plants *Service
}

const aliasSelect = `
	SELECT a.plant_alias_id, a.plant_id, COALESCE(NULLIF(p.common_name, ''), p.scientific_name), a.name, a.alias_type,
		a.language, a.region, a.source, a.plant_suggestion_id, a.is_active, a.created_at,
		COALESCE((SELECT jsonb_agg(o.plant_id ORDER BY o.plant_id) FROM public.plant_aliases o
			WHERE o.is_active AND o.search_key = a.search_key AND o.plant_id <> a.plant_id), '[]')
	FROM public.plant_aliases a
	JOIN public.plants p ON p.plant_id = a.plant_id`

func scanAlias(row interface{ Scan(...any) error }) (PlantAlias, error) {
	var a PlantAlias
	var others []byte
	err := row.Scan(&a.ID, &a.PlantID, &a.PlantName, &a.Name, &a.AliasType, &a.Language, &a.Region, &a.Source,
		&a.SuggestionID, &a.IsActive, &a.CreatedAt, &others)
	if err == nil {
		err = json.Unmarshal(others, &a.OtherPlants)
	}
	return a, err
}

func normalizeAlias(in *AliasRequest) error {
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	in.AliasType = strings.ToUpper(strings.TrimSpace(in.AliasType))
	if in.AliasType == "" {
		in.AliasType = "LOCAL"
	}
	if len(in.Name) < 2 || len(in.Name) > 255 || !aliasTypes[in.AliasType] {
		return ErrInvalidInput
	}
	for _, f := range []**string{&in.Language, &in.Region} {
		if *f != nil {
			v := strings.TrimSpace(**f)
			if v == "" {
				*f = nil
			} else if len(v) > 80 {
				return ErrInvalidInput
			} else {
				*f = &v
			}
		}
	}
	return nil
}

// List: one plant's aliases (plantID > 0) or a search over all (admin).
func (s *Aliases) List(ctx context.Context, actor ActorContext, plantID int64, search string, page, perPage int) ([]PlantAlias, int64, error) {
	if !canManagePlants(actor) {
		return nil, 0, ErrForbidden
	}
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 50
	}
	where := ` WHERE ($1 = 0 OR a.plant_id = $1) AND ($2 = '' OR a.search_key LIKE '%' || public.plant_search_key($2) || '%'
		OR public.plant_search_key(p.scientific_name) LIKE '%' || public.plant_search_key($2) || '%')`
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM public.plant_aliases a JOIN public.plants p ON p.plant_id = a.plant_id`+where,
		plantID, strings.TrimSpace(search)).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, aliasSelect+where+` ORDER BY a.is_active DESC, a.name, a.plant_alias_id LIMIT $3 OFFSET $4`,
		plantID, strings.TrimSpace(search), perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]PlantAlias, 0)
	for rows.Next() {
		a, err := scanAlias(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

func (s *Aliases) get(ctx context.Context, id int64) (PlantAlias, error) {
	a, err := scanAlias(s.db.QueryRowContext(ctx, aliasSelect+` WHERE a.plant_alias_id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func (s *Aliases) Create(ctx context.Context, actor ActorContext, plantID int64, in AliasRequest) (PlantAlias, error) {
	if !canManagePlants(actor) {
		return PlantAlias{}, ErrForbidden
	}
	if err := normalizeAlias(&in); err != nil {
		return PlantAlias{}, err
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO public.plant_aliases (plant_id, name, alias_type, language, region, source, created_by_user_id)
		SELECT plant_id, $2, $3, $4, $5, 'ADMIN', $6 FROM public.plants WHERE plant_id = $1 AND is_active
		RETURNING plant_alias_id`, plantID, in.Name, in.AliasType, in.Language, in.Region, actor.UserID).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return PlantAlias{}, ErrNotFound
	case pgerr.Constraint(err) == "uq_plant_aliases_active":
		return PlantAlias{}, ErrAliasExists
	case err != nil:
		return PlantAlias{}, err
	}
	s.plants.invalidateSearch(ctx)
	s.audit(ctx, actor, id, auditlog.ActionCreate, fmt.Sprintf("Plant #%d also called %q (%s)", plantID, in.Name, in.AliasType), nil, in)
	return s.get(ctx, id)
}

// Update edits an alias or retires it (is_active false), e.g. a wrong mapping
// or a duplicate. Quotations keep the names they were made with.
func (s *Aliases) Update(ctx context.Context, actor ActorContext, id int64, in AliasRequest) (PlantAlias, error) {
	if !canManagePlants(actor) {
		return PlantAlias{}, ErrForbidden
	}
	if err := normalizeAlias(&in); err != nil {
		return PlantAlias{}, err
	}
	old, err := s.get(ctx, id)
	if err != nil {
		return PlantAlias{}, err
	}
	active := old.IsActive
	if in.IsActive != nil {
		active = *in.IsActive
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE public.plant_aliases SET name = $2, alias_type = $3, language = $4, region = $5, is_active = $6,
			updated_at = CURRENT_TIMESTAMP
		WHERE plant_alias_id = $1`, id, in.Name, in.AliasType, in.Language, in.Region, active)
	if pgerr.Constraint(err) == "uq_plant_aliases_active" {
		return PlantAlias{}, ErrAliasExists
	}
	if err != nil {
		return PlantAlias{}, err
	}
	s.plants.invalidateSearch(ctx)
	s.audit(ctx, actor, id, auditlog.ActionUpdate, fmt.Sprintf("Plant alias %q updated (active %v)", in.Name, active), old, in)
	return s.get(ctx, id)
}

func (s *Aliases) audit(ctx context.Context, actor ActorContext, id int64, action auditlog.Action, desc string, oldV, newV any) {
	s.plants.auditSvc.Log(ctx, auditlog.Entry{
		UserID: actor.UserID, Module: auditlog.ModulePlants, EntityType: "plant_alias", EntityID: id,
		Action: action, Description: desc, OldValue: oldV, NewValue: newV, IPAddress: actor.IPAddress, DeviceInfo: actor.UserAgent,
	})
}

// addSuggestionAliasTx makes a linked customer name searchable for its plant,
// replacing the alias an earlier (wrong) link of the same suggestion made.
func addSuggestionAliasTx(ctx context.Context, tx *sql.Tx, suggestionID, plantID, actorID int64, name string) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE public.plant_aliases SET is_active = false, updated_at = CURRENT_TIMESTAMP
		WHERE plant_suggestion_id = $1 AND plant_id <> $2 AND is_active`, suggestionID, plantID); err != nil {
		return err
	}
	// Not when it is just the plant's own name.
	_, err := tx.ExecContext(ctx, `
		INSERT INTO public.plant_aliases (plant_id, name, alias_type, source, plant_suggestion_id, created_by_user_id)
		SELECT p.plant_id, $3::varchar, 'LOCAL', 'SUGGESTION', $1, $4 FROM public.plants p
		WHERE p.plant_id = $2 AND public.plant_search_key($3::varchar) NOT IN
			(public.plant_search_key(p.common_name), public.plant_search_key(p.scientific_name))
		ON CONFLICT (plant_id, search_key) WHERE is_active DO NOTHING`, suggestionID, plantID, name, actorID)
	return err
}

// ── HTTP ─────────────────────────────────────────────────────────────────────

func (h *Handler) ListAliases(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var plantID int64
	if chi.URLParam(r, "id") != "" {
		if plantID, ok = pathID(w, r, "id"); !ok {
			return
		}
	}
	q := r.URL.Query()
	aliases, total, err := h.aliases.List(r.Context(), actor, plantID, q.Get("search"), intQuery(q.Get("page")), intQuery(q.Get("per_page")))
	if err != nil {
		writeAliasError(w, err)
		return
	}
	response.OK(w, map[string]any{"aliases": aliases, "total": total})
}

func (h *Handler) CreateAlias(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	plantID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var in AliasRequest
	if !response.DecodeJSON(w, r, &in) {
		return
	}
	a, err := h.aliases.Create(r.Context(), actor, plantID, in)
	if err != nil {
		writeAliasError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, map[string]any{"alias": a})
}

func (h *Handler) UpdateAlias(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "aliasId")
	if !ok {
		return
	}
	var in AliasRequest
	if !response.DecodeJSON(w, r, &in) {
		return
	}
	a, err := h.aliases.Update(r.Context(), actor, id, in)
	if err != nil {
		writeAliasError(w, err)
		return
	}
	response.OK(w, map[string]any{"alias": a})
}

func writeAliasError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrAliasExists):
		response.Error(w, http.StatusConflict, "alias_exists", "this plant already has that name")
	default:
		writePlantsError(w, err)
	}
}
