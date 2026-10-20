package sourcing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/location"
)

var ErrNotFound = apperrs.ErrNotFound

type Repository interface {
	// Network membership
	GetMember(ctx context.Context, nurseryID int64) (*Member, error)
	JoinNetwork(ctx context.Context, nurseryID int64, userID int64, req JoinNetworkRequest) (*Member, error)
	LeaveNetwork(ctx context.Context, nurseryID int64) error

	// Nearby discovery
	ListNearby(ctx context.Context, q NearbyQuery) ([]NearbyNursery, int64, error)
	GetNurseryProfile(ctx context.Context, nurseryID int64) (*NearbyNursery, error)

	// Auth helpers
	IsNurseryMember(ctx context.Context, nurseryID int64, userID int64) (bool, error)
	IsNurseryOwner(ctx context.Context, nurseryID int64, userID int64) (bool, error)
}

type PostgresRepository struct{ db *sql.DB }

func NewRepository(db *sql.DB) Repository { return &PostgresRepository{db: db} }

// ---- Network membership ----

func (r *PostgresRepository) GetMember(ctx context.Context, nurseryID int64) (*Member, error) {
	const q = `
		SELECT snm.member_id, snm.nursery_id, n.nursery_name, snm.is_active,
			snm.is_road_accessible, snm.is_lorry_accessible, snm.is_contact_visible,
			snm.service_radius_km, snm.joined_at, snm.updated_at
		FROM public.sourcing_network_members snm
		JOIN public.nurseries n ON n.nursery_id = snm.nursery_id
		WHERE snm.nursery_id = $1
	`
	m, err := scanMember(r.db.QueryRowContext(ctx, q, nurseryID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

func (r *PostgresRepository) JoinNetwork(ctx context.Context, nurseryID int64, userID int64, req JoinNetworkRequest) (*Member, error) {
	radius := req.ServiceRadiusKM
	if radius <= 0 {
		radius = 50
	}
	const q = `
		INSERT INTO public.sourcing_network_members
			(nursery_id, is_active, is_road_accessible, is_lorry_accessible, is_contact_visible, service_radius_km, joined_by_user_id)
		VALUES ($1, true, $2, $3, $4, $5, $6)
		ON CONFLICT (nursery_id) DO UPDATE
			SET is_active = true, is_road_accessible = $2, is_lorry_accessible = $3,
				is_contact_visible = $4, service_radius_km = $5, updated_at = CURRENT_TIMESTAMP,
				deactivated_at = NULL
		RETURNING member_id
	`
	var id int64
	if err := r.db.QueryRowContext(ctx, q, nurseryID, req.RoadAccessible, req.LorryAccessible, req.ContactVisible, radius, userID).Scan(&id); err != nil {
		return nil, err
	}
	return r.GetMember(ctx, nurseryID)
}

func (r *PostgresRepository) LeaveNetwork(ctx context.Context, nurseryID int64) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE public.sourcing_network_members SET is_active = false, deactivated_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE nursery_id = $1 AND is_active = true`,
		nurseryID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- Nearby discovery ----

func (r *PostgresRepository) ListNearby(ctx context.Context, q NearbyQuery) ([]NearbyNursery, int64, error) {
	args := []any{}
	clauses := []string{"snm.is_active = true"}

	// Plant name filter (matches against the nursery's top items)
	if q.PlantName != "" {
		args = append(args, "%"+q.PlantName+"%")
		clauses = append(clauses, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM public.nursery_top_items ti
			JOIN public.plants p ON p.plant_id = ti.plant_id
			WHERE ti.nursery_id = snm.nursery_id
			  AND (p.scientific_name ILIKE $%d OR p.common_name ILIKE $%d)
		)`, len(args), len(args)))
	}

	where := "WHERE " + strings.Join(clauses, " AND ")

	var distanceExpr string
	if q.Latitude != nil && q.Longitude != nil {
		args = append(args, *q.Longitude, *q.Latitude) // lon first for ST_MakePoint
		lonIdx, latIdx := len(args)-1, len(args)
		distanceExpr = location.DistanceKM("na.location", lonIdx, latIdx)
	} else {
		distanceExpr = "NULL::float8"
	}

	// #nosec G201 -- WHERE/ORDER BY fragments are fixed strings with $n placeholders (ORDER BY from an allowlist); values are bound args.
	countQ := fmt.Sprintf(`
		SELECT COUNT(DISTINCT snm.nursery_id)
		FROM public.sourcing_network_members snm
		JOIN public.nurseries n ON n.nursery_id = snm.nursery_id
		LEFT JOIN public.nursery_addresses na ON na.nursery_id = snm.nursery_id AND na.is_primary = true
		%s`, where)
	var total int64
	if err := r.db.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	page, perPage := q.Page, q.PerPage
	if page <= 0 {
		page = 1
	}
	if perPage <= 0 || perPage > 50 {
		perPage = 20
	}
	offset := (page - 1) * perPage
	args = append(args, perPage, offset)

	// #nosec G201 -- WHERE/ORDER BY fragments are fixed strings with $n placeholders (ORDER BY from an allowlist); values are bound args.
	// DISTINCT ON needs ORDER BY nursery_id, so the nearest-first order is
	// applied outside it.
	dataQ := fmt.Sprintf(`
		SELECT * FROM (
			SELECT DISTINCT ON (snm.nursery_id)
				snm.nursery_id, n.nursery_name, na.city, %s AS distance_km,
				snm.is_road_accessible, snm.is_lorry_accessible,
				CASE WHEN snm.is_contact_visible THEN n.mobile ELSE NULL END AS contact_number
			FROM public.sourcing_network_members snm
			JOIN public.nurseries n ON n.nursery_id = snm.nursery_id
			LEFT JOIN public.nursery_addresses na ON na.nursery_id = snm.nursery_id AND na.is_primary = true
			%s
			ORDER BY snm.nursery_id, distance_km ASC NULLS LAST
		) nearby
		ORDER BY distance_km ASC NULLS LAST, nursery_id
		LIMIT $%d OFFSET $%d
	`, distanceExpr, where, len(args)-1, len(args))

	rows, err := r.db.QueryContext(ctx, dataQ, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var nurseries []NearbyNursery
	for rows.Next() {
		var nn NearbyNursery
		var village sql.NullString
		var dist sql.NullFloat64
		var contact sql.NullString
		if err := rows.Scan(&nn.NurseryID, &nn.NurseryName, &village, &dist, &nn.RoadAccessible, &nn.LorryAccessible, &contact); err != nil {
			return nil, 0, err
		}
		if village.Valid {
			nn.Village = &village.String
		}
		if dist.Valid {
			nn.DistanceKM = &dist.Float64
		}
		if contact.Valid {
			nn.ContactNumber = &contact.String
		}
		nurseries = append(nurseries, nn)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if nurseries == nil {
		return []NearbyNursery{}, total, nil
	}
	// Top 5 items of every nursery on this page in one query.
	ids := make([]int64, len(nurseries))
	for i, nn := range nurseries {
		ids[i] = nn.NurseryID
	}
	items, err := r.topItems(ctx, ids, 5)
	if err != nil {
		return nil, 0, err
	}
	for i := range nurseries {
		nurseries[i].TopItems = items[nurseries[i].NurseryID]
	}
	return nurseries, total, nil
}

func (r *PostgresRepository) GetNurseryProfile(ctx context.Context, nurseryID int64) (*NearbyNursery, error) {
	const q = `
		SELECT snm.nursery_id, n.nursery_name, na.city,
			snm.is_road_accessible, snm.is_lorry_accessible,
			CASE WHEN snm.is_contact_visible THEN n.mobile ELSE NULL END
		FROM public.sourcing_network_members snm
		JOIN public.nurseries n ON n.nursery_id = snm.nursery_id
		LEFT JOIN public.nursery_addresses na ON na.nursery_id = snm.nursery_id AND na.is_primary = true
		WHERE snm.nursery_id = $1 AND snm.is_active = true
	`
	var nn NearbyNursery
	var village, contact sql.NullString
	if err := r.db.QueryRowContext(ctx, q, nurseryID).Scan(
		&nn.NurseryID, &nn.NurseryName, &village,
		&nn.RoadAccessible, &nn.LorryAccessible, &contact,
	); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if village.Valid {
		nn.Village = &village.String
	}
	if contact.Valid {
		nn.ContactNumber = &contact.String
	}
	items, err := r.topItems(ctx, []int64{nurseryID}, 20)
	if err != nil {
		return nil, err
	}
	nn.TopItems = items[nurseryID]
	return &nn, nil
}

// topItems returns up to perNursery top items for each nursery, in display
// order; every requested nursery gets a non-nil slice.
func (r *PostgresRepository) topItems(ctx context.Context, nurseryIDs []int64, perNursery int) (map[int64][]TopItem, error) {
	out := make(map[int64][]TopItem, len(nurseryIDs))
	for _, id := range nurseryIDs {
		out[id] = []TopItem{}
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT nursery_id, plant_id, plant_name, size_name, unit_price, quality_notes, photos
		FROM (
			SELECT ti.nursery_id, ti.plant_id, COALESCE(p.common_name, p.scientific_name) AS plant_name,
				ps.display_name AS size_name, ti.unit_price, ti.quality_notes, ti.photos,
				row_number() OVER (PARTITION BY ti.nursery_id ORDER BY ti.display_order, ti.top_item_id) AS rank
			FROM public.nursery_top_items ti
			JOIN public.plants p ON p.plant_id = ti.plant_id
			JOIN public.plant_sizes ps ON ps.size_id = ti.size_id
			WHERE ti.nursery_id = ANY($1)
		) ranked
		WHERE rank <= $2
		ORDER BY nursery_id, rank`, nurseryIDs, perNursery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var nurseryID int64
		var item TopItem
		var price sql.NullFloat64
		var notes sql.NullString
		var photos []byte
		if err := rows.Scan(&nurseryID, &item.PlantID, &item.PlantName, &item.SizeName, &price, &notes, &photos); err != nil {
			return nil, err
		}
		if price.Valid {
			item.UnitPrice = &price.Float64
		}
		item.QualityNotes = nullStr(notes)
		if err := json.Unmarshal(photos, &item.Photos); err != nil || item.Photos == nil {
			item.Photos = []string{}
		}
		out[nurseryID] = append(out[nurseryID], item)
	}
	return out, rows.Err()
}

// ---- Sourcing posts ----

// ---- Post responses ----

// ---- Auth helpers ----

func (r *PostgresRepository) IsNurseryMember(ctx context.Context, nurseryID int64, userID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM public.nursery_users
		WHERE nursery_id = $1 AND user_id = $2 AND COALESCE(is_active, true) = true
		UNION ALL
		SELECT 1 FROM public.nurseries
		WHERE nursery_id = $1 AND owner_user_id = $2 AND COALESCE(status::text,'') <> 'DELETED'
	)`, nurseryID, userID).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) IsNurseryOwner(ctx context.Context, nurseryID int64, userID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM public.nurseries WHERE nursery_id = $1 AND owner_user_id = $2)`,
		nurseryID, userID).Scan(&exists)
	return exists, err
}

// ---- SQL helpers ----

// ---- Scan helpers ----

func scanMember(row interface{ Scan(...any) error }) (*Member, error) {
	var m Member
	err := row.Scan(&m.ID, &m.NurseryID, &m.NurseryName, &m.IsActive,
		&m.RoadAccessible, &m.LorryAccessible, &m.ContactVisible,
		&m.ServiceRadiusKM, &m.JoinedAt, &m.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func nullStr(s sql.NullString) *string {
	if !s.Valid || s.String == "" {
		return nil
	}
	return &s.String
}
