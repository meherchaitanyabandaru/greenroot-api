package plants

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
)

// Admin browsing of all ~1.66 M botanical names (Admin → Plants) and their
// families (Admin → Categories). Each row says whether its species is in the
// catalog, so admins see the whole reference and what nurseries trade.

// BotanicalRow is one botanical name with its catalog link, if any.
type BotanicalRow struct {
	BotanicalName
	Genus            *string `json:"genus,omitempty"`
	CatalogPlantID   *int64  `json:"catalog_plant_id,omitempty"`
	CatalogPlantCode *string `json:"catalog_plant_code,omitempty"`
	// EnglishName is the catalog plant's common name; WFO has no common names.
	EnglishName *string `json:"english_name,omitempty"`
	// SynonymCount: old names that point to this name (list rows only).
	SynonymCount int64 `json:"synonym_count,omitempty"`
	// MatchedName: the old name that matched the search, when the row's own
	// name did not (searching "Sansevieria" finds Dracaena trifasciata).
	MatchedName *string `json:"matched_name,omitempty"`
}

type ListBotanicalRequest struct {
	Page      int
	PerPage   int
	Search    string
	Family    string
	Status    string
	Rank      string
	InCatalog *bool
	// ShowSynonyms lists old names as rows of their own. Off by default: one
	// row per plant, and a search on an old name finds its accepted plant.
	ShowSynonyms bool
	SortBy       string
	SortOrder    string
}

type BotanicalListResponse struct {
	Names      []BotanicalRow `json:"names"`
	Pagination Pagination     `json:"pagination"`
}

type BotanicalFamily struct {
	Family    string `json:"family"`
	Names     int64  `json:"names"`
	Accepted  int64  `json:"accepted"`
	InCatalog int64  `json:"in_catalog"`
}

type BotanicalFamiliesResponse struct {
	Families   []BotanicalFamily `json:"families"`
	TotalNames int64             `json:"total_names"`
}

type BotanicalDetailResponse struct {
	Name         BotanicalRow   `json:"name"`
	Accepted     *BotanicalRow  `json:"accepted,omitempty"`
	Synonyms     []BotanicalRow `json:"synonyms"`
	SynonymCount int64          `json:"synonym_count"`
}

// ── repository ──────────────────────────────────────────────────────────────

// catalogPlantJoin finds the active catalog plant of row b's species.
const catalogPlantJoin = `
	LEFT JOIN public.botanical_names a ON a.wfo_id = b.accepted_wfo_id
	LEFT JOIN LATERAL (
		SELECT p.plant_id, p.plant_code, p.common_name
		FROM public.plants p
		LEFT JOIN public.botanical_names cb ON cb.wfo_id = p.wfo_id
		WHERE p.wfo_id IS NOT NULL AND p.is_active
			AND COALESCE(cb.accepted_wfo_id, p.wfo_id) = COALESCE(b.accepted_wfo_id, b.wfo_id)
		ORDER BY p.plant_id
		LIMIT 1
	) cp ON true`

const botanicalRowColumns = `b.wfo_id, b.scientific_name, b.authorship, b.family, b.genus, b.taxon_rank, b.status,
	b.accepted_wfo_id, a.scientific_name, cp.plant_id, cp.plant_code, cp.common_name`

const catalogSpecies = `(
	SELECT COALESCE(cb.accepted_wfo_id, p.wfo_id)
	FROM public.plants p
	LEFT JOIN public.botanical_names cb ON cb.wfo_id = p.wfo_id
	WHERE p.wfo_id IS NOT NULL AND p.is_active)`

func scanBotanicalRow(row interface{ Scan(...any) error }) (BotanicalRow, error) {
	var r BotanicalRow
	var authorship, family, genus, acceptedID, acceptedName, code, english sql.NullString
	var plantID sql.NullInt64
	if err := row.Scan(&r.WfoID, &r.ScientificName, &authorship, &family, &genus, &r.TaxonRank, &r.Status,
		&acceptedID, &acceptedName, &plantID, &code, &english); err != nil {
		return BotanicalRow{}, err
	}
	r.Authorship, r.Family, r.Genus = nullableString(authorship), nullableString(family), nullableString(genus)
	r.AcceptedWfoID, r.AcceptedName, r.CatalogPlantCode = nullableString(acceptedID), nullableString(acceptedName), nullableString(code)
	r.EnglishName = nullableString(english)
	if plantID.Valid {
		r.CatalogPlantID = &plantID.Int64
	}
	return r, nil
}

func (r *PostgresRepository) queryBotanicalRows(ctx context.Context, query string, args ...any) ([]BotanicalRow, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]BotanicalRow, 0)
	for rows.Next() {
		row, err := scanBotanicalRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) ListBotanical(ctx context.Context, in ListBotanicalRequest) ([]BotanicalRow, int64, error) {
	clauses := []string{"true"}
	args := []any{}
	add := func(clause string, v any) {
		args = append(args, v)
		clauses = append(clauses, fmt.Sprintf(clause, len(args)))
	}
	collapse := !in.ShowSynonyms && in.Status == ""
	if collapse {
		clauses = append(clauses, "b.status <> 'SYNONYM'")
	}
	searchArg, speciesArg := 0, 0
	if in.Search != "" {
		// Species whose catalog plant has a matching English or local name
		// ("neem" → Azadirachta indica), plus, when old names are hidden,
		// species whose old names match. Fetched first as short lists so every
		// OR branch below uses an index.
		species, err := r.speciesByEnglishName(ctx, escapeLike(in.Search))
		if err != nil {
			return nil, 0, err
		}
		synWhole, synPartial := []string{}, []string{}
		if collapse {
			if synWhole, synPartial, err = r.speciesBySynonymName(ctx, escapeLike(in.Search)); err != nil {
				return nil, 0, err
			}
		}
		args = append(args, escapeLike(in.Search), species, synWhole, synPartial)
		searchArg, speciesArg = len(args)-3, len(args)-2
		clauses = append(clauses, fmt.Sprintf(`(public.botanical_search_key(b.scientific_name) LIKE '%%' || public.botanical_search_key($%d) || '%%'
			OR b.wfo_id = ANY($%[2]d::text[]) OR b.accepted_wfo_id = ANY($%[2]d::text[])
			OR b.wfo_id = ANY($%[3]d::text[]) OR b.wfo_id = ANY($%[4]d::text[]))`, searchArg, speciesArg, speciesArg+1, speciesArg+2))
	}
	if in.Family != "" {
		add(`b.family = $%d`, in.Family)
	}
	if in.Status != "" {
		add(`b.status = $%d`, in.Status)
	}
	if in.Rank != "" {
		add(`b.taxon_rank = $%d`, in.Rank)
	}
	if in.InCatalog != nil {
		op := "IN"
		if !*in.InCatalog {
			op = "NOT IN"
		}
		clauses = append(clauses, "COALESCE(b.accepted_wfo_id, b.wfo_id) "+op+" "+catalogSpecies)
	}
	where := " WHERE " + strings.Join(clauses, " AND ")

	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM public.botanical_names b`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	dir := "ASC"
	if in.SortOrder == "desc" {
		dir = "DESC"
	}
	order := "b.scientific_name " + dir + ", b.wfo_id " + dir
	switch {
	case in.SortBy == "family":
		// Default NULLS placement matches idx_botanical_names_family in both directions.
		order = "b.family " + dir + ", b.scientific_name " + dir + ", b.wfo_id " + dir
	case searchArg > 0 && in.SortBy == "":
		// Searching without an explicit sort: best match first, as in the pickers.
		key := fmt.Sprintf("public.botanical_search_key($%d)", searchArg)
		order = fmt.Sprintf(`CASE
			WHEN b.wfo_id IN (SELECT unnest($%[2]d::text[])) THEN -2
			WHEN b.accepted_wfo_id IN (SELECT unnest($%[2]d::text[])) THEN -1
			WHEN ' ' || public.botanical_search_key(b.scientific_name) || ' ' LIKE '%% ' || %[1]s || ' %%' THEN 0
			WHEN b.wfo_id IN (SELECT unnest($%[3]d::text[])) THEN 0
			WHEN public.botanical_search_key(b.scientific_name) LIKE %[1]s || '%%' THEN 1
			ELSE 2 END,
			COALESCE(b.accepted_wfo_id, b.wfo_id) IN %[4]s DESC,
			(b.status = 'ACCEPTED') DESC, length(b.scientific_name), b.scientific_name, b.wfo_id`, key, speciesArg, speciesArg+1, catalogSpecies)
	}
	args = append(args, in.PerPage, (in.Page-1)*in.PerPage)
	// Two steps: pick the page's ids by index order, then load those rows with
	// their catalog link. Joining first made the lookup run for every skipped
	// row (page 20,000 took 8 s).
	// #nosec G201 -- WHERE uses $n placeholders; ORDER BY is built from fixed strings.
	idRows, err := r.db.QueryContext(ctx, fmt.Sprintf(`SELECT b.wfo_id FROM public.botanical_names b %s ORDER BY %s LIMIT $%d OFFSET $%d`,
		where, order, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer idRows.Close()
	ids := make([]string, 0, in.PerPage)
	for idRows.Next() {
		var id string
		if err := idRows.Scan(&id); err != nil {
			return nil, 0, err
		}
		ids = append(ids, id)
	}
	if err := idRows.Err(); err != nil {
		return nil, 0, err
	}
	rows, err := r.queryBotanicalRows(ctx, `SELECT `+botanicalRowColumns+` FROM public.botanical_names b `+catalogPlantJoin+` WHERE b.wfo_id = ANY($1::text[])`, ids)
	if err != nil {
		return nil, 0, err
	}
	byID := make(map[string]BotanicalRow, len(rows))
	for _, row := range rows {
		byID[row.WfoID] = row
	}
	if err := r.addSynonymInfo(ctx, byID, ids, in.Search); err != nil {
		return nil, 0, err
	}
	names := make([]BotanicalRow, 0, len(ids))
	for _, id := range ids {
		names = append(names, byID[id])
	}
	return names, total, nil
}

// maxSynonymSpecies caps a broad search ("ana") so the id list stays small.
const maxSynonymSpecies = 5000

// speciesBySynonymName returns accepted names whose old names match term,
// split by whether an old name matches it as a whole word ("sansevieria" in
// "Sansevieria trifasciata"), which ranks like a direct whole-word match.
func (r *PostgresRepository) speciesBySynonymName(ctx context.Context, term string) (whole, partial []string, err error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT accepted_wfo_id,
			bool_or(' ' || public.botanical_search_key(scientific_name) || ' ' LIKE '% ' || public.botanical_search_key($1) || ' %')
		FROM public.botanical_names
		WHERE accepted_wfo_id IS NOT NULL
			AND public.botanical_search_key(scientific_name) LIKE '%' || public.botanical_search_key($1) || '%'
		GROUP BY accepted_wfo_id
		LIMIT $2`, term, maxSynonymSpecies)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	whole, partial = []string{}, []string{}
	for rows.Next() {
		var id string
		var isWhole bool
		if err := rows.Scan(&id, &isWhole); err != nil {
			return nil, nil, err
		}
		if isWhole {
			whole = append(whole, id)
		} else {
			partial = append(partial, id)
		}
	}
	return whole, partial, rows.Err()
}

// addSynonymInfo sets each row's old-name count and, when the search matched
// an old name rather than the row's own name, that old name.
func (r *PostgresRepository) addSynonymInfo(ctx context.Context, byID map[string]BotanicalRow, ids []string, search string) error {
	rows, err := r.db.QueryContext(ctx, `
		SELECT accepted_wfo_id, count(*),
			min(scientific_name) FILTER (WHERE $2 <> '' AND public.botanical_search_key(scientific_name) LIKE '%' || public.botanical_search_key($2) || '%')
		FROM public.botanical_names
		WHERE accepted_wfo_id = ANY($1::text[])
		GROUP BY accepted_wfo_id`, ids, escapeLike(search))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var count int64
		var matched sql.NullString
		if err := rows.Scan(&id, &count, &matched); err != nil {
			return err
		}
		row := byID[id]
		row.SynonymCount = count
		if matched.Valid && !strings.Contains(searchKey(row.ScientificName), searchKey(search)) {
			row.MatchedName = &matched.String
		}
		byID[id] = row
	}
	return rows.Err()
}

// searchKey mirrors public.botanical_search_key for comparisons in Go.
func searchKey(s string) string {
	return strings.ToLower(strings.NewReplacer("× ", "", "×", "").Replace(s))
}

func (r *PostgresRepository) speciesByEnglishName(ctx context.Context, term string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT COALESCE(cb.accepted_wfo_id, p.wfo_id)
		FROM public.plants p
		LEFT JOIN public.botanical_names cb ON cb.wfo_id = p.wfo_id
		WHERE p.wfo_id IS NOT NULL AND p.is_active AND p.common_name ILIKE '%' || $1 || '%'`, term)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	species := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		species = append(species, id)
	}
	return species, rows.Err()
}

func (r *PostgresRepository) BotanicalFamilies(ctx context.Context) ([]BotanicalFamily, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT COALESCE(b.family, ''), count(*), count(*) FILTER (WHERE b.status = 'ACCEPTED'),
			count(*) FILTER (WHERE b.wfo_id IN `+catalogSpecies+`)
		FROM public.botanical_names b
		GROUP BY 1
		ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]BotanicalFamily, 0, 800)
	for rows.Next() {
		var f BotanicalFamily
		if err := rows.Scan(&f.Family, &f.Names, &f.Accepted, &f.InCatalog); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) GetBotanical(ctx context.Context, wfoID string) (BotanicalDetailResponse, error) {
	rows, err := r.queryBotanicalRows(ctx, `SELECT `+botanicalRowColumns+` FROM public.botanical_names b `+catalogPlantJoin+` WHERE b.wfo_id = $1`, wfoID)
	if err != nil {
		return BotanicalDetailResponse{}, err
	}
	if len(rows) == 0 {
		return BotanicalDetailResponse{}, ErrNotFound
	}
	detail := BotanicalDetailResponse{Name: rows[0]}
	species := rows[0].WfoID
	if rows[0].AcceptedWfoID != nil {
		species = *rows[0].AcceptedWfoID
		accepted, err := r.queryBotanicalRows(ctx, `SELECT `+botanicalRowColumns+` FROM public.botanical_names b `+catalogPlantJoin+` WHERE b.wfo_id = $1`, species)
		if err != nil {
			return BotanicalDetailResponse{}, err
		}
		if len(accepted) == 1 {
			detail.Accepted = &accepted[0]
		}
	}
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM public.botanical_names WHERE accepted_wfo_id = $1`, species).Scan(&detail.SynonymCount); err != nil {
		return BotanicalDetailResponse{}, err
	}
	detail.Synonyms, err = r.queryBotanicalRows(ctx, `SELECT `+botanicalRowColumns+` FROM public.botanical_names b `+catalogPlantJoin+`
		WHERE b.accepted_wfo_id = $1 ORDER BY b.scientific_name, b.wfo_id LIMIT 50`, species)
	return detail, err
}

// ── service ─────────────────────────────────────────────────────────────────

var botanicalStatuses = map[string]bool{"ACCEPTED": true, "SYNONYM": true, "UNCHECKED": true}

func (s *Service) ListBotanical(ctx context.Context, actor ActorContext, in ListBotanicalRequest) ([]BotanicalRow, Pagination, error) {
	if !canManagePlants(actor) {
		return nil, Pagination{}, ErrForbidden
	}
	if in.Page <= 0 {
		in.Page = 1
	}
	if in.PerPage <= 0 {
		in.PerPage = 20
	}
	if in.PerPage > 100 {
		in.PerPage = 100
	}
	in.Search = strings.Join(strings.Fields(in.Search), " ")
	in.Family = strings.TrimSpace(in.Family)
	in.Rank = strings.ToLower(strings.TrimSpace(in.Rank))
	in.Status = strings.ToUpper(strings.TrimSpace(in.Status))
	if in.Status != "" && !botanicalStatuses[in.Status] {
		return nil, Pagination{}, ErrInvalidInput
	}
	names, total, err := s.repository.ListBotanical(ctx, in)
	if err != nil {
		return nil, Pagination{}, err
	}
	return names, Pagination{Page: in.Page, PerPage: in.PerPage, Total: total, TotalPages: totalPages(total, in.PerPage)}, nil
}

func (s *Service) BotanicalFamilies(ctx context.Context, actor ActorContext) (BotanicalFamiliesResponse, error) {
	if !canManagePlants(actor) {
		return BotanicalFamiliesResponse{}, ErrForbidden
	}
	families, err := s.repository.BotanicalFamilies(ctx)
	if err != nil {
		return BotanicalFamiliesResponse{}, err
	}
	resp := BotanicalFamiliesResponse{Families: families}
	for _, f := range families {
		resp.TotalNames += f.Names
	}
	return resp, nil
}

func (s *Service) GetBotanical(ctx context.Context, actor ActorContext, wfoID string) (BotanicalDetailResponse, error) {
	if !canManagePlants(actor) {
		return BotanicalDetailResponse{}, ErrForbidden
	}
	return s.repository.GetBotanical(ctx, strings.TrimSpace(wfoID))
}

// ── handlers ────────────────────────────────────────────────────────────────

// ListBotanical godoc
//
//	@Summary	Browse all botanical names (admin)
//	@Tags		Plants
//	@Security	BearerAuth
//	@Success	200	{object}	BotanicalListResponse
//	@Router		/api/v1/botanical-names [get]
func (h *Handler) ListBotanical(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	in := ListBotanicalRequest{
		Page: intQuery(q.Get("page")), PerPage: intQuery(q.Get("per_page")),
		Search: q.Get("search"), Family: q.Get("family"), Status: q.Get("status"), Rank: q.Get("rank"),
		SortBy: q.Get("sort_by"), SortOrder: strings.ToLower(q.Get("sort_order")),
		ShowSynonyms: q.Get("show_synonyms") == "true",
	}
	if v := q.Get("in_catalog"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			response.Error(w, http.StatusBadRequest, "invalid_input", "in_catalog must be true or false")
			return
		}
		in.InCatalog = &b
	}
	names, pagination, err := h.service.ListBotanical(r.Context(), actor, in)
	if err != nil {
		writePlantsError(w, err)
		return
	}
	response.OK(w, BotanicalListResponse{Names: names, Pagination: pagination})
}

// BotanicalFamilies godoc
//
//	@Summary	Plant families with name counts (admin)
//	@Tags		Plants
//	@Security	BearerAuth
//	@Success	200	{object}	BotanicalFamiliesResponse
//	@Router		/api/v1/botanical-names/families [get]
func (h *Handler) BotanicalFamilies(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	resp, err := h.service.BotanicalFamilies(r.Context(), actor)
	if err != nil {
		writePlantsError(w, err)
		return
	}
	response.OK(w, resp)
}

// GetBotanical godoc
//
//	@Summary	One botanical name with its accepted name and synonyms (admin)
//	@Tags		Plants
//	@Security	BearerAuth
//	@Success	200	{object}	BotanicalDetailResponse
//	@Router		/api/v1/botanical-names/{wfoId} [get]
func (h *Handler) GetBotanical(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	detail, err := h.service.GetBotanical(r.Context(), actor, chi.URLParam(r, "wfoId"))
	if errors.Is(err, ErrNotFound) {
		response.Error(w, http.StatusNotFound, "not_found", "botanical name not found")
		return
	}
	if err != nil {
		writePlantsError(w, err)
		return
	}
	response.OK(w, detail)
}
