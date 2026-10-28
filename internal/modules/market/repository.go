package market

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/location"
)

var ErrNotFound = apperrs.ErrNotFound

type Repository interface {
	// Nursery helpers
	NurseryIDForUser(ctx context.Context, userID int64) (int64, error)
	NurseryName(ctx context.Context, nurseryID int64) (string, error)
	NurseryOwnerID(ctx context.Context, nurseryID int64) (int64, error)
	IsNurseryMember(ctx context.Context, nurseryID int64, userID int64) (bool, error)
	IsNurseryActive(ctx context.Context, nurseryID int64) (bool, error)

	// Ads
	CreateAd(ctx context.Context, nurseryID, userID int64, req CreateAdRequest) (Ad, error)
	GetAd(ctx context.Context, id int64) (Ad, error)
	ListPublished(ctx context.Context, q AdsQuery) ([]Ad, int, error)
	ListByNursery(ctx context.Context, nurseryID int64, q AdsQuery) ([]Ad, int, error)
	UpdateAd(ctx context.Context, id, userID int64, req UpdateAdRequest) (Ad, error)
	SetAdStatus(ctx context.Context, id int64, status string, ts *time.Time) error
	ArchiveAd(ctx context.Context, id int64, closeReason *string) error
	// IsCatalogSize reports whether name is an active size (Seed, Sapling, ...).
	IsCatalogSize(ctx context.Context, name string) (bool, error)
	// SoftDeleteAd marks the ad DELETED; false when it is no longer deletable
	// (a chat or pending report arrived since it was read).
	SoftDeleteAd(ctx context.Context, id, userID int64) (bool, error)
	SetExpiry(ctx context.Context, id int64, expiresAt time.Time) error
	// ModerateAd moves an ad to SUSPENDED, REMOVED or back to PUBLISHED and
	// records the note the seller sees.
	ModerateAd(ctx context.Context, id int64, status string, note *string) error
	PostingBlockedUntil(ctx context.Context, nurseryID int64) (*time.Time, error)
	SellerProfile(ctx context.Context, nurseryID int64) (SellerProfile, error)

	// Views & saves
	RecordView(ctx context.Context, adID, nurseryID int64) (bool, error)
	ToggleSave(ctx context.Context, adID, nurseryID, userID int64) (bool, error)
	IsSaved(ctx context.Context, adID, nurseryID int64) (bool, error)
	ListSaved(ctx context.Context, nurseryID int64, q AdsQuery) ([]Ad, int, error)
	FlushAdCounters(ctx context.Context, views map[int64]int64, saves map[int64]int64) error

	// Reports
	CreateReport(ctx context.Context, adID, userID, nurseryID int64, reason string, notes *string) error

	// Enquiries
	CreateEnquiry(ctx context.Context, adID, adNurseryID, enquiringNurseryID, userID int64, req CreateEnquiryRequest) (Enquiry, error)
	GetEnquiry(ctx context.Context, id int64) (Enquiry, error)
	ListEnquiries(ctx context.Context, nurseryID int64, q EnquiriesQuery) ([]Enquiry, int, error)
	MarkEnquiryViewed(ctx context.Context, id int64) error
	AddMessage(ctx context.Context, enquiryID, userID, nurseryID int64, body string) (Message, error)
	AddOffer(ctx context.Context, enquiryID, userID, nurseryID int64, req MakeOfferRequest, body string) (Message, error)
	RespondToOffer(ctx context.Context, enquiryID, messageID, userID, nurseryID int64, accept bool, dealBody func(price float64, qty *int) string) error
	SetEnquiryStatus(ctx context.Context, id int64, status string) error
	GetMessages(ctx context.Context, enquiryID int64) ([]Message, error)
	EnquiryIDFor(ctx context.Context, adID, nurseryID int64) (int64, error) // 0 when the nursery has no chat on the ad
	// MarkChatRead records that the viewer's side opened the chat now.
	MarkChatRead(ctx context.Context, enquiryID, viewerNurseryID int64) error
	MarkChatsDelivered(ctx context.Context, viewerNurseryID int64) error
	// ChatSummaries fills LastMessage* and UnreadCount for the viewer.
	ChatSummaries(ctx context.Context, enquiries []Enquiry, viewerNurseryID int64) error
	UnreadTotal(ctx context.Context, viewerNurseryID int64) (int, error)
	// NurseryPickup is the nursery's primary address, the default pickup for its ads.
	NurseryPickup(ctx context.Context, nurseryID int64) (address *string, lat, lon *float64, err error)
	SetAdPickupLocation(ctx context.Context, adID int64, lat, lon float64, source string) error
}

type PostgresRepository struct{ db *sql.DB }

func NewRepository(db *sql.DB) Repository { return &PostgresRepository{db: db} }

// ── Nursery helpers ───────────────────────────────────────────

func (r *PostgresRepository) NurseryIDForUser(ctx context.Context, userID int64) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx,
		`SELECT nursery_id
		 FROM (
			SELECT nursery_id, 1 AS priority
			FROM public.nurseries
			WHERE owner_user_id = $1 AND COALESCE(status::text, '') <> 'DELETED'

			UNION ALL

			SELECT nursery_id, 2 AS priority
			FROM public.nursery_users
			WHERE user_id = $1 AND status = 'ACTIVE'
		 ) scoped
		 ORDER BY priority
		 LIMIT 1`, userID,
	).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return id, nil
}

func (r *PostgresRepository) NurseryName(ctx context.Context, nurseryID int64) (string, error) {
	var name string
	err := r.db.QueryRowContext(ctx,
		`SELECT nursery_name FROM public.nurseries WHERE nursery_id = $1`, nurseryID,
	).Scan(&name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return name, nil
}

func (r *PostgresRepository) IsNurseryMember(ctx context.Context, nurseryID int64, userID int64) (bool, error) {
	var ok bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM public.nursery_users
			WHERE nursery_id = $1 AND user_id = $2 AND status = 'ACTIVE'

			UNION ALL

			SELECT 1 FROM public.nurseries
			WHERE nursery_id = $1 AND owner_user_id = $2 AND COALESCE(status::text, '') <> 'DELETED'
		)`, nurseryID, userID,
	).Scan(&ok)
	return ok, err
}

func (r *PostgresRepository) IsNurseryActive(ctx context.Context, nurseryID int64) (bool, error) {
	var ok bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM public.nurseries WHERE nursery_id = $1 AND status IN ('ACTIVE', 'APPROVED'))`,
		nurseryID,
	).Scan(&ok)
	return ok, err
}

// ── Ads ───────────────────────────────────────────────────────

func (r *PostgresRepository) CreateAd(ctx context.Context, nurseryID, userID int64, req CreateAdRequest) (Ad, error) {
	photos := photosJSON(req.Photos)
	var id int64
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO public.market_ads
		   (nursery_id, created_by_user_id, plant_id, plant_name, category_name,
		    title, description, quantity, size_description, price_per_unit, price_unit, photos,
		    pickup_address, pickup_landmark, pickup_latitude, pickup_longitude, pickup_location,
		    pickup_gps_accuracy_meters, pickup_location_source, pickup_confirmed_by_user_id, pickup_confirmed_at,
		    ad_type, needed_by_date, is_road_accessible, is_truck_accessible, access_instructions)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,
		    CASE WHEN $15::numeric IS NOT NULL AND $16::numeric IS NOT NULL
		      THEN ST_SetSRID(ST_MakePoint($16::double precision, $15::double precision), 4326)::geography
		      ELSE NULL
		    END,
		    $17,$18,$19,$20,$21,$22::date,$23,$24,$25)
		 RETURNING ad_id`,
		nurseryID, userID, req.PlantID, req.PlantName, req.CategoryName,
		req.Title, req.Description, req.Quantity, req.SizeDescription,
		req.PricePerUnit, req.PriceUnit, photos,
		req.PickupAddress, req.PickupLandmark, req.PickupLatitude, req.PickupLongitude,
		req.PickupGPSAccuracyM, req.PickupLocationSource, req.PickupConfirmedBy, req.PickupConfirmedAt,
		req.AdType, req.NeededBy, req.RoadAccessible, req.TruckAccessible, req.AccessInstructions,
	).Scan(&id)
	if err != nil {
		return Ad{}, err
	}
	return r.GetAd(ctx, id)
}

func (r *PostgresRepository) GetAd(ctx context.Context, id int64) (Ad, error) {
	const q = `
		SELECT ma.ad_id, ma.ad_code, ma.ad_type, ma.needed_by_date, ma.nursery_id, n.nursery_name,
		       (n.status IN ('ACTIVE', 'APPROVED')) AS nursery_verified, n.mobile AS nursery_mobile,
		       ma.created_by_user_id, ma.plant_id, ma.plant_name, ma.category_name,
		       ma.title, ma.description, ma.quantity, ma.size_description,
		       ma.price_per_unit, ma.price_unit, ma.photos,
		       ma.pickup_address, ma.pickup_landmark, ma.pickup_latitude, ma.pickup_longitude,
		       ma.pickup_gps_accuracy_meters, ma.pickup_location_source,
		       ma.pickup_confirmed_by_user_id, ma.pickup_confirmed_at,
		       ma.is_road_accessible, ma.is_truck_accessible, ma.access_instructions,
		       ma.status, ma.view_count, ma.save_count, ma.enquiry_count,
		       ma.expires_at, ma.published_at,
		       ma.paused_at, ma.expired_at, ma.archived_at, ma.moderation_note,
		       NULLIF(TRIM(CONCAT_WS(' ', pu.first_name, pu.last_name)), ''), pu.profile_image_url,
		       ma.created_at, ma.updated_at, NULL::float8
		FROM public.market_ads ma
		JOIN public.nurseries n ON n.nursery_id = ma.nursery_id
		LEFT JOIN public.users pu ON pu.user_id = ma.created_by_user_id
		WHERE ma.ad_id = $1 AND ma.status <> 'DELETED'`
	row := r.db.QueryRowContext(ctx, q, id)
	return scanAd(row)
}

func (r *PostgresRepository) ListPublished(ctx context.Context, q AdsQuery) ([]Ad, int, error) {
	args := []any{}
	filter := "ma.status = 'PUBLISHED'"
	if q.Type != "" {
		args = append(args, q.Type)
		filter += fmt.Sprintf(" AND ma.ad_type = $%d", len(args))
	}
	if q.Search != "" {
		args = append(args, "%"+strings.ToLower(q.Search)+"%")
		n := len(args)
		filter += fmt.Sprintf(
			" AND (LOWER(ma.title) LIKE $%[1]d::text"+
				" OR LOWER(ma.plant_name) LIKE $%[1]d::text"+
				" OR LOWER(n.nursery_name) LIKE $%[1]d::text"+
				" OR LOWER(COALESCE(ma.description,'')) LIKE $%[1]d::text"+
				" OR EXISTS (SELECT 1 FROM public.plants p WHERE p.plant_id = ma.plant_id"+
				" AND (LOWER(p.scientific_name) LIKE $%[1]d::text OR LOWER(COALESCE(p.common_name,'')) LIKE $%[1]d::text)))", n)
	}
	if q.Category != "" {
		args = append(args, q.Category)
		filter += fmt.Sprintf(" AND LOWER(COALESCE(ma.category_name,'')) = LOWER($%d)", len(args))
	}
	if q.MinPrice > 0 {
		args = append(args, q.MinPrice)
		filter += fmt.Sprintf(" AND ma.price_per_unit >= $%d", len(args))
	}
	if q.MaxPrice > 0 {
		args = append(args, q.MaxPrice)
		filter += fmt.Sprintf(" AND ma.price_per_unit <= $%d", len(args))
	}
	distance := noDistance
	if q.NearLat != nil && q.NearLon != nil {
		radiusKM := DefaultRadiusKM
		if q.RadiusKM != nil && *q.RadiusKM > 0 {
			radiusKM = *q.RadiusKM
		}
		args = append(args, *q.NearLon, *q.NearLat) // lon first for ST_MakePoint
		lonIdx, latIdx := len(args)-1, len(args)
		filter += " AND ma.pickup_location IS NOT NULL AND " +
			location.DWithin("ma.pickup_location", lonIdx, latIdx, radiusKM*1000)
		distance = location.DistanceKM("ma.pickup_location", lonIdx, latIdx)
	}
	return r.listAds(ctx, filter, q.Sort, q.Page, q.PerPage, args, distance)
}

func (r *PostgresRepository) ListByNursery(ctx context.Context, nurseryID int64, q AdsQuery) ([]Ad, int, error) {
	// "mine" (not the public-browse default) -- a nursery managing its own
	// ads needs to see what it just created/edited first. The default order
	// sorts by published_at NULLS LAST, so a never-published DRAFT always
	// sorts after every ad that has ever been published, regardless of how
	// recently the draft was created; once a nursery accumulates >= one
	// page of ever-published ads, new drafts fall off page 1 permanently
	// (the My Ads screen has no "load more").
	return r.listAds(ctx, "ma.nursery_id = $1 AND ma.status <> 'DELETED'", "mine", q.Page, q.PerPage, []any{nurseryID}, noDistance)
}

// listAds runs count + paginated SELECT. filterArgs are args for the WHERE clause only;
// LIMIT and OFFSET are appended here so parameter indices are always correct.
// noDistance fills the distance column when the list is not around a point.
const noDistance = "NULL::float8"

// listAds runs count + page; distance is the SQL for each ad's distance in km
// from the viewer (noDistance when not browsing nearby).
func (r *PostgresRepository) listAds(ctx context.Context, filter, sort string, page, perPage int, filterArgs []any, distance string) ([]Ad, int, error) {
	// #nosec G201 -- WHERE/ORDER BY fragments are fixed strings with $n placeholders (ORDER BY from an allowlist); values are bound args.
	countQ := fmt.Sprintf(`
		SELECT COUNT(*) FROM public.market_ads ma
		JOIN public.nurseries n ON n.nursery_id = ma.nursery_id
		WHERE %s`, filter)

	var total int
	if err := r.db.QueryRowContext(ctx, countQ, filterArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	orderBy := "ma.published_at DESC NULLS LAST, ma.created_at DESC"
	switch sort {
	case "mine":
		orderBy = "ma.created_at DESC"
	case "oldest":
		orderBy = "ma.published_at ASC NULLS LAST, ma.created_at ASC"
	case "name":
		orderBy = "LOWER(ma.plant_name) ASC, LOWER(ma.title) ASC, ma.created_at DESC"
	// "Price on request" (no price, or 0 from older clients) sorts last both ways.
	case "price_asc":
		orderBy = "NULLIF(ma.price_per_unit, 0) ASC NULLS LAST, ma.created_at DESC"
	case "price_desc":
		orderBy = "NULLIF(ma.price_per_unit, 0) DESC NULLS LAST, ma.created_at DESC"
	case "popular":
		orderBy = "ma.view_count DESC, ma.created_at DESC"
	case "nearest":
		orderBy = "distance_km ASC NULLS LAST, ma.created_at DESC"
	}

	offset := (page - 1) * perPage
	listArgs := append(append([]any{}, filterArgs...), perPage, offset)
	limitIdx := len(listArgs) - 1
	offsetIdx := len(listArgs)

	// #nosec G201 -- WHERE/ORDER BY fragments are fixed strings with $n placeholders (ORDER BY from an allowlist); values are bound args.
	listQ := fmt.Sprintf(`
		SELECT ma.ad_id, ma.ad_code, ma.ad_type, ma.needed_by_date, ma.nursery_id, n.nursery_name,
		       (n.status IN ('ACTIVE', 'APPROVED')) AS nursery_verified, n.mobile AS nursery_mobile,
		       ma.created_by_user_id, ma.plant_id, ma.plant_name, ma.category_name,
		       ma.title, ma.description, ma.quantity, ma.size_description,
		       ma.price_per_unit, ma.price_unit, ma.photos,
		       ma.pickup_address, ma.pickup_landmark, ma.pickup_latitude, ma.pickup_longitude,
		       ma.pickup_gps_accuracy_meters, ma.pickup_location_source,
		       ma.pickup_confirmed_by_user_id, ma.pickup_confirmed_at,
		       ma.is_road_accessible, ma.is_truck_accessible, ma.access_instructions,
		       ma.status, ma.view_count, ma.save_count, ma.enquiry_count,
		       ma.expires_at, ma.published_at,
		       ma.paused_at, ma.expired_at, ma.archived_at, ma.moderation_note,
		       NULLIF(TRIM(CONCAT_WS(' ', pu.first_name, pu.last_name)), ''), pu.profile_image_url,
		       ma.created_at, ma.updated_at, %s AS distance_km
		FROM public.market_ads ma
		JOIN public.nurseries n ON n.nursery_id = ma.nursery_id
		LEFT JOIN public.users pu ON pu.user_id = ma.created_by_user_id
		WHERE %s
		ORDER BY %s
		LIMIT $%d OFFSET $%d`, distance, filter, orderBy, limitIdx, offsetIdx)

	rows, err := r.db.QueryContext(ctx, listQ, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var ads []Ad
	for rows.Next() {
		a, err := scanAdRow(rows)
		if err != nil {
			return nil, 0, err
		}
		ads = append(ads, a)
	}
	if ads == nil {
		ads = []Ad{}
	}
	return ads, total, rows.Err()
}

func (r *PostgresRepository) UpdateAd(ctx context.Context, id, userID int64, req UpdateAdRequest) (Ad, error) {
	setClauses := []string{"updated_by_user_id = $1", "updated_at = NOW()"}
	args := []any{userID}
	add := func(col string, v any) {
		args = append(args, v)
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	addExpr := func(expr string, values ...any) {
		start := len(args) + 1
		args = append(args, values...)
		// One pass: renumbering $1→$2 then $2→$3 in turn would also move the first.
		pairs := make([]string, 0, 2*len(values))
		for i := range values {
			pairs = append(pairs, fmt.Sprintf("$%d", i+1), fmt.Sprintf("$%d", start+i))
		}
		setClauses = append(setClauses, strings.NewReplacer(pairs...).Replace(expr))
	}
	if req.PlantName != nil {
		add("plant_name", *req.PlantName)
	}
	if req.NeededBy != nil {
		addExpr("needed_by_date = NULLIF($1, '')::date", *req.NeededBy)
	}
	if req.PlantID != nil {
		add("plant_id", *req.PlantID)
	}
	if req.CategoryName != nil {
		add("category_name", *req.CategoryName)
	}
	if req.Title != nil {
		add("title", *req.Title)
	}
	if req.Description != nil {
		add("description", *req.Description)
	}
	if req.Quantity != nil {
		add("quantity", *req.Quantity)
	}
	if req.SizeDescription != nil {
		add("size_description", *req.SizeDescription)
	}
	if req.PricePerUnit != nil {
		add("price_per_unit", *req.PricePerUnit)
	}
	if req.PriceUnit != nil {
		add("price_unit", *req.PriceUnit)
	}
	if req.Photos != nil {
		add("photos", photosJSON(req.Photos))
	}
	if req.PickupAddress != nil {
		add("pickup_address", *req.PickupAddress)
	}
	if req.PickupLandmark != nil {
		add("pickup_landmark", *req.PickupLandmark)
	}
	if req.RoadAccessible != nil {
		add("is_road_accessible", *req.RoadAccessible)
	}
	if req.TruckAccessible != nil {
		add("is_truck_accessible", *req.TruckAccessible)
	}
	if req.AccessInstructions != nil {
		add("access_instructions", *req.AccessInstructions)
	}
	if req.PickupLatitude != nil && req.PickupLongitude != nil {
		addExpr("pickup_latitude = $1::double precision, pickup_longitude = $2::double precision, pickup_location = ST_SetSRID(ST_MakePoint($2::double precision, $1::double precision), 4326)::geography", *req.PickupLatitude, *req.PickupLongitude)
	}
	if req.PickupGPSAccuracyM != nil {
		add("pickup_gps_accuracy_meters", *req.PickupGPSAccuracyM)
	}
	if req.PickupLocationSource != nil {
		add("pickup_location_source", *req.PickupLocationSource)
	}
	if req.PickupConfirmedBy != nil {
		add("pickup_confirmed_by_user_id", *req.PickupConfirmedBy)
	}
	if req.PickupConfirmedAt != nil {
		add("pickup_confirmed_at", *req.PickupConfirmedAt)
	}
	args = append(args, id)
	// #nosec G201 -- SET columns are code literals; values are bound $n args.
	q := fmt.Sprintf(`UPDATE public.market_ads SET %s WHERE ad_id = $%d`,
		strings.Join(setClauses, ", "), len(args))
	if _, err := r.db.ExecContext(ctx, q, args...); err != nil {
		return Ad{}, err
	}
	return r.GetAd(ctx, id)
}

func (r *PostgresRepository) SetAdStatus(ctx context.Context, id int64, status string, ts *time.Time) error {
	tsCol := map[string]string{
		StatusPublished: "published_at",
		StatusPaused:    "paused_at",
		StatusExpired:   "expired_at",
		StatusArchived:  "archived_at",
	}
	q := `UPDATE public.market_ads SET status = $1, updated_at = NOW() WHERE ad_id = $2`
	if col, ok := tsCol[status]; ok && ts != nil {
		q = fmt.Sprintf(
			`UPDATE public.market_ads SET status = $1, %s = $2, updated_at = NOW() WHERE ad_id = $3`,
			col,
		)
		_, err := r.db.ExecContext(ctx, q, status, ts, id)
		return err
	}
	_, err := r.db.ExecContext(ctx, q, status, id)
	return err
}

func (r *PostgresRepository) ArchiveAd(ctx context.Context, id int64, closeReason *string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE public.market_ads SET status = 'ARCHIVED', archived_at = NOW(), close_reason = $2, updated_at = NOW()
		 WHERE ad_id = $1`, id, closeReason)
	return err
}

func (r *PostgresRepository) IsCatalogSize(ctx context.Context, name string) (bool, error) {
	var ok bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM public.plant_sizes WHERE is_active AND display_name = $1::text)`, name).Scan(&ok)
	return ok, err
}

func (r *PostgresRepository) SoftDeleteAd(ctx context.Context, id, userID int64) (bool, error) {
	// The deletable check is repeated here so a chat or report that lands
	// between the read and this write still blocks the delete.
	res, err := r.db.ExecContext(ctx, `
		UPDATE public.market_ads ma
		SET status = 'DELETED', deleted_at = NOW(), deleted_by_user_id = $2,
		    updated_by_user_id = $2, updated_at = NOW()
		WHERE ma.ad_id = $1
		  AND (ma.status = 'DRAFT' OR (ma.status IN ('PUBLISHED', 'PAUSED')
		       AND NOT EXISTS (SELECT 1 FROM public.market_enquiries e WHERE e.ad_id = ma.ad_id)
		       AND NOT EXISTS (SELECT 1 FROM public.market_ad_reports rp WHERE rp.ad_id = ma.ad_id AND rp.status = 'PENDING')))`,
		id, userID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (r *PostgresRepository) ModerateAd(ctx context.Context, id int64, status string, note *string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE public.market_ads SET status = $2, moderation_note = $3, moderated_at = NOW(), updated_at = NOW()
		 WHERE ad_id = $1`, id, status, note)
	return err
}

func (r *PostgresRepository) PostingBlockedUntil(ctx context.Context, nurseryID int64) (*time.Time, error) {
	var until sql.NullTime
	err := r.db.QueryRowContext(ctx,
		`SELECT market_blocked_until_at FROM public.nurseries WHERE nursery_id = $1 AND market_blocked_until_at > NOW()`,
		nurseryID).Scan(&until)
	if errors.Is(err, sql.ErrNoRows) || !until.Valid {
		return nil, nil
	}
	return &until.Time, err
}

// SellerProfile is the public market face of a nursery: no strikes, no
// private contact beyond what its ads already show.
func (r *PostgresRepository) SellerProfile(ctx context.Context, nurseryID int64) (SellerProfile, error) {
	var p SellerProfile
	err := r.db.QueryRowContext(ctx, `
		SELECT n.nursery_id, n.nursery_name, (n.status IN ('ACTIVE', 'APPROVED')), n.description, n.logo_url,
		       (SELECT a.city FROM public.nursery_addresses a WHERE a.nursery_id = n.nursery_id ORDER BY a.is_primary DESC, a.nursery_address_id LIMIT 1),
		       NULLIF(TRIM(CONCAT_WS(' ', u.first_name, u.last_name)), ''), u.profile_image_url, n.created_at,
		       (SELECT count(*) FROM public.market_ads x WHERE x.nursery_id = n.nursery_id AND x.published_at IS NOT NULL AND x.status <> 'DELETED'),
		       (SELECT count(*) FROM public.market_ads x WHERE x.nursery_id = n.nursery_id AND x.status = 'PUBLISHED'),
		       (SELECT count(*) FROM public.market_enquiries e WHERE e.ad_nursery_id = n.nursery_id AND e.agreed_at IS NOT NULL)
		FROM public.nurseries n
		LEFT JOIN public.users u ON u.user_id = n.owner_user_id
		WHERE n.nursery_id = $1 AND n.deleted_at IS NULL`, nurseryID).Scan(
		&p.NurseryID, &p.NurseryName, &p.Verified, &p.Description, &p.LogoURL, &p.City,
		&p.OwnerName, &p.OwnerPhoto, &p.MemberSince, &p.AdsPosted, &p.LiveAds, &p.DealsAgreed)
	if errors.Is(err, sql.ErrNoRows) {
		return SellerProfile{}, ErrNotFound
	}
	if err != nil {
		return SellerProfile{}, err
	}
	p.Ads, _, err = r.listAds(ctx, "ma.status = 'PUBLISHED' AND ma.nursery_id = $1", "newest", 1, 50, []any{nurseryID}, noDistance)
	if err != nil {
		return SellerProfile{}, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT t.plant_id, p.scientific_name, p.common_name, s.display_name, t.unit_price, t.quality_notes, t.photos->>0
		FROM public.nursery_top_items t
		JOIN public.plants p      ON p.plant_id = t.plant_id
		JOIN public.plant_sizes s ON s.size_id  = t.size_id
		WHERE t.nursery_id = $1
		ORDER BY t.display_order, t.top_item_id`, nurseryID)
	if err != nil {
		return SellerProfile{}, err
	}
	defer rows.Close()
	p.TopItems = []SellerTopItem{}
	for rows.Next() {
		var t SellerTopItem
		if err := rows.Scan(&t.PlantID, &t.PlantName, &t.CommonName, &t.SizeName, &t.UnitPrice, &t.QualityNotes, &t.Photo); err != nil {
			return SellerProfile{}, err
		}
		p.TopItems = append(p.TopItems, t)
	}
	return p, rows.Err()
}

func (r *PostgresRepository) SetExpiry(ctx context.Context, id int64, expiresAt time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE public.market_ads SET expires_at = $1, updated_at = NOW() WHERE ad_id = $2`,
		expiresAt, id,
	)
	return err
}

// ── Views & saves ─────────────────────────────────────────────

// RecordView reports whether this nursery's first view of the ad was recorded.
func (r *PostgresRepository) RecordView(ctx context.Context, adID, nurseryID int64) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO public.market_ad_views (ad_id, nursery_id)
		 VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		adID, nurseryID,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (r *PostgresRepository) ToggleSave(ctx context.Context, adID, nurseryID, userID int64) (bool, error) {
	already, err := r.IsSaved(ctx, adID, nurseryID)
	if err != nil {
		return false, err
	}
	if already {
		if _, err := r.db.ExecContext(ctx,
			`DELETE FROM public.market_ad_saves WHERE ad_id = $1 AND nursery_id = $2`,
			adID, nurseryID,
		); err != nil {
			return false, err
		}
		return false, nil
	}
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO public.market_ad_saves (ad_id, nursery_id, saved_by_user_id) VALUES ($1,$2,$3)`,
		adID, nurseryID, userID,
	); err != nil {
		return false, err
	}
	return true, nil
}

func (r *PostgresRepository) IsSaved(ctx context.Context, adID, nurseryID int64) (bool, error) {
	var ok bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM public.market_ad_saves WHERE ad_id = $1 AND nursery_id = $2)`,
		adID, nurseryID,
	).Scan(&ok)
	return ok, err
}

func (r *PostgresRepository) ListSaved(ctx context.Context, nurseryID int64, q AdsQuery) ([]Ad, int, error) {
	filter := `ma.status = 'PUBLISHED' AND EXISTS(
		SELECT 1 FROM public.market_ad_saves s
		WHERE s.ad_id = ma.ad_id AND s.nursery_id = $1
	)`
	return r.listAds(ctx, filter, "", q.Page, q.PerPage, []any{nurseryID}, noDistance)
}

func (r *PostgresRepository) FlushAdCounters(ctx context.Context, views map[int64]int64, saves map[int64]int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for adID, delta := range views {
		if delta == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE public.market_ads
			 SET view_count = view_count + $2, updated_at = NOW()
			 WHERE ad_id = $1`,
			adID, delta,
		); err != nil {
			return err
		}
	}

	for adID, delta := range saves {
		if delta == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE public.market_ads
			 SET save_count = GREATEST(0, save_count + $2), updated_at = NOW()
			 WHERE ad_id = $1`,
			adID, delta,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// ── Reports ───────────────────────────────────────────────────

func (r *PostgresRepository) CreateReport(ctx context.Context, adID, userID, nurseryID int64, reason string, notes *string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO public.market_ad_reports
		   (ad_id, reported_by_user_id, reported_by_nursery_id, reason, notes)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (ad_id, reported_by_user_id) DO NOTHING`,
		adID, userID, nurseryID, reason, notes,
	)
	return err
}

// ── Enquiries ─────────────────────────────────────────────────

func (r *PostgresRepository) CreateEnquiry(ctx context.Context, adID, adNurseryID, enquiringNurseryID, userID int64, req CreateEnquiryRequest) (Enquiry, error) {
	var id int64
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO public.market_enquiries
		   (ad_id, ad_nursery_id, enquiring_nursery_id, created_by_user_id, message, quantity_needed)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 RETURNING enquiry_id`,
		adID, adNurseryID, enquiringNurseryID, userID, req.Message, req.QuantityNeeded,
	).Scan(&id)
	if err != nil {
		return Enquiry{}, err
	}
	_, _ = r.db.ExecContext(ctx,
		`UPDATE public.market_ads SET enquiry_count = enquiry_count + 1 WHERE ad_id = $1`,
		adID,
	)
	_, _ = r.db.ExecContext(ctx,
		`INSERT INTO public.market_enquiry_messages (enquiry_id, sent_by_user_id, sent_by_nursery_id, body)
		 VALUES ($1,$2,$3,$4)`,
		id, userID, enquiringNurseryID, req.Message,
	)
	return r.GetEnquiry(ctx, id)
}

func (r *PostgresRepository) GetEnquiry(ctx context.Context, id int64) (Enquiry, error) {
	row := r.db.QueryRowContext(ctx, enquirySelect+` WHERE me.enquiry_id = $1`, id)
	return scanEnquiry(row)
}

func (r *PostgresRepository) ListEnquiries(ctx context.Context, nurseryID int64, q EnquiriesQuery) ([]Enquiry, int, error) {
	// Filter values first ($1..$n), paging last: the count query takes only the filters.
	args := []any{nurseryID}
	cond := "(me.ad_nursery_id = $1 OR me.enquiring_nursery_id = $1)"
	if q.Direction == "received" {
		cond = "me.ad_nursery_id = $1"
	} else if q.Direction == "sent" {
		cond = "me.enquiring_nursery_id = $1"
	}
	if q.Status != "" {
		args = append(args, q.Status)
		cond += fmt.Sprintf(" AND me.status = $%d", len(args))
	}
	if q.AdID > 0 {
		args = append(args, q.AdID)
		cond += fmt.Sprintf(" AND me.ad_id = $%d", len(args))
	}

	var total int
	if err := r.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM public.market_enquiries me WHERE %s`, cond),
		args...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, q.PerPage, (q.Page-1)*q.PerPage)

	rows, err := r.db.QueryContext(ctx, enquirySelect+fmt.Sprintf(`
		WHERE %s
		ORDER BY me.updated_at DESC
		LIMIT $%d OFFSET $%d`, cond, len(args)-1, len(args)),
		args...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var enquiries []Enquiry
	for rows.Next() {
		e, err := scanEnquiryRow(rows)
		if err != nil {
			return nil, 0, err
		}
		enquiries = append(enquiries, e)
	}
	if enquiries == nil {
		enquiries = []Enquiry{}
	}
	return enquiries, total, rows.Err()
}

func (r *PostgresRepository) MarkEnquiryViewed(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE public.market_enquiries SET viewed_at = NOW(), updated_at = NOW()
		 WHERE enquiry_id = $1 AND viewed_at IS NULL`,
		id,
	)
	return err
}

func (r *PostgresRepository) AddMessage(ctx context.Context, enquiryID, userID, nurseryID int64, body string) (Message, error) {
	var msgID int64
	if err := r.db.QueryRowContext(ctx,
		`INSERT INTO public.market_enquiry_messages (enquiry_id, sent_by_user_id, sent_by_nursery_id, body)
		 VALUES ($1,$2,$3,$4) RETURNING message_id`,
		enquiryID, userID, nurseryID, body,
	).Scan(&msgID); err != nil {
		return Message{}, err
	}
	if err := r.touchEnquiry(ctx, r.db, enquiryID, nurseryID); err != nil {
		return Message{}, err
	}
	return r.getMessage(ctx, msgID)
}

// touchEnquiry moves a NEW chat to IN_PROGRESS once the seller first writes,
// and bumps updated_at so the inbox sorts by latest activity.
func (r *PostgresRepository) touchEnquiry(ctx context.Context, db execer, enquiryID, senderNurseryID int64) error {
	_, err := db.ExecContext(ctx,
		`UPDATE public.market_enquiries
		 SET updated_at = NOW(),
		     replied_at = CASE WHEN ad_nursery_id = $2 THEN COALESCE(replied_at, NOW()) ELSE replied_at END,
		     status     = CASE WHEN ad_nursery_id = $2 AND status = 'NEW' THEN 'IN_PROGRESS' ELSE status END
		 WHERE enquiry_id = $1`,
		enquiryID, senderNurseryID,
	)
	return err
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// AddOffer withdraws the chat's pending offer, if any, and posts the new one.
func (r *PostgresRepository) AddOffer(ctx context.Context, enquiryID, userID, nurseryID int64, req MakeOfferRequest, body string) (Message, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Message{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`UPDATE public.market_enquiry_messages SET status = 'WITHDRAWN'
		 WHERE enquiry_id = $1 AND status = 'PENDING'`, enquiryID); err != nil {
		return Message{}, err
	}
	var msgID int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO public.market_enquiry_messages
		   (enquiry_id, sent_by_user_id, sent_by_nursery_id, body, message_type, offer_price_per_unit, offer_quantity, status)
		 VALUES ($1,$2,$3,$4,'OFFER',$5,$6,'PENDING') RETURNING message_id`,
		enquiryID, userID, nurseryID, body, req.PricePerUnit, req.Quantity,
	).Scan(&msgID); err != nil {
		return Message{}, err
	}
	if err := r.touchEnquiry(ctx, tx, enquiryID, nurseryID); err != nil {
		return Message{}, err
	}
	if err := tx.Commit(); err != nil {
		return Message{}, err
	}
	return r.getMessage(ctx, msgID)
}

// RespondToOffer accepts or declines a pending offer the other nursery made.
// Accepting records the deal on the chat. The PENDING guard makes a double tap
// or a race with a newer offer fail with ErrConflict instead of two deals.
func (r *PostgresRepository) RespondToOffer(ctx context.Context, enquiryID, messageID, userID, nurseryID int64, accept bool, dealBody func(price float64, qty *int) string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	next := OfferDeclined
	if accept {
		next = OfferAccepted
	}
	var price float64
	var qty sql.NullInt64
	err = tx.QueryRowContext(ctx,
		`UPDATE public.market_enquiry_messages SET status = $4
		 WHERE message_id = $1 AND enquiry_id = $2 AND status = 'PENDING' AND sent_by_nursery_id <> $3
		 RETURNING offer_price_per_unit, offer_quantity`,
		messageID, enquiryID, nurseryID, next,
	).Scan(&price, &qty)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: this offer is no longer open", ErrConflict)
	}
	if err != nil {
		return err
	}
	if accept {
		var q *int
		if qty.Valid {
			v := int(qty.Int64)
			q = &v
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE public.market_enquiries
			 SET status = 'DEAL_AGREED', agreed_price_per_unit = $2, agreed_quantity = $3, agreed_at = NOW(), updated_at = NOW()
			 WHERE enquiry_id = $1`, enquiryID, price, q); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO public.market_enquiry_messages (enquiry_id, sent_by_user_id, sent_by_nursery_id, body, message_type)
			 VALUES ($1,$2,$3,$4,'SYSTEM')`, enquiryID, userID, nurseryID, dealBody(price, q)); err != nil {
			return err
		}
	} else if err := r.touchEnquiry(ctx, tx, enquiryID, nurseryID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *PostgresRepository) SetEnquiryStatus(ctx context.Context, id int64, status string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE public.market_enquiries
		 SET status = $1::text, updated_at = NOW(),
		     closed_at    = CASE WHEN $1::text = 'CLOSED'    THEN NOW() ELSE closed_at END,
		     cancelled_at = CASE WHEN $1::text = 'CANCELLED' THEN NOW() ELSE cancelled_at END
		 WHERE enquiry_id = $2`,
		status, id,
	)
	if err != nil {
		return err
	}
	// Nobody can answer an offer in a finished chat.
	_, err = r.db.ExecContext(ctx,
		`UPDATE public.market_enquiry_messages SET status = 'WITHDRAWN'
		 WHERE enquiry_id = $1 AND status = 'PENDING'`, id)
	return err
}

func (r *PostgresRepository) GetMessages(ctx context.Context, enquiryID int64) ([]Message, error) {
	rows, err := r.db.QueryContext(ctx, messageSelect+` WHERE m.enquiry_id = $1 ORDER BY m.created_at ASC, m.message_id ASC`, enquiryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var msgs []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	if msgs == nil {
		msgs = []Message{}
	}
	return msgs, rows.Err()
}

func (r *PostgresRepository) getMessage(ctx context.Context, id int64) (Message, error) {
	m, err := scanMessage(r.db.QueryRowContext(ctx, messageSelect+` WHERE m.message_id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Message{}, ErrNotFound
	}
	return m, err
}

// unreadForViewer counts the other side's messages after the viewer's last
// visit; $1 is the viewer's nursery, e the enquiry row.
const unreadForViewer = `(SELECT count(*) FROM public.market_enquiry_messages m
	 WHERE m.enquiry_id = e.enquiry_id AND m.sent_by_nursery_id <> $1
	   AND m.created_at > COALESCE(CASE WHEN e.ad_nursery_id = $1 THEN e.ad_nursery_read_at ELSE e.enquirer_read_at END, '-infinity'))`

func (r *PostgresRepository) MarkChatRead(ctx context.Context, enquiryID, viewerNurseryID int64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE public.market_enquiries
		SET ad_nursery_read_at = CASE WHEN ad_nursery_id = $2 THEN NOW() ELSE ad_nursery_read_at END,
		    enquirer_read_at   = CASE WHEN enquiring_nursery_id = $2 THEN NOW() ELSE enquirer_read_at END
		WHERE enquiry_id = $1`, enquiryID, viewerNurseryID)
	return err
}

// MarkChatsDelivered records that the viewer's app has fetched its open chats:
// the other side's messages up to now show as delivered (two grey ticks).
func (r *PostgresRepository) MarkChatsDelivered(ctx context.Context, viewerNurseryID int64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE public.market_enquiries
		SET ad_nursery_delivered_at = CASE WHEN ad_nursery_id = $1 THEN NOW() ELSE ad_nursery_delivered_at END,
		    enquirer_delivered_at   = CASE WHEN enquiring_nursery_id = $1 THEN NOW() ELSE enquirer_delivered_at END
		WHERE (ad_nursery_id = $1 OR enquiring_nursery_id = $1)
		  AND status IN ('NEW', 'IN_PROGRESS', 'DEAL_AGREED')`, viewerNurseryID)
	return err
}

func (r *PostgresRepository) ChatSummaries(ctx context.Context, enquiries []Enquiry, viewerNurseryID int64) error {
	if len(enquiries) == 0 {
		return nil
	}
	ids := make([]int64, len(enquiries))
	index := make(map[int64]int, len(enquiries))
	for i, e := range enquiries {
		ids[i] = e.ID
		index[e.ID] = i
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT e.enquiry_id, lm.body, lm.message_type, lm.created_at, `+unreadForViewer+`
		FROM public.market_enquiries e
		LEFT JOIN LATERAL (
			SELECT body, message_type, created_at FROM public.market_enquiry_messages
			WHERE enquiry_id = e.enquiry_id ORDER BY created_at DESC, message_id DESC LIMIT 1
		) lm ON true
		WHERE e.enquiry_id = ANY($2)`, viewerNurseryID, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var body, typ sql.NullString
		var at sql.NullTime
		var unread int
		if err := rows.Scan(&id, &body, &typ, &at, &unread); err != nil {
			return err
		}
		e := &enquiries[index[id]]
		if body.Valid {
			e.LastMessage, e.LastMessageType, e.LastMessageAt = &body.String, &typ.String, &at.Time
		}
		e.UnreadCount = unread
	}
	return rows.Err()
}

func (r *PostgresRepository) UnreadTotal(ctx context.Context, viewerNurseryID int64) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(`+unreadForViewer+`), 0)
		FROM public.market_enquiries e
		WHERE (e.ad_nursery_id = $1 OR e.enquiring_nursery_id = $1)
		  AND e.status IN ('NEW', 'IN_PROGRESS', 'DEAL_AGREED')`, viewerNurseryID).Scan(&n)
	return n, err
}

func (r *PostgresRepository) NurseryPickup(ctx context.Context, nurseryID int64) (*string, *float64, *float64, error) {
	var addr *string
	var lat, lon *float64
	err := r.db.QueryRowContext(ctx, `
		SELECT NULLIF(CONCAT_WS(', ', NULLIF(address_line1, ''), NULLIF(address_line2, ''), NULLIF(city, '')), ''),
		       latitude, longitude
		FROM public.nursery_addresses WHERE nursery_id = $1
		ORDER BY is_primary DESC, nursery_address_id LIMIT 1`, nurseryID).Scan(&addr, &lat, &lon)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil, nil
	}
	return addr, lat, lon, err
}

// SetAdPickupLocation fills an ad's pickup coordinates (and its geography).
func (r *PostgresRepository) SetAdPickupLocation(ctx context.Context, adID int64, lat, lon float64, source string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE public.market_ads
		SET pickup_latitude = $2::double precision, pickup_longitude = $3::double precision, pickup_location_source = $4,
		    pickup_location = ST_SetSRID(ST_MakePoint($3::double precision, $2::double precision), 4326)::geography,
		    updated_at = CURRENT_TIMESTAMP
		WHERE ad_id = $1`, adID, lat, lon, source)
	return err
}

func (r *PostgresRepository) NurseryOwnerID(ctx context.Context, nurseryID int64) (int64, error) {
	var id sql.NullInt64
	err := r.db.QueryRowContext(ctx, `SELECT owner_user_id FROM public.nurseries WHERE nursery_id = $1`, nurseryID).Scan(&id)
	return id.Int64, err
}

func (r *PostgresRepository) EnquiryIDFor(ctx context.Context, adID, nurseryID int64) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx,
		`SELECT enquiry_id FROM public.market_enquiries WHERE ad_id = $1 AND enquiring_nursery_id = $2`,
		adID, nurseryID,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// ── Shared SELECT lists ──────────────────────────────────────────────

const messageSelect = `
	SELECT m.message_id, m.enquiry_id, m.sent_by_user_id, m.sent_by_nursery_id,
	       n.nursery_name, TRIM(COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '')),
	       m.message_type, m.body, m.offer_price_per_unit, m.offer_quantity, m.status, m.created_at,
	       u.profile_image_url,
	       -- The receiving side's read/delivered times decide the ticks.
	       CASE WHEN m.created_at <= CASE WHEN m.sent_by_nursery_id = e.ad_nursery_id THEN e.enquirer_read_at ELSE e.ad_nursery_read_at END THEN 'READ'
	            WHEN m.created_at <= CASE WHEN m.sent_by_nursery_id = e.ad_nursery_id THEN e.enquirer_delivered_at ELSE e.ad_nursery_delivered_at END THEN 'DELIVERED'
	            ELSE 'SENT' END
	FROM public.market_enquiry_messages m
	JOIN public.market_enquiries e ON e.enquiry_id = m.enquiry_id
	JOIN public.nurseries n ON n.nursery_id = m.sent_by_nursery_id
	JOIN public.users u     ON u.user_id    = m.sent_by_user_id`

const enquirySelect = `
	SELECT me.enquiry_id, me.enquiry_code,
	       me.ad_id, ma.title, ma.ad_type, ma.status, ma.close_reason, ma.price_per_unit, ma.price_unit, ma.quantity, ma.photos->>0,
	       me.ad_nursery_id, ln.nursery_name,
	       me.enquiring_nursery_id, en.nursery_name,
	       me.created_by_user_id, me.message, me.quantity_needed,
	       me.status, me.agreed_price_per_unit, me.agreed_quantity, me.agreed_at,
	       me.viewed_at, me.replied_at,
	       me.created_at, me.updated_at
	FROM public.market_enquiries me
	JOIN public.market_ads ma  ON ma.ad_id     = me.ad_id
	JOIN public.nurseries ln   ON ln.nursery_id = me.ad_nursery_id
	JOIN public.nurseries en   ON en.nursery_id = me.enquiring_nursery_id`
