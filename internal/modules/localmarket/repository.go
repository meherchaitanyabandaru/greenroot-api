package localmarket

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrNotFound = errors.New("not found")

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
	IncrementViewCount(ctx context.Context, adID int64) error
	ToggleSave(ctx context.Context, adID, nurseryID, userID int64) (bool, error)
	IsSaved(ctx context.Context, adID, nurseryID int64) (bool, error)
	ListSaved(ctx context.Context, nurseryID int64, q AdsQuery) ([]Ad, int, error)

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
		`SELECT nursery_id FROM public.nursery_users
		 WHERE user_id = $1 AND status = 'ACTIVE' LIMIT 1`, userID,
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
		)`, nurseryID, userID,
	).Scan(&ok)
	return ok, err
}

func (r *PostgresRepository) IsNurseryActive(ctx context.Context, nurseryID int64) (bool, error) {
	var ok bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM public.nurseries WHERE nursery_id = $1 AND status = 'ACTIVE')`,
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
		    title, description, quantity, size_description, price_per_unit, price_unit, photos)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		 RETURNING ad_id`,
		nurseryID, userID, req.PlantID, req.PlantName, req.CategoryName,
		req.Title, req.Description, req.Quantity, req.SizeDescription,
		req.PricePerUnit, req.PriceUnit, photos,
	).Scan(&id)
	if err != nil {
		return Ad{}, err
	}
	return r.GetAd(ctx, id)
}

func (r *PostgresRepository) GetAd(ctx context.Context, id int64) (Ad, error) {
	const q = `
		SELECT ma.ad_id, ma.ad_code, ma.nursery_id, n.nursery_name,
		       (n.status = 'ACTIVE') AS nursery_verified, n.mobile AS nursery_mobile,
		       ma.created_by_user_id, ma.plant_id, ma.plant_name, ma.category_name,
		       ma.title, ma.description, ma.quantity, ma.size_description,
		       ma.price_per_unit, ma.price_unit, ma.photos,
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
	filterArgs := []any{}
	filter := "ma.status = 'PUBLISHED'"
	if q.PlantName != "" {
		filterArgs = append(filterArgs, "%"+strings.ToLower(q.PlantName)+"%")
		filter += fmt.Sprintf(" AND LOWER(ma.plant_name) LIKE $%d::text", len(filterArgs))
	}
	return r.listAds(ctx, filter, q.Page, q.PerPage, filterArgs)
}

func (r *PostgresRepository) ListByNursery(ctx context.Context, nurseryID int64, q AdsQuery) ([]Ad, int, error) {
	return r.listAds(ctx, "ma.nursery_id = $1", q.Page, q.PerPage, []any{nurseryID})
}

// listAds runs count + paginated SELECT. filterArgs are args for the WHERE clause only;
// LIMIT and OFFSET are appended here so parameter indices are always correct.
func (r *PostgresRepository) listAds(ctx context.Context, filter string, page, perPage int, filterArgs []any) ([]Ad, int, error) {
	countQ := fmt.Sprintf(`
		SELECT COUNT(*) FROM public.market_ads ma
		JOIN public.nurseries n ON n.nursery_id = ma.nursery_id
		WHERE %s`, filter)

	var total int
	if err := r.db.QueryRowContext(ctx, countQ, filterArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * perPage
	listArgs := append(append([]any{}, filterArgs...), perPage, offset)
	limitIdx := len(listArgs) - 1
	offsetIdx := len(listArgs)

	listQ := fmt.Sprintf(`
		SELECT ma.ad_id, ma.ad_code, ma.nursery_id, n.nursery_name,
		       (n.status = 'ACTIVE') AS nursery_verified, n.mobile AS nursery_mobile,
		       ma.created_by_user_id, ma.plant_id, ma.plant_name, ma.category_name,
		       ma.title, ma.description, ma.quantity, ma.size_description,
		       ma.price_per_unit, ma.price_unit, ma.photos,
		       ma.status, ma.view_count, ma.save_count, ma.enquiry_count,
		       ma.expires_at, ma.published_at,
		       ma.paused_at, ma.expired_at, ma.archived_at,
		       ma.created_at, ma.updated_at
		FROM public.market_ads ma
		JOIN public.nurseries n ON n.nursery_id = ma.nursery_id
		WHERE %s
		ORDER BY ma.published_at DESC NULLS LAST, ma.created_at DESC
		LIMIT $%d OFFSET $%d`, filter, limitIdx, offsetIdx)

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

func (r *PostgresRepository) IncrementViewCount(ctx context.Context, adID int64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE public.market_ads SET view_count = view_count + 1, updated_at = NOW()
		 WHERE ad_id = $1`,
		adID,
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
		_, _ = r.db.ExecContext(ctx,
			`UPDATE public.market_ads SET save_count = GREATEST(0, save_count - 1) WHERE ad_id = $1`,
			adID,
		)
		return false, nil
	}
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO public.market_ad_saves (ad_id, nursery_id, saved_by_user_id) VALUES ($1,$2,$3)`,
		adID, nurseryID, userID,
	); err != nil {
		return false, err
	}
	_, _ = r.db.ExecContext(ctx,
		`UPDATE public.market_ads SET save_count = save_count + 1 WHERE ad_id = $1`,
		adID,
	)
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
	return r.listAds(ctx, filter, q.Page, q.PerPage, []any{nurseryID})
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
	var pricePerUnit sql.NullFloat64
	var quantity sql.NullInt64
	var expiresAt, publishedAt, pausedAt, expiredAt, archivedAt sql.NullTime

	if err := row.Scan(
		&a.ID, &a.Code, &a.NurseryID, &a.NurseryName, &a.NurseryVerified, &nurseryMobile,
		&a.CreatedByUserID, &plantID, &a.PlantName, &categoryName,
		&a.Title, &description, &quantity, &sizeDesc,
		&pricePerUnit, &priceUnit, &photosRaw,
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
