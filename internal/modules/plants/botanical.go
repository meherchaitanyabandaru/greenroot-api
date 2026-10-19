package plants

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/publiccode"
)

// Botanical names (public.botanical_names, ~1.66 M World Flora Online rows)
// back the plant pickers: a name that is not in the catalog yet becomes a
// catalog plant the first time someone picks it (ResolveBotanical), so nobody
// has to enter master data by hand.
//
// A species is identified by its accepted WFO id: a synonym and its accepted
// name are the same plant, so the catalog never holds both.

// pickableRanks excludes genus, family and other groupings: you cannot stock "Ficus".
var pickableRanks = []string{"species", "subspecies", "variety", "form", "subvariety", "subform"}

// minBotanicalQuery keeps 1-2 letter keystrokes off the 1.66 M-row index.
const minBotanicalQuery = 3

func (r *PostgresRepository) BotanicalNameExists(ctx context.Context, wfoID string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM public.botanical_names WHERE wfo_id = $1)`, wfoID).Scan(&exists)
	return exists, err
}

// LookupBotanical returns pickable names matching query that are not in the
// catalog yet, one row per species, best match first.
func (r *PostgresRepository) LookupBotanical(ctx context.Context, query string, limit int) ([]BotanicalName, error) {
	const q = `
		WITH term AS (SELECT public.botanical_search_key($1) AS k),
		catalog_species AS (
			SELECT COALESCE(cb.accepted_wfo_id, p.wfo_id) AS species
			FROM public.plants p
			LEFT JOIN public.botanical_names cb ON cb.wfo_id = p.wfo_id
			WHERE p.wfo_id IS NOT NULL
		),
		matches AS (
			SELECT DISTINCT ON (COALESCE(b.accepted_wfo_id, b.wfo_id))
				b.wfo_id, b.scientific_name, b.authorship, b.family, b.taxon_rank, b.status,
				b.accepted_wfo_id, a.scientific_name AS accepted_name,
				-- rank: whole word ("cumini" in "Syzygium cumini") > name prefix > word prefix > anywhere
				CASE
					WHEN ' ' || public.botanical_search_key(b.scientific_name) || ' ' LIKE '% ' || term.k || ' %' THEN 0
					WHEN public.botanical_search_key(b.scientific_name) LIKE term.k || '%' THEN 1
					WHEN ' ' || public.botanical_search_key(b.scientific_name) LIKE '% ' || term.k || '%' THEN 2
					ELSE 3
				END AS match_rank
			FROM public.botanical_names b
			CROSS JOIN term
			LEFT JOIN public.botanical_names a ON a.wfo_id = b.accepted_wfo_id
			WHERE public.botanical_search_key(b.scientific_name) LIKE '%' || term.k || '%'
				AND b.taxon_rank = ANY($2::text[])
				AND COALESCE(b.accepted_wfo_id, b.wfo_id) NOT IN (SELECT species FROM catalog_species)
			ORDER BY COALESCE(b.accepted_wfo_id, b.wfo_id), match_rank, (b.status = 'ACCEPTED') DESC, length(b.scientific_name)
		)
		SELECT wfo_id, scientific_name, authorship, family, taxon_rank, status, accepted_wfo_id, accepted_name
		FROM matches
		ORDER BY match_rank, (status = 'ACCEPTED') DESC, (status = 'SYNONYM') DESC, (taxon_rank = 'species') DESC, length(scientific_name), scientific_name
		LIMIT $3`
	rows, err := r.db.QueryContext(ctx, q, escapeLike(query), pickableRanks, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := make([]BotanicalName, 0, limit)
	for rows.Next() {
		var n BotanicalName
		var authorship, family, acceptedID, acceptedName sql.NullString
		if err := rows.Scan(&n.WfoID, &n.ScientificName, &authorship, &family, &n.TaxonRank, &n.Status, &acceptedID, &acceptedName); err != nil {
			return nil, err
		}
		n.Authorship, n.Family = nullableString(authorship), nullableString(family)
		n.AcceptedWfoID, n.AcceptedName = nullableString(acceptedID), nullableString(acceptedName)
		names = append(names, n)
	}
	return names, rows.Err()
}

// ResolveBotanical returns the catalog plant for the species of wfoID,
// creating it when the catalog has none. In order it reuses: a plant linked to
// this name or its species, then an unlinked plant with the same scientific
// name (which it links). The advisory lock serialises resolves of one species,
// so concurrent pickers never create two plants.
func (r *PostgresRepository) ResolveBotanical(ctx context.Context, wfoID string) (int64, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()

	var name, species, speciesName string
	err = tx.QueryRowContext(ctx, `
		SELECT b.scientific_name, COALESCE(b.accepted_wfo_id, b.wfo_id), COALESCE(a.scientific_name, b.scientific_name)
		FROM public.botanical_names b
		LEFT JOIN public.botanical_names a ON a.wfo_id = b.accepted_wfo_id
		WHERE b.wfo_id = $1`, wfoID).Scan(&name, &species, &speciesName)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, ErrNotFound
	}
	if err != nil {
		return 0, false, err
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('plants.resolve:' || $1))`, species); err != nil {
		return 0, false, err
	}

	var plantID int64
	var active bool
	err = tx.QueryRowContext(ctx, `
		SELECT p.plant_id, p.is_active
		FROM public.plants p
		LEFT JOIN public.botanical_names b ON b.wfo_id = p.wfo_id
		WHERE p.wfo_id = $1 OR p.wfo_id = $2 OR b.accepted_wfo_id = $2
		ORDER BY p.is_active DESC, (p.wfo_id = $1) DESC, (p.wfo_id = $2) DESC, p.plant_id
		LIMIT 1`, wfoID, species).Scan(&plantID, &active)
	if err == nil {
		if !active {
			return 0, false, ErrPlantRemoved
		}
		return plantID, false, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}

	// A plant entered by hand before the names were loaded: link it.
	err = tx.QueryRowContext(ctx, `
		UPDATE public.plants SET wfo_id = $1, updated_at = CURRENT_TIMESTAMP
		WHERE plant_id = (
			SELECT plant_id FROM public.plants
			WHERE wfo_id IS NULL AND is_active AND lower(scientific_name) IN (lower($2), lower($3))
			ORDER BY plant_id LIMIT 1)
		RETURNING plant_id`, species, speciesName, name).Scan(&plantID)
	if err == nil {
		return plantID, false, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}

	code, err := publiccode.Next(ctx, tx, publiccode.Plants, time.Now())
	if err != nil {
		return 0, false, err
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO public.plants (plant_code, scientific_name, wfo_id, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING plant_id`, code, speciesName, species).Scan(&plantID)
	if err != nil {
		return 0, false, err
	}
	return plantID, true, tx.Commit()
}

// escapeLike makes user input match literally inside LIKE '%…%'.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
