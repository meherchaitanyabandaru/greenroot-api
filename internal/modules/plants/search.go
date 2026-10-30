package plants

import (
	"context"
	"database/sql"
	"strings"
)

// Plant search for people who know a plant by a local name, a common or
// scientific name, a nursery's own name or with a typo. All matching is on
// public.plant_search_key (case, punctuation and spacing ignored, any
// script); near spellings use trigram similarity. Nothing is ever picked
// for the user: a fuzzy match is only offered, and "Other / not listed"
// stays available in the apps.

// minFuzzyQuery: shorter queries have too few trigrams to compare usefully.
const minFuzzyQuery = 4

// CatalogMatch is a catalog plant that matched, and how.
type CatalogMatch struct {
	PlantID     int64
	MatchType   string // NAME, ALIAS or FUZZY
	MatchedName *string
}

// LookupCatalog finds catalog plants by name or alias: exact, then prefix,
// word prefix, anywhere, then near spellings; the best match per plant.
func (r *PostgresRepository) LookupCatalog(ctx context.Context, query string, limit int) ([]CatalogMatch, error) {
	const q = `
		WITH term AS (SELECT public.plant_search_key($1) AS k),
		hits AS (
			SELECT p.plant_id, NULL::text AS matched, public.plant_search_key(p.common_name) AS key
			FROM public.plants p, term
			WHERE p.is_active AND (public.plant_search_key(p.common_name) LIKE '%' || term.k || '%'
				OR (length(term.k) >= $3 AND public.plant_search_key(p.common_name) % term.k))
			UNION ALL
			SELECT p.plant_id, NULL::text, public.plant_search_key(p.scientific_name)
			FROM public.plants p, term
			WHERE p.is_active AND (public.plant_search_key(p.scientific_name) LIKE '%' || term.k || '%'
				OR (length(term.k) >= $3 AND public.plant_search_key(p.scientific_name) % term.k))
			UNION ALL
			SELECT a.plant_id, a.name, a.search_key
			FROM public.plant_aliases a
			JOIN public.plants p ON p.plant_id = a.plant_id AND p.is_active, term
			WHERE a.is_active AND (a.search_key LIKE '%' || term.k || '%'
				OR (length(term.k) >= $3 AND a.search_key % term.k))
		),
		ranked AS (
			SELECT DISTINCT ON (h.plant_id) h.plant_id, h.matched,
				CASE
					WHEN h.key = t.k THEN 0
					WHEN h.key LIKE t.k || '%' THEN 1
					WHEN ' ' || h.key LIKE '% ' || t.k || '%' THEN 2
					WHEN h.key LIKE '%' || t.k || '%' THEN 3
					ELSE 4
				END AS match_rank,
				similarity(h.key, t.k) AS sim
			FROM hits h, term t
			WHERE t.k <> '' AND h.key <> ''
			ORDER BY h.plant_id, match_rank, sim DESC, h.matched NULLS FIRST
		)
		SELECT plant_id, matched, match_rank FROM ranked
		ORDER BY match_rank, sim DESC, plant_id
		LIMIT $2`
	rows, err := r.db.QueryContext(ctx, q, query, limit, minFuzzyQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]CatalogMatch, 0, limit)
	for rows.Next() {
		var m CatalogMatch
		var matched sql.NullString
		var rank int
		if err := rows.Scan(&m.PlantID, &matched, &rank); err != nil {
			return nil, err
		}
		switch {
		case rank == 4:
			m.MatchType = "FUZZY"
		case matched.Valid:
			m.MatchType = "ALIAS"
		default:
			m.MatchType = "NAME"
		}
		if matched.Valid {
			m.MatchedName = &matched.String
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// LookupBotanicalFuzzy offers WFO names spelt like the query ("mangifra
// indca") when the substring search found too few; uses the trigram index.
func (r *PostgresRepository) LookupBotanicalFuzzy(ctx context.Context, query string, limit int) ([]BotanicalName, error) {
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
				similarity(public.botanical_search_key(b.scientific_name), term.k) AS sim
			FROM public.botanical_names b
			CROSS JOIN term
			LEFT JOIN public.botanical_names a ON a.wfo_id = b.accepted_wfo_id
			WHERE public.botanical_search_key(b.scientific_name) % term.k
				AND public.botanical_search_key(b.scientific_name) NOT LIKE '%' || term.k || '%'
				AND b.taxon_rank = ANY($2::text[])
				AND COALESCE(b.accepted_wfo_id, b.wfo_id) NOT IN (SELECT species FROM catalog_species)
			ORDER BY COALESCE(b.accepted_wfo_id, b.wfo_id), sim DESC, (b.status = 'ACCEPTED') DESC
		)
		SELECT wfo_id, scientific_name, authorship, family, taxon_rank, status, accepted_wfo_id, accepted_name
		FROM matches
		ORDER BY sim DESC, (status = 'ACCEPTED') DESC, length(scientific_name), scientific_name
		LIMIT $3`
	rows, err := r.db.QueryContext(ctx, q, query, pickableRanks, limit)
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

// botanicalKey dedupes WFO's duplicate records (same name and author).
func botanicalKey(n BotanicalName) string {
	key := n.ScientificName
	if a := n.Authorship; a != nil {
		key += "|" + strings.TrimSpace(*a)
	}
	return key
}
