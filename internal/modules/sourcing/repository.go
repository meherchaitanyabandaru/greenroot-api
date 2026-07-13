package sourcing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/location"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/publiccode"
)

var ErrNotFound = errors.New("not found")

type Repository interface {
	// Network membership
	GetMember(ctx context.Context, nurseryID int64) (*Member, error)
	JoinNetwork(ctx context.Context, nurseryID int64, userID int64, req JoinNetworkRequest) (*Member, error)
	LeaveNetwork(ctx context.Context, nurseryID int64) error

	// Nearby discovery
	ListNearby(ctx context.Context, q NearbyQuery) ([]NearbyNursery, int64, error)
	GetNurseryProfile(ctx context.Context, nurseryID int64) (*NearbyNursery, error)

	// Featured plants
	ListFeaturedPlants(ctx context.Context, nurseryID int64) ([]FeaturedPlant, error)
	AddFeaturedPlant(ctx context.Context, nurseryID int64, userID int64, req CreateFeaturedPlantRequest) (*FeaturedPlant, error)
	UpdateFeaturedPlant(ctx context.Context, featuredID int64, req UpdateFeaturedPlantRequest) (*FeaturedPlant, error)
	DeleteFeaturedPlant(ctx context.Context, featuredID int64) error

	// Sourcing posts
	ListPosts(ctx context.Context, q ListPostsQuery) ([]SourcingPost, int64, error)
	GetPost(ctx context.Context, postID int64) (*SourcingPost, error)
	CreatePost(ctx context.Context, userID int64, req CreatePostRequest) (*SourcingPost, error)
	UpdatePost(ctx context.Context, postID int64, req UpdatePostRequest) (*SourcingPost, error)
	DeletePost(ctx context.Context, postID int64) error

	// Post responses
	ListResponses(ctx context.Context, postID int64) ([]PostResponse, error)
	CreateResponse(ctx context.Context, postID int64, userID int64, req CreateResponseRequest) (*PostResponse, error)
	UpdateResponse(ctx context.Context, responseID int64, req UpdateResponseRequest) (*PostResponse, error)

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
			snm.road_accessible, snm.lorry_accessible, snm.contact_visible,
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
			(nursery_id, is_active, road_accessible, lorry_accessible, contact_visible, service_radius_km, joined_by_user_id)
		VALUES ($1, true, $2, $3, $4, $5, $6)
		ON CONFLICT (nursery_id) DO UPDATE
			SET is_active = true, road_accessible = $2, lorry_accessible = $3,
				contact_visible = $4, service_radius_km = $5, updated_at = CURRENT_TIMESTAMP,
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

	// Plant name filter (matches against featured plants)
	if q.PlantName != "" {
		args = append(args, "%"+q.PlantName+"%")
		clauses = append(clauses, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM public.nursery_featured_plants nfp
			JOIN public.plants p ON p.plant_id = nfp.plant_id
			WHERE nfp.nursery_id = snm.nursery_id AND nfp.is_active = true
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

	dataQ := fmt.Sprintf(`
		SELECT DISTINCT ON (snm.nursery_id)
			snm.nursery_id, n.nursery_name, na.city, %s AS distance_km,
			snm.road_accessible, snm.lorry_accessible,
			CASE WHEN snm.contact_visible THEN n.mobile ELSE NULL END AS contact_number
		FROM public.sourcing_network_members snm
		JOIN public.nurseries n ON n.nursery_id = snm.nursery_id
		LEFT JOIN public.nursery_addresses na ON na.nursery_id = snm.nursery_id AND na.is_primary = true
		%s
		ORDER BY snm.nursery_id, distance_km ASC NULLS LAST
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
		// Load top 5 featured plants for each nursery
		nn.FeaturedPlants, _ = r.listTopFeaturedPlants(ctx, nn.NurseryID, 5)
		nurseries = append(nurseries, nn)
	}
	if nurseries == nil {
		nurseries = []NearbyNursery{}
	}
	return nurseries, total, rows.Err()
}

func (r *PostgresRepository) GetNurseryProfile(ctx context.Context, nurseryID int64) (*NearbyNursery, error) {
	const q = `
		SELECT snm.nursery_id, n.nursery_name, na.city,
			snm.road_accessible, snm.lorry_accessible,
			CASE WHEN snm.contact_visible THEN n.mobile ELSE NULL END
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
	nn.FeaturedPlants, _ = r.listTopFeaturedPlants(ctx, nurseryID, 20)
	return &nn, nil
}

// ---- Featured plants ----

func (r *PostgresRepository) ListFeaturedPlants(ctx context.Context, nurseryID int64) ([]FeaturedPlant, error) {
	return r.listTopFeaturedPlants(ctx, nurseryID, 20)
}

func (r *PostgresRepository) listTopFeaturedPlants(ctx context.Context, nurseryID int64, limit int) ([]FeaturedPlant, error) {
	const q = `
		SELECT nfp.featured_id, nfp.nursery_id, nfp.plant_id,
			COALESCE(p.common_name, p.scientific_name) AS plant_name,
			nfp.display_order, nfp.approximate_quantity, nfp.approximate_size,
			nfp.quality_notes, nfp.photos, nfp.is_active, nfp.created_at, nfp.updated_at
		FROM public.nursery_featured_plants nfp
		JOIN public.plants p ON p.plant_id = nfp.plant_id
		WHERE nfp.nursery_id = $1 AND nfp.is_active = true
		ORDER BY nfp.display_order ASC
		LIMIT $2
	`
	rows, err := r.db.QueryContext(ctx, q, nurseryID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFeaturedPlants(rows)
}

func (r *PostgresRepository) AddFeaturedPlant(ctx context.Context, nurseryID int64, userID int64, req CreateFeaturedPlantRequest) (*FeaturedPlant, error) {
	order := req.DisplayOrder
	if order < 1 {
		order = 1
	}
	photos, _ := json.Marshal(req.Photos)
	const q = `
		INSERT INTO public.nursery_featured_plants
			(nursery_id, plant_id, display_order, approximate_quantity, approximate_size, quality_notes, photos, added_by_user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (nursery_id, plant_id) DO UPDATE
			SET display_order = $3, approximate_quantity = $4, approximate_size = $5,
				quality_notes = $6, photos = $7, is_active = true, updated_at = CURRENT_TIMESTAMP
		RETURNING featured_id
	`
	var id int64
	if err := r.db.QueryRowContext(ctx, q, nurseryID, req.PlantID, order,
		intPtrOrNil(req.ApproximateQuantity), strPtrOrNil(req.ApproximateSize),
		strPtrOrNil(req.QualityNotes), string(photos), userID,
	).Scan(&id); err != nil {
		return nil, err
	}
	return r.findFeaturedPlant(ctx, id)
}

func (r *PostgresRepository) UpdateFeaturedPlant(ctx context.Context, featuredID int64, req UpdateFeaturedPlantRequest) (*FeaturedPlant, error) {
	photos, _ := json.Marshal(req.Photos)
	result, err := r.db.ExecContext(ctx, `
		UPDATE public.nursery_featured_plants
		SET display_order = $2, approximate_quantity = $3, approximate_size = $4,
			quality_notes = $5, photos = $6, is_active = $7, updated_at = CURRENT_TIMESTAMP
		WHERE featured_id = $1`,
		featuredID, req.DisplayOrder, intPtrOrNil(req.ApproximateQuantity),
		strPtrOrNil(req.ApproximateSize), strPtrOrNil(req.QualityNotes), string(photos), req.IsActive,
	)
	if err != nil {
		return nil, err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return r.findFeaturedPlant(ctx, featuredID)
}

func (r *PostgresRepository) DeleteFeaturedPlant(ctx context.Context, featuredID int64) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE public.nursery_featured_plants SET is_active = false, updated_at = CURRENT_TIMESTAMP WHERE featured_id = $1`,
		featuredID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) findFeaturedPlant(ctx context.Context, id int64) (*FeaturedPlant, error) {
	const q = `
		SELECT nfp.featured_id, nfp.nursery_id, nfp.plant_id,
			COALESCE(p.common_name, p.scientific_name) AS plant_name,
			nfp.display_order, nfp.approximate_quantity, nfp.approximate_size,
			nfp.quality_notes, nfp.photos, nfp.is_active, nfp.created_at, nfp.updated_at
		FROM public.nursery_featured_plants nfp
		JOIN public.plants p ON p.plant_id = nfp.plant_id
		WHERE nfp.featured_id = $1
	`
	row := r.db.QueryRowContext(ctx, q, id)
	fp, err := scanFeaturedPlant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return fp, err
}

// ---- Sourcing posts ----

func (r *PostgresRepository) ListPosts(ctx context.Context, q ListPostsQuery) ([]SourcingPost, int64, error) {
	where, args := buildPostWhere(q)

	var total int64
	countQ := `SELECT COUNT(*) FROM public.sourcing_posts sp ` + where
	if err := r.db.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	page, perPage := q.Page, q.PerPage
	if page <= 0 {
		page = 1
	}
	if perPage <= 0 || perPage > 100 {
		perPage = 20
	}
	offset := (page - 1) * perPage
	args = append(args, perPage, offset)

	dataQ := fmt.Sprintf(`%s %s ORDER BY sp.post_id DESC LIMIT $%d OFFSET $%d`,
		postBaseSelect(), where, len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, dataQ, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var posts []SourcingPost
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, 0, err
		}
		posts = append(posts, p)
	}
	if posts == nil {
		posts = []SourcingPost{}
	}
	return posts, total, rows.Err()
}

func (r *PostgresRepository) GetPost(ctx context.Context, postID int64) (*SourcingPost, error) {
	p, err := scanPostRow(r.db.QueryRowContext(ctx, postBaseSelect()+` WHERE sp.post_id = $1 AND sp.deleted_at IS NULL`, postID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Photos, _ = r.listPostPhotos(ctx, postID)
	return p, nil
}

func (r *PostgresRepository) CreatePost(ctx context.Context, userID int64, req CreatePostRequest) (*SourcingPost, error) {
	now := time.Now()
	postCode, err := publiccode.Next(ctx, r.db, publiccode.SourcingPosts, now)
	if err != nil {
		return nil, err
	}
	radius := req.RadiusKM
	if radius <= 0 {
		radius = 50
	}
	urgency := strings.ToUpper(req.Urgency)
	if urgency == "" {
		urgency = "FLEXIBLE"
	}
	const q = `
		INSERT INTO public.sourcing_posts
			(post_code, nursery_id, posted_by_user_id, post_type, plant_id, plant_name,
			size_description, quantity, urgency, needed_by_date, notes, radius_km, status, expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 'OPEN', $13, $14, $14)
		RETURNING post_id
	`
	var id int64
	if err := r.db.QueryRowContext(ctx, q,
		postCode, req.NurseryID, userID, strings.ToUpper(req.PostType),
		req.PlantID, strings.TrimSpace(req.PlantName),
		strPtrOrNil(req.SizeDesc), intPtrOrNil(req.Quantity), urgency,
		parseDateOrNil(req.NeededByDate), strPtrOrNil(req.Notes), radius,
		parseTimeOrNil(req.ExpiresAt), now,
	).Scan(&id); err != nil {
		return nil, err
	}
	return r.GetPost(ctx, id)
}

func (r *PostgresRepository) UpdatePost(ctx context.Context, postID int64, req UpdatePostRequest) (*SourcingPost, error) {
	status := strings.ToUpper(strings.TrimSpace(req.Status))
	urgency := strings.ToUpper(strings.TrimSpace(req.Urgency))
	result, err := r.db.ExecContext(ctx, `
		UPDATE public.sourcing_posts
		SET plant_name = $2, size_description = $3, quantity = $4, urgency = $5, notes = $6,
			status = $7,
			closed_at = CASE WHEN $8 = 'CLOSED' THEN COALESCE(closed_at, CURRENT_TIMESTAMP) ELSE closed_at END,
			updated_at = CURRENT_TIMESTAMP
		WHERE post_id = $1 AND deleted_at IS NULL`,
		postID, strings.TrimSpace(req.PlantName), strPtrOrNil(req.SizeDesc),
		intPtrOrNil(req.Quantity), urgency, strPtrOrNil(req.Notes), status, status,
	)
	if err != nil {
		return nil, err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return r.GetPost(ctx, postID)
}

func (r *PostgresRepository) DeletePost(ctx context.Context, postID int64) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE public.sourcing_posts SET deleted_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE post_id = $1 AND deleted_at IS NULL`,
		postID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) listPostPhotos(ctx context.Context, postID int64) ([]PostPhoto, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT photo_id, post_id, photo_url, display_order, created_at FROM public.sourcing_post_photos WHERE post_id = $1 ORDER BY display_order ASC`,
		postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var photos []PostPhoto
	for rows.Next() {
		var ph PostPhoto
		if err := rows.Scan(&ph.ID, &ph.PostID, &ph.PhotoURL, &ph.DisplayOrder, &ph.CreatedAt); err != nil {
			return nil, err
		}
		photos = append(photos, ph)
	}
	return photos, rows.Err()
}

// ---- Post responses ----

func (r *PostgresRepository) ListResponses(ctx context.Context, postID int64) ([]PostResponse, error) {
	rows, err := r.db.QueryContext(ctx, responseBaseSelect()+` WHERE spr.post_id = $1 ORDER BY spr.response_id DESC`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var responses []PostResponse
	for rows.Next() {
		resp, err := scanResponseRow(rows)
		if err != nil {
			return nil, err
		}
		responses = append(responses, *resp)
	}
	if responses == nil {
		responses = []PostResponse{}
	}
	return responses, rows.Err()
}

func (r *PostgresRepository) CreateResponse(ctx context.Context, postID int64, userID int64, req CreateResponseRequest) (*PostResponse, error) {
	const q = `
		INSERT INTO public.sourcing_post_responses
			(post_id, responder_nursery_id, responded_by_user_id, available_quantity, notes, contact_info, status)
		VALUES ($1, $2, $3, $4, $5, $6, 'PENDING')
		ON CONFLICT (post_id, responder_nursery_id) DO UPDATE
			SET responded_by_user_id = $3, available_quantity = $4, notes = $5,
				contact_info = $6, status = 'PENDING', responded_at = CURRENT_TIMESTAMP
		RETURNING response_id
	`
	var id int64
	if err := r.db.QueryRowContext(ctx, q,
		postID, req.ResponderNurseryID, userID,
		intPtrOrNil(req.AvailableQuantity), strPtrOrNil(req.Notes), strPtrOrNil(req.ContactInfo),
	).Scan(&id); err != nil {
		return nil, err
	}
	// increment response_count on the post
	_, _ = r.db.ExecContext(ctx,
		`UPDATE public.sourcing_posts SET response_count = response_count + 1, updated_at = CURRENT_TIMESTAMP WHERE post_id = $1`,
		postID)
	return r.findResponse(ctx, id)
}

func (r *PostgresRepository) UpdateResponse(ctx context.Context, responseID int64, req UpdateResponseRequest) (*PostResponse, error) {
	result, err := r.db.ExecContext(ctx,
		`UPDATE public.sourcing_post_responses SET status = $2 WHERE response_id = $1`,
		responseID, strings.ToUpper(req.Status))
	if err != nil {
		return nil, err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return r.findResponse(ctx, responseID)
}

func (r *PostgresRepository) findResponse(ctx context.Context, id int64) (*PostResponse, error) {
	resp, err := scanResponseRow(r.db.QueryRowContext(ctx, responseBaseSelect()+` WHERE spr.response_id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return resp, err
}

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

func postBaseSelect() string {
	return `
		SELECT sp.post_id, sp.post_code, sp.nursery_id, n.nursery_name,
			sp.posted_by_user_id, u.first_name,
			sp.post_type, sp.plant_id, sp.plant_name, sp.size_description,
			sp.quantity, sp.urgency, sp.needed_by_date, sp.notes,
			sp.radius_km, sp.response_count, sp.status,
			sp.expires_at, sp.closed_at, sp.created_at, sp.updated_at
		FROM public.sourcing_posts sp
		JOIN public.nurseries n ON n.nursery_id = sp.nursery_id
		JOIN public.users u ON u.user_id = sp.posted_by_user_id
	`
}

func responseBaseSelect() string {
	return `
		SELECT spr.response_id, spr.post_id, spr.responder_nursery_id, n.nursery_name,
			spr.responded_by_user_id, u.first_name,
			spr.available_quantity, spr.notes, spr.contact_info, spr.status,
			spr.responded_at, spr.created_at
		FROM public.sourcing_post_responses spr
		JOIN public.nurseries n ON n.nursery_id = spr.responder_nursery_id
		JOIN public.users u ON u.user_id = spr.responded_by_user_id
	`
}

func buildPostWhere(q ListPostsQuery) (string, []any) {
	clauses := []string{"sp.deleted_at IS NULL"}
	args := []any{}
	if q.NurseryID > 0 {
		args = append(args, q.NurseryID)
		clauses = append(clauses, fmt.Sprintf("sp.nursery_id = $%d", len(args)))
	}
	if q.PostType != "" {
		args = append(args, strings.ToUpper(q.PostType))
		clauses = append(clauses, fmt.Sprintf("sp.post_type = $%d", len(args)))
	}
	if q.Status != "" {
		args = append(args, strings.ToUpper(q.Status))
		clauses = append(clauses, fmt.Sprintf("sp.status = $%d", len(args)))
	}
	if q.PlantName != "" {
		args = append(args, "%"+q.PlantName+"%")
		clauses = append(clauses, fmt.Sprintf("sp.plant_name ILIKE $%d", len(args)))
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

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

func scanFeaturedPlants(rows *sql.Rows) ([]FeaturedPlant, error) {
	var result []FeaturedPlant
	for rows.Next() {
		fp, err := scanFeaturedPlant(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *fp)
	}
	if result == nil {
		result = []FeaturedPlant{}
	}
	return result, rows.Err()
}

func scanFeaturedPlant(row interface{ Scan(...any) error }) (*FeaturedPlant, error) {
	var fp FeaturedPlant
	var photosJSON string
	var approxQty sql.NullInt32
	var approxSize, qualityNotes sql.NullString
	if err := row.Scan(&fp.ID, &fp.NurseryID, &fp.PlantID, &fp.PlantName,
		&fp.DisplayOrder, &approxQty, &approxSize, &qualityNotes,
		&photosJSON, &fp.IsActive, &fp.CreatedAt, &fp.UpdatedAt); err != nil {
		return nil, err
	}
	if approxQty.Valid {
		v := int(approxQty.Int32)
		fp.ApproximateQuantity = &v
	}
	fp.ApproximateSize = nullStr(approxSize)
	fp.QualityNotes = nullStr(qualityNotes)
	_ = json.Unmarshal([]byte(photosJSON), &fp.Photos)
	if fp.Photos == nil {
		fp.Photos = []string{}
	}
	return &fp, nil
}

func scanPost(row interface{ Scan(...any) error }) (SourcingPost, error) {
	var p SourcingPost
	var sizeDesc, notes sql.NullString
	var plantID sql.NullInt64
	var qty sql.NullInt32
	var neededByDate, expiresAt, closedAt sql.NullTime
	if err := row.Scan(
		&p.ID, &p.PostCode, &p.NurseryID, &p.NurseryName,
		&p.PostedByUserID, &p.PostedByName,
		&p.PostType, &plantID, &p.PlantName, &sizeDesc,
		&qty, &p.Urgency, &neededByDate, &notes,
		&p.RadiusKM, &p.ResponseCount, &p.Status,
		&expiresAt, &closedAt, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return SourcingPost{}, err
	}
	if plantID.Valid {
		p.PlantID = &plantID.Int64
	}
	p.SizeDesc = nullStr(sizeDesc)
	p.Notes = nullStr(notes)
	if qty.Valid {
		v := int(qty.Int32)
		p.Quantity = &v
	}
	if neededByDate.Valid {
		p.NeededByDate = &neededByDate.Time
	}
	if expiresAt.Valid {
		p.ExpiresAt = &expiresAt.Time
	}
	if closedAt.Valid {
		p.ClosedAt = &closedAt.Time
	}
	return p, nil
}

func scanPostRow(row interface{ Scan(...any) error }) (*SourcingPost, error) {
	p, err := scanPost(row)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func scanResponseRow(row interface{ Scan(...any) error }) (*PostResponse, error) {
	var resp PostResponse
	var notes, contactInfo sql.NullString
	var qty sql.NullInt32
	if err := row.Scan(
		&resp.ID, &resp.PostID, &resp.ResponderNurseryID, &resp.ResponderNursery,
		&resp.RespondedByUserID, &resp.RespondedByName,
		&qty, &notes, &contactInfo, &resp.Status,
		&resp.RespondedAt, &resp.CreatedAt,
	); err != nil {
		return nil, err
	}
	if qty.Valid {
		v := int(qty.Int32)
		resp.AvailableQuantity = &v
	}
	resp.Notes = nullStr(notes)
	resp.ContactInfo = nullStr(contactInfo)
	return &resp, nil
}

func nullStr(s sql.NullString) *string {
	if !s.Valid || s.String == "" {
		return nil
	}
	return &s.String
}

func strPtrOrNil(s *string) any {
	if s == nil || *s == "" {
		return nil
	}
	return *s
}

func intPtrOrNil(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func parseDateOrNil(s *string) any {
	if s == nil || *s == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", *s)
	if err != nil {
		return nil
	}
	return t
}

func parseTimeOrNil(s *string) any {
	if s == nil || *s == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, *s); err == nil {
			return t
		}
	}
	return nil
}
