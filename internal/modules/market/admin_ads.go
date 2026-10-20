package market

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
)

// Admin → Local Market → All ads: every ad in any status, and direct
// actions an admin takes without waiting for a report:
//   - HIDE:    a live or paused ad goes SUSPENDED (under review) with a note.
//   - RESTORE: a suspended ad goes live again; its open reports are dismissed.
//   - REMOVE:  REMOVED for good with a note, a strike and any posting block,
//     exactly as when a report is upheld.
// Unblocking lifts a nursery's posting block early.

// AdminAd is one row of the admin ad list.
type AdminAd struct {
	ID                 int64      `json:"id"`
	Code               string     `json:"code"`
	AdType             string     `json:"ad_type"`
	Title              string     `json:"title"`
	PlantName          string     `json:"plant_name"`
	Description        *string    `json:"description,omitempty"`
	Photos             []string   `json:"photos"`
	Status             string     `json:"status"`
	ModerationNote     *string    `json:"moderation_note,omitempty"`
	Quantity           *int       `json:"quantity,omitempty"`
	PricePerUnit       *float64   `json:"price_per_unit,omitempty"`
	PriceUnit          *string    `json:"price_unit,omitempty"`
	ViewCount          int        `json:"view_count"`
	EnquiryCount       int        `json:"enquiry_count"`
	PendingReports     int        `json:"pending_reports"`
	NurseryID          int64      `json:"nursery_id"`
	NurseryName        string     `json:"nursery_name"`
	NurseryVerified    bool       `json:"nursery_verified"`
	NurseryStrikes     int        `json:"nursery_strikes"`
	NurseryBlockedTill *time.Time `json:"nursery_blocked_until,omitempty"`
	PostedByName       *string    `json:"posted_by_name,omitempty"`
	PostedByPhoto      *string    `json:"posted_by_photo,omitempty"`
	PublishedAt        *time.Time `json:"published_at,omitempty"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`

	SizeDescription *string `json:"size_description,omitempty"`
	NeededBy        *string `json:"needed_by,omitempty"`
	// Where buyers collect. HasLocation false means nearby lists never show
	// the ad (they filter by distance).
	HasLocation        bool     `json:"has_location"`
	PickupLatitude     *float64 `json:"pickup_latitude,omitempty"`
	PickupLongitude    *float64 `json:"pickup_longitude,omitempty"`
	PickupSource       *string  `json:"pickup_location_source,omitempty"`
	PickupAddress      *string  `json:"pickup_address,omitempty"`
	PickupPoint        *string  `json:"pickup_landmark,omitempty"`
	RoadAccessible     *bool    `json:"road_accessible,omitempty"`
	TruckAccessible    *bool    `json:"truck_accessible,omitempty"`
	AccessInstructions *string  `json:"access_instructions,omitempty"`
	// Owner soft delete.
	DeletedAt     *time.Time `json:"deleted_at,omitempty"`
	DeletedByName *string    `json:"deleted_by_name,omitempty"`
}

type ModerateAdRequest struct {
	Action string `json:"action"` // HIDE | RESTORE | REMOVE
	Note   string `json:"note"`   // shown to the seller; required for HIDE and REMOVE
}

var adminAdStatuses = map[string]bool{
	StatusDraft: true, StatusPublished: true, StatusPaused: true, StatusExpired: true,
	StatusArchived: true, StatusSuspended: true, StatusRemoved: true, StatusDeleted: true,
}

const adminAdSelect = `
	SELECT a.ad_id, a.ad_code, a.ad_type, a.title, a.plant_name, a.description, a.photos, a.status, a.moderation_note,
	       a.quantity, a.price_per_unit, a.price_unit, a.view_count, a.enquiry_count,
	       (SELECT count(*) FROM public.market_ad_reports rp WHERE rp.ad_id = a.ad_id AND rp.status = 'PENDING'),
	       n.nursery_id, n.nursery_name, (n.status IN ('ACTIVE', 'APPROVED')),
	       (SELECT count(*) FROM public.market_ads x WHERE x.nursery_id = n.nursery_id AND x.status = 'REMOVED'),
	       CASE WHEN n.market_blocked_until > NOW() THEN n.market_blocked_until END,
	       NULLIF(TRIM(CONCAT_WS(' ', u.first_name, u.last_name)), ''), u.profile_image_url,
	       a.published_at, a.expires_at, a.created_at,
	       a.size_description, to_char(a.needed_by, 'YYYY-MM-DD'), a.pickup_location IS NOT NULL,
	       a.pickup_latitude, a.pickup_longitude, a.pickup_location_source, a.pickup_address, a.pickup_landmark,
	       a.is_road_accessible, a.is_truck_accessible, a.access_instructions,
	       a.deleted_at, NULLIF(TRIM(CONCAT_WS(' ', du.first_name, du.last_name)), '')
	FROM public.market_ads a
	JOIN public.nurseries n ON n.nursery_id = a.nursery_id
	LEFT JOIN public.users u ON u.user_id = a.created_by_user_id
	LEFT JOIN public.users du ON du.user_id = a.deleted_by_user_id`

func scanAdminAd(row interface{ Scan(...any) error }) (AdminAd, error) {
	var a AdminAd
	var photos []byte
	err := row.Scan(&a.ID, &a.Code, &a.AdType, &a.Title, &a.PlantName, &a.Description, &photos, &a.Status, &a.ModerationNote,
		&a.Quantity, &a.PricePerUnit, &a.PriceUnit, &a.ViewCount, &a.EnquiryCount, &a.PendingReports,
		&a.NurseryID, &a.NurseryName, &a.NurseryVerified, &a.NurseryStrikes, &a.NurseryBlockedTill,
		&a.PostedByName, &a.PostedByPhoto, &a.PublishedAt, &a.ExpiresAt, &a.CreatedAt,
		&a.SizeDescription, &a.NeededBy, &a.HasLocation,
		&a.PickupLatitude, &a.PickupLongitude, &a.PickupSource, &a.PickupAddress, &a.PickupPoint,
		&a.RoadAccessible, &a.TruckAccessible, &a.AccessInstructions,
		&a.DeletedAt, &a.DeletedByName)
	a.Photos = []string{}
	_ = json.Unmarshal(photos, &a.Photos)
	return a, err
}

// adminOnly writes 401/403 and reports false unless an admin is calling.
func (m *moderationHandler) adminOnly(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	actor, ok := m.actor(w, r)
	if !ok {
		return ActorContext{}, false
	}
	if !isAdmin(actor) {
		writeError(w, ErrForbidden)
		return ActorContext{}, false
	}
	return actor, true
}

// ListAllAds godoc
//
//	@Summary	All market ads in any status, with reports and seller history (admin)
//	@Tags		Market
//	@Security	BearerAuth
//	@Router		/api/v1/market/admin/ads [get]
func (m *moderationHandler) ListAllAds(w http.ResponseWriter, r *http.Request) {
	if _, ok := m.adminOnly(w, r); !ok {
		return
	}
	status := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))
	if status != "" && !adminAdStatuses[status] {
		writeError(w, fmt.Errorf("%w: unknown status %q", ErrInvalidInput, status))
		return
	}
	search := strings.TrimSpace(r.URL.Query().Get("search"))
	// no_location=true: ads with no position, which nearby lists never show.
	noLocation := r.URL.Query().Get("no_location") == "true"
	page, perPage := intParam(r, "page", 1), intParam(r, "per_page", 20)
	if perPage > 100 {
		perPage = 100
	}
	const where = `
		WHERE ($1::text = '' OR a.status = $1::text)
		  AND ($2::text = '' OR a.title ILIKE '%' || $2::text || '%' OR a.plant_name ILIKE '%' || $2::text || '%'
		       OR a.ad_code ILIKE '%' || $2::text || '%' OR n.nursery_name ILIKE '%' || $2::text || '%')
		  AND (NOT $3::boolean OR a.pickup_location IS NULL)`
	var total int64
	if err := m.db.QueryRowContext(r.Context(),
		`SELECT count(*) FROM public.market_ads a JOIN public.nurseries n ON n.nursery_id = a.nursery_id`+where,
		status, search, noLocation).Scan(&total); err != nil {
		writeError(w, err)
		return
	}
	// Ads needing attention first: under review, then ones with open reports.
	rows, err := m.db.QueryContext(r.Context(), adminAdSelect+where+`
		ORDER BY (a.status = 'SUSPENDED') DESC,
		         EXISTS (SELECT 1 FROM public.market_ad_reports rp WHERE rp.ad_id = a.ad_id AND rp.status = 'PENDING') DESC,
		         a.updated_at DESC
		LIMIT $4 OFFSET $5`, status, search, noLocation, perPage, (page-1)*perPage)
	if err != nil {
		writeError(w, err)
		return
	}
	defer rows.Close()
	ads := []AdminAd{}
	for rows.Next() {
		a, err := scanAdminAd(rows)
		if err != nil {
			writeError(w, err)
			return
		}
		ads = append(ads, a)
	}
	if err := rows.Err(); err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, map[string]any{
		"ads":        ads,
		"pagination": map[string]any{"page": page, "per_page": perPage, "total": total, "total_pages": (total + int64(perPage) - 1) / int64(perPage)},
	})
}

// ModerateAd godoc
//
//	@Summary	Hide, restore or remove an ad directly (admin)
//	@Tags		Market
//	@Security	BearerAuth
//	@Router		/api/v1/market/admin/ads/{id}/moderate [post]
func (m *moderationHandler) ModerateAd(w http.ResponseWriter, r *http.Request) {
	actor, ok := m.adminOnly(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req ModerateAdRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Action = strings.ToUpper(strings.TrimSpace(req.Action))
	req.Note = strings.TrimSpace(req.Note)
	if req.Action != "HIDE" && req.Action != "RESTORE" && req.Action != "REMOVE" {
		writeError(w, fmt.Errorf("%w: action must be HIDE, RESTORE or REMOVE", ErrInvalidInput))
		return
	}
	if req.Action != "RESTORE" && req.Note == "" {
		writeError(w, fmt.Errorf("%w: tell the seller why (note)", ErrInvalidInput))
		return
	}
	if len(req.Note) > 500 {
		writeError(w, fmt.Errorf("%w: note is too long (500 characters max)", ErrInvalidInput))
		return
	}
	outcome, err := m.moderate(r.Context(), actor, id, req)
	if err != nil {
		writeError(w, err)
		return
	}
	ad, err := scanAdminAd(m.db.QueryRowContext(r.Context(), adminAdSelect+` WHERE a.ad_id = $1`, id))
	if err != nil {
		writeError(w, err)
		return
	}
	m.service.bumpMarketSearchCache(r.Context(), actor.UserID)
	response.OK(w, map[string]any{"ad": ad, "outcome": outcome})
}

func (m *moderationHandler) moderate(ctx context.Context, actor ActorContext, id int64, req ModerateAdRequest) (ReviewOutcome, error) {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return ReviewOutcome{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var nurseryID int64
	var status string
	err = tx.QueryRowContext(ctx,
		`SELECT nursery_id, status FROM public.market_ads WHERE ad_id = $1 FOR UPDATE`, id).Scan(&nurseryID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return ReviewOutcome{}, ErrNotFound
	}
	if err != nil {
		return ReviewOutcome{}, err
	}

	var outcome ReviewOutcome
	switch req.Action {
	case "HIDE":
		if status != StatusPublished && status != StatusPaused {
			return ReviewOutcome{}, fmt.Errorf("%w: only a live or paused ad can be hidden (this one is %s)", ErrConflict, status)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE public.market_ads SET status = 'SUSPENDED', moderation_note = $2, moderated_at = NOW(), updated_at = NOW()
			WHERE ad_id = $1`, id, "Hidden by GreenRoot for review: "+req.Note); err != nil {
			return ReviewOutcome{}, err
		}
	case "RESTORE":
		if status != StatusSuspended {
			return ReviewOutcome{}, fmt.Errorf("%w: only an ad under review can be restored (this one is %s)", ErrConflict, status)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE public.market_ads SET status = 'PUBLISHED', moderation_note = NULL, moderated_at = NOW(), updated_at = NOW()
			WHERE ad_id = $1`, id); err != nil {
			return ReviewOutcome{}, err
		}
		// The admin looked and found nothing wrong: close the open reports.
		if _, err := tx.ExecContext(ctx, `
			UPDATE public.market_ad_reports SET status = 'DISMISSED', reviewed_by_user_id = $2, reviewed_at = NOW()
			WHERE ad_id = $1 AND status = 'PENDING'`, id, actor.UserID); err != nil {
			return ReviewOutcome{}, err
		}
	case "REMOVE":
		if status == StatusRemoved {
			return ReviewOutcome{}, fmt.Errorf("%w: this ad is already removed", ErrConflict)
		}
		if status == StatusDeleted {
			return ReviewOutcome{}, fmt.Errorf("%w: the owner already deleted this ad", ErrConflict)
		}
		outcome, err = removeAd(ctx, tx, id, nurseryID, actor.UserID, "Removed by GreenRoot: "+req.Note)
		if err != nil {
			return ReviewOutcome{}, err
		}
	}
	return outcome, tx.Commit()
}

// UnblockNursery godoc
//
//	@Summary	Lift a nursery's market posting block early (admin)
//	@Tags		Market
//	@Security	BearerAuth
//	@Router		/api/v1/market/admin/nurseries/{id}/unblock [post]
func (m *moderationHandler) UnblockNursery(w http.ResponseWriter, r *http.Request) {
	if _, ok := m.adminOnly(w, r); !ok {
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	res, err := m.db.ExecContext(r.Context(),
		`UPDATE public.nurseries SET market_blocked_until = NULL, updated_at = NOW() WHERE nursery_id = $1`, id)
	if err != nil {
		writeError(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, ErrNotFound)
		return
	}
	response.OK(w, map[string]any{"nursery_id": id, "blocked_until": nil})
}

// BlockedNursery is a nursery currently barred from posting market ads.
type BlockedNursery struct {
	NurseryID    int64     `json:"nursery_id"`
	NurseryName  string    `json:"nursery_name"`
	Strikes      int       `json:"strikes"`
	BlockedUntil time.Time `json:"blocked_until"`
}

// ListBlockedNurseries godoc
//
//	@Summary	Nurseries currently blocked from posting market ads (admin)
//	@Tags		Market
//	@Security	BearerAuth
//	@Router		/api/v1/market/admin/blocked-nurseries [get]
func (m *moderationHandler) ListBlockedNurseries(w http.ResponseWriter, r *http.Request) {
	if _, ok := m.adminOnly(w, r); !ok {
		return
	}
	rows, err := m.db.QueryContext(r.Context(), `
		SELECT n.nursery_id, n.nursery_name,
		       (SELECT count(*) FROM public.market_ads x WHERE x.nursery_id = n.nursery_id AND x.status = 'REMOVED'),
		       n.market_blocked_until
		FROM public.nurseries n
		WHERE n.market_blocked_until > NOW()
		ORDER BY n.market_blocked_until DESC
		LIMIT 200`)
	if err != nil {
		writeError(w, err)
		return
	}
	defer rows.Close()
	out := []BlockedNursery{}
	for rows.Next() {
		var b BlockedNursery
		if err := rows.Scan(&b.NurseryID, &b.NurseryName, &b.Strikes, &b.BlockedUntil); err != nil {
			writeError(w, err)
			return
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, map[string]any{"nurseries": out})
}
