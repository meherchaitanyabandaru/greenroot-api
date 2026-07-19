package market

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/location"
	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
)

var ErrNotFound     = apperrs.ErrNotFound

type Repository interface {
	// Nursery helpers
	NurseryIDForUser(ctx context.Context, userID int64) (int64, error)
	NurseryName(ctx context.Context, nurseryID int64) (string, error)
	IsNurseryMember(ctx context.Context, nurseryID int64, userID int64) (bool, error)
	IsNurseryActive(ctx context.Context, nurseryID int64) (bool, error)

	// Ads
	CreateAd(ctx context.Context, nurseryID, userID int64, req CreateAdRequest) (Ad, error)
	GetAd(ctx context.Context, id int64) (Ad, error)
	ListPublished(ctx context.Context, q AdsQuery) ([]Ad, int, error)
	ListByNursery(ctx context.Context, nurseryID int64, q AdsQuery) ([]Ad, int, error)
	UpdateAd(ctx context.Context, id, userID int64, req UpdateAdRequest) (Ad, error)
	SetAdStatus(ctx context.Context, id int64, status string, ts *time.Time) error
	SetExpiry(ctx context.Context, id int64, expiresAt time.Time) error

	// Views & saves
	RecordView(ctx context.Context, adID, nurseryID int64) error
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
	SetEnquiryStatus(ctx context.Context, id int64, status string) error
	LinkQuotation(ctx context.Context, enquiryID, quotationID int64) error
	GetMessages(ctx context.Context, enquiryID int64) ([]Message, error)
	HasEnquiry(ctx context.Context, adID, nurseryID int64) (bool, error)
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
		    pickup_gps_accuracy_meters, pickup_location_source, pickup_confirmed_by, pickup_confirmed_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,
		    CASE WHEN $15::numeric IS NOT NULL AND $16::numeric IS NOT NULL
		      THEN ST_SetSRID(ST_MakePoint($16::double precision, $15::double precision), 4326)::geography
		      ELSE NULL
		    END,
		    $17,$18,$19,$20)
		 RETURNING ad_id`,
		nurseryID, userID, req.PlantID, req.PlantName, req.CategoryName,
		req.Title, req.Description, req.Quantity, req.SizeDescription,
		req.PricePerUnit, req.PriceUnit, photos,
		req.PickupAddress, req.PickupLandmark, req.PickupLatitude, req.PickupLongitude,
		req.PickupGPSAccuracyM, req.PickupLocationSource, req.PickupConfirmedBy, req.PickupConfirmedAt,
	).Scan(&id)
	if err != nil {
		return Ad{}, err
	}
	return r.GetAd(ctx, id)
}

func (r *PostgresRepository) GetAd(ctx context.Context, id int64) (Ad, error) {
	const q = `
		SELECT ma.ad_id, ma.ad_code, ma.nursery_id, n.nursery_name,
		       (n.status IN ('ACTIVE', 'APPROVED')) AS nursery_verified, n.mobile AS nursery_mobile,
		       ma.created_by_user_id, ma.plant_id, ma.plant_name, ma.category_name,
		       ma.title, ma.description, ma.quantity, ma.size_description,
		       ma.price_per_unit, ma.price_unit, ma.photos,
		       ma.pickup_address, ma.pickup_landmark, ma.pickup_latitude, ma.pickup_longitude,
		       ma.pickup_gps_accuracy_meters, ma.pickup_location_source,
		       ma.pickup_confirmed_by, ma.pickup_confirmed_at,
		       ma.status, ma.view_count, ma.save_count, ma.enquiry_count,
		       ma.expires_at, ma.published_at,
		       ma.paused_at, ma.expired_at, ma.archived_at,
		       ma.created_at, ma.updated_at
		FROM public.market_ads ma
		JOIN public.nurseries n ON n.nursery_id = ma.nursery_id
		WHERE ma.ad_id = $1`
	row := r.db.QueryRowContext(ctx, q, id)
	return scanAd(row)
}

func (r *PostgresRepository) ListPublished(ctx context.Context, q AdsQuery) ([]Ad, int, error) {
	args := []any{}
	filter := "ma.status = 'PUBLISHED'"
	if q.Search != "" {
		args = append(args, "%"+strings.ToLower(q.Search)+"%")
		n := len(args)
		filter += fmt.Sprintf(
			" AND (LOWER(ma.title) LIKE $%[1]d::text"+
				" OR LOWER(ma.plant_name) LIKE $%[1]d::text"+
				" OR LOWER(n.nursery_name) LIKE $%[1]d::text"+
				" OR LOWER(COALESCE(ma.description,'')) LIKE $%[1]d::text)", n)
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
	if q.NearLat != nil && q.NearLon != nil {
		radiusKM := 50.0
		if q.RadiusKM != nil && *q.RadiusKM > 0 {
			radiusKM = *q.RadiusKM
		}
		args = append(args, *q.NearLon, *q.NearLat) // lon first for ST_MakePoint
		lonIdx, latIdx := len(args)-1, len(args)
		filter += " AND ma.pickup_location IS NOT NULL AND " +
			location.DWithin("ma.pickup_location", lonIdx, latIdx, radiusKM*1000)
	}
	return r.listAds(ctx, filter, q.Sort, q.Page, q.PerPage, args)
}

func (r *PostgresRepository) ListByNursery(ctx context.Context, nurseryID int64, q AdsQuery) ([]Ad, int, error) {
	return r.listAds(ctx, "ma.nursery_id = $1", "", q.Page, q.PerPage, []any{nurseryID})
}

// listAds runs count + paginated SELECT. filterArgs are args for the WHERE clause only;
// LIMIT and OFFSET are appended here so parameter indices are always correct.
func (r *PostgresRepository) listAds(ctx context.Context, filter, sort string, page, perPage int, filterArgs []any) ([]Ad, int, error) {
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
	case "oldest":
		orderBy = "ma.created_at ASC"
	case "price_asc":
		orderBy = "ma.price_per_unit ASC NULLS LAST, ma.created_at DESC"
	case "price_desc":
		orderBy = "ma.price_per_unit DESC NULLS FIRST, ma.created_at DESC"
	case "popular":
		orderBy = "ma.view_count DESC, ma.created_at DESC"
	case "nearest":
		// Distance ordering is handled by the nearby filter appended to the filter
		// clause; here we order by pickup_location distance when a reference point
		// is embedded in the filter args. Fall back to newest if no location given.
		orderBy = "ma.published_at DESC NULLS LAST, ma.created_at DESC"
	}

	offset := (page - 1) * perPage
	listArgs := append(append([]any{}, filterArgs...), perPage, offset)
	limitIdx := len(listArgs) - 1
	offsetIdx := len(listArgs)

	listQ := fmt.Sprintf(`
		SELECT ma.ad_id, ma.ad_code, ma.nursery_id, n.nursery_name,
		       (n.status IN ('ACTIVE', 'APPROVED')) AS nursery_verified, n.mobile AS nursery_mobile,
		       ma.created_by_user_id, ma.plant_id, ma.plant_name, ma.category_name,
		       ma.title, ma.description, ma.quantity, ma.size_description,
		       ma.price_per_unit, ma.price_unit, ma.photos,
		       ma.pickup_address, ma.pickup_landmark, ma.pickup_latitude, ma.pickup_longitude,
		       ma.pickup_gps_accuracy_meters, ma.pickup_location_source,
		       ma.pickup_confirmed_by, ma.pickup_confirmed_at,
		       ma.status, ma.view_count, ma.save_count, ma.enquiry_count,
		       ma.expires_at, ma.published_at,
		       ma.paused_at, ma.expired_at, ma.archived_at,
		       ma.created_at, ma.updated_at
		FROM public.market_ads ma
		JOIN public.nurseries n ON n.nursery_id = ma.nursery_id
		WHERE %s
		ORDER BY %s
		LIMIT $%d OFFSET $%d`, filter, orderBy, limitIdx, offsetIdx)

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
		for _, v := range values {
			args = append(args, v)
		}
		for i := range values {
			expr = strings.ReplaceAll(expr, fmt.Sprintf("$%d", i+1), fmt.Sprintf("$%d", start+i))
		}
		setClauses = append(setClauses, expr)
	}
	if req.PlantName != nil {
		add("plant_name", *req.PlantName)
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
	if req.PickupLatitude != nil && req.PickupLongitude != nil {
		addExpr("pickup_latitude = $1, pickup_longitude = $2, pickup_location = ST_SetSRID(ST_MakePoint($2::double precision, $1::double precision), 4326)::geography", *req.PickupLatitude, *req.PickupLongitude)
	}
	if req.PickupGPSAccuracyM != nil {
		add("pickup_gps_accuracy_meters", *req.PickupGPSAccuracyM)
	}
	if req.PickupLocationSource != nil {
		add("pickup_location_source", *req.PickupLocationSource)
	}
	if req.PickupConfirmedBy != nil {
		add("pickup_confirmed_by", *req.PickupConfirmedBy)
	}
	if req.PickupConfirmedAt != nil {
		add("pickup_confirmed_at", *req.PickupConfirmedAt)
	}
	args = append(args, id)
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

func (r *PostgresRepository) SetExpiry(ctx context.Context, id int64, expiresAt time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE public.market_ads SET expires_at = $1, updated_at = NOW() WHERE ad_id = $2`,
		expiresAt, id,
	)
	return err
}

// ── Views & saves ─────────────────────────────────────────────

func (r *PostgresRepository) RecordView(ctx context.Context, adID, nurseryID int64) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO public.market_ad_views (ad_id, nursery_id)
		 VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		adID, nurseryID,
	)
	return err
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
	return r.listAds(ctx, filter, "", q.Page, q.PerPage, []any{nurseryID})
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
	const q = `
		SELECT me.enquiry_id, me.enquiry_code,
		       me.ad_id, ma.title,
		       me.ad_nursery_id, ln.nursery_name,
		       me.enquiring_nursery_id, en.nursery_name,
		       me.created_by_user_id, me.message, me.quantity_needed,
		       me.status, me.quotation_id,
		       me.viewed_at, me.replied_at,
		       me.created_at, me.updated_at
		FROM public.market_enquiries me
		JOIN public.market_ads ma  ON ma.ad_id     = me.ad_id
		JOIN public.nurseries ln   ON ln.nursery_id = me.ad_nursery_id
		JOIN public.nurseries en   ON en.nursery_id = me.enquiring_nursery_id
		WHERE me.enquiry_id = $1`
	row := r.db.QueryRowContext(ctx, q, id)
	return scanEnquiry(row)
}

func (r *PostgresRepository) ListEnquiries(ctx context.Context, nurseryID int64, q EnquiriesQuery) ([]Enquiry, int, error) {
	offset := (q.Page - 1) * q.PerPage
	args := []any{nurseryID, q.PerPage, offset}
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

	var total int
	if err := r.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM public.market_enquiries me WHERE %s`, cond),
		args[:len(args)-2]...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT me.enquiry_id, me.enquiry_code,
		       me.ad_id, ma.title,
		       me.ad_nursery_id, ln.nursery_name,
		       me.enquiring_nursery_id, en.nursery_name,
		       me.created_by_user_id, me.message, me.quantity_needed,
		       me.status, me.quotation_id,
		       me.viewed_at, me.replied_at,
		       me.created_at, me.updated_at
		FROM public.market_enquiries me
		JOIN public.market_ads ma  ON ma.ad_id     = me.ad_id
		JOIN public.nurseries ln   ON ln.nursery_id = me.ad_nursery_id
		JOIN public.nurseries en   ON en.nursery_id = me.enquiring_nursery_id
		WHERE %s
		ORDER BY me.created_at DESC
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
	var createdAt time.Time
	if err := r.db.QueryRowContext(ctx,
		`INSERT INTO public.market_enquiry_messages (enquiry_id, sent_by_user_id, sent_by_nursery_id, body)
		 VALUES ($1,$2,$3,$4) RETURNING message_id, created_at`,
		enquiryID, userID, nurseryID, body,
	).Scan(&msgID, &createdAt); err != nil {
		return Message{}, err
	}
	_, _ = r.db.ExecContext(ctx,
		`UPDATE public.market_enquiries SET replied_at = NOW(), status = 'IN_PROGRESS', updated_at = NOW()
		 WHERE enquiry_id = $1 AND replied_at IS NULL`,
		enquiryID,
	)
	nurseryName, _ := r.NurseryName(ctx, nurseryID)
	return Message{
		ID:              msgID,
		EnquiryID:       enquiryID,
		SentByUserID:    userID,
		SentByNurseryID: nurseryID,
		NurseryName:     nurseryName,
		Body:            body,
		CreatedAt:       createdAt,
	}, nil
}

func (r *PostgresRepository) SetEnquiryStatus(ctx context.Context, id int64, status string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE public.market_enquiries SET status = $1, updated_at = NOW() WHERE enquiry_id = $2`,
		status, id,
	)
	return err
}

func (r *PostgresRepository) LinkQuotation(ctx context.Context, enquiryID, quotationID int64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE public.market_enquiries SET quotation_id = $1, status = 'QUOTATION_CREATED', updated_at = NOW()
		 WHERE enquiry_id = $2`,
		quotationID, enquiryID,
	)
	return err
}

func (r *PostgresRepository) GetMessages(ctx context.Context, enquiryID int64) ([]Message, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT m.message_id, m.enquiry_id, m.sent_by_user_id, m.sent_by_nursery_id,
		        n.nursery_name, m.body, m.created_at
		 FROM public.market_enquiry_messages m
		 JOIN public.nurseries n ON n.nursery_id = m.sent_by_nursery_id
		 WHERE m.enquiry_id = $1
		 ORDER BY m.created_at ASC`,
		enquiryID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.EnquiryID, &m.SentByUserID, &m.SentByNurseryID, &m.NurseryName, &m.Body, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	if msgs == nil {
		msgs = []Message{}
	}
	return msgs, rows.Err()
}

func (r *PostgresRepository) HasEnquiry(ctx context.Context, adID, nurseryID int64) (bool, error) {
	var ok bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM public.market_enquiries WHERE ad_id = $1 AND enquiring_nursery_id = $2)`,
		adID, nurseryID,
	).Scan(&ok)
	return ok, err
}

// ── Scan helpers ──────────────────────────────────────────────

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAd(row rowScanner) (Ad, error) {
	return scanAdRow(row)
}

func scanAdRow(row rowScanner) (Ad, error) {
	var a Ad
	var photosRaw []byte
	var plantID sql.NullInt64
	var nurseryMobile, categoryName, description, sizeDesc, priceUnit sql.NullString
	var pickupAddress, pickupLandmark, pickupSource sql.NullString
	var pricePerUnit, pickupLat, pickupLon, pickupAccuracy sql.NullFloat64
	var quantity sql.NullInt64
	var pickupConfirmedBy sql.NullInt64
	var pickupConfirmedAt, expiresAt, publishedAt, pausedAt, expiredAt, archivedAt sql.NullTime

	if err := row.Scan(
		&a.ID, &a.Code, &a.NurseryID, &a.NurseryName, &a.NurseryVerified, &nurseryMobile,
		&a.CreatedByUserID, &plantID, &a.PlantName, &categoryName,
		&a.Title, &description, &quantity, &sizeDesc,
		&pricePerUnit, &priceUnit, &photosRaw,
		&pickupAddress, &pickupLandmark, &pickupLat, &pickupLon,
		&pickupAccuracy, &pickupSource, &pickupConfirmedBy, &pickupConfirmedAt,
		&a.Status, &a.ViewCount, &a.SaveCount, &a.EnquiryCount,
		&expiresAt, &publishedAt, &pausedAt, &expiredAt, &archivedAt,
		&a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Ad{}, ErrNotFound
		}
		return Ad{}, err
	}
	if nurseryMobile.Valid {
		a.NurseryMobile = &nurseryMobile.String
	}
	if plantID.Valid {
		a.PlantID = &plantID.Int64
	}
	if categoryName.Valid {
		a.CategoryName = &categoryName.String
	}
	if description.Valid {
		a.Description = &description.String
	}
	if quantity.Valid {
		v := int(quantity.Int64)
		a.Quantity = &v
	}
	if sizeDesc.Valid {
		a.SizeDescription = &sizeDesc.String
	}
	if pricePerUnit.Valid {
		a.PricePerUnit = &pricePerUnit.Float64
	}
	if priceUnit.Valid {
		a.PriceUnit = &priceUnit.String
	}
	if pickupAddress.Valid {
		a.PickupAddress = &pickupAddress.String
	}
	if pickupLandmark.Valid {
		a.PickupLandmark = &pickupLandmark.String
	}
	if pickupLat.Valid {
		a.PickupLatitude = &pickupLat.Float64
	}
	if pickupLon.Valid {
		a.PickupLongitude = &pickupLon.Float64
	}
	if pickupAccuracy.Valid {
		a.PickupGPSAccuracyM = &pickupAccuracy.Float64
	}
	if pickupSource.Valid {
		a.PickupLocationSource = &pickupSource.String
	}
	if pickupConfirmedBy.Valid {
		a.PickupConfirmedBy = &pickupConfirmedBy.Int64
	}
	if pickupConfirmedAt.Valid {
		a.PickupConfirmedAt = &pickupConfirmedAt.Time
	}
	if expiresAt.Valid {
		a.ExpiresAt = &expiresAt.Time
	}
	if publishedAt.Valid {
		a.PublishedAt = &publishedAt.Time
	}
	if pausedAt.Valid {
		a.PausedAt = &pausedAt.Time
	}
	if expiredAt.Valid {
		a.ExpiredAt = &expiredAt.Time
	}
	if archivedAt.Valid {
		a.ArchivedAt = &archivedAt.Time
	}
	if err := json.Unmarshal(photosRaw, &a.Photos); err != nil {
		a.Photos = []string{}
	}
	if a.Photos == nil {
		a.Photos = []string{}
	}
	return a, nil
}

func scanEnquiry(row rowScanner) (Enquiry, error) {
	return scanEnquiryRow(row)
}

func scanEnquiryRow(row rowScanner) (Enquiry, error) {
	var e Enquiry
	var quantityNeeded sql.NullInt64
	var quotationID sql.NullInt64
	var viewedAt, repliedAt sql.NullTime

	if err := row.Scan(
		&e.ID, &e.Code,
		&e.AdID, &e.AdTitle,
		&e.AdNurseryID, &e.AdNurseryName,
		&e.EnquiringNurseryID, &e.EnquiryNurseryName,
		&e.CreatedByUserID, &e.Message, &quantityNeeded,
		&e.Status, &quotationID,
		&viewedAt, &repliedAt,
		&e.CreatedAt, &e.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Enquiry{}, ErrNotFound
		}
		return Enquiry{}, err
	}
	if quantityNeeded.Valid {
		v := int(quantityNeeded.Int64)
		e.QuantityNeeded = &v
	}
	if quotationID.Valid {
		e.QuotationID = &quotationID.Int64
	}
	if viewedAt.Valid {
		e.ViewedAt = &viewedAt.Time
	}
	if repliedAt.Valid {
		e.RepliedAt = &repliedAt.Time
	}
	e.Messages = []Message{}
	return e, nil
}

func photosJSON(photos []string) []byte {
	if photos == nil {
		photos = []string{}
	}
	b, _ := json.Marshal(photos)
	return b
}
