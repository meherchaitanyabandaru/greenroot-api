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
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

// Moderation of reported market ads (Admin → Local Market). Nurseries report
// ads with POST /market/ads/{id}/report; a serious report (seriousReasons)
// hides the ad as SUSPENDED at once. An admin then decides:
//   - DISMISS: the report was wrong; a suspended ad goes live again.
//   - REMOVE:  the ad is REMOVED for good, every open report on it closes,
//     and the nursery gets a strike. Strike 2 blocks posting for 30 days,
//     strike 3+ for 90 days; the admin can suspend the nursery as well.

// AdReport is one report with enough context to decide without a second call.
type AdReport struct {
	ID            int64    `json:"id"`
	AdID          int64    `json:"ad_id"`
	AdCode        string   `json:"ad_code"`
	AdTitle       string   `json:"ad_title"`
	AdStatus      string   `json:"ad_status"`
	AdNurseryName string   `json:"ad_nursery_name"`
	AdDescription *string  `json:"ad_description,omitempty"`
	AdPhotos      []string `json:"ad_photos"`
	// AdNurseryStrikes counts the nursery's ads GreenRoot already removed.
	AdNurseryStrikes      int        `json:"ad_nursery_strikes"`
	AdNurseryBlockedUntil *time.Time `json:"ad_nursery_blocked_until,omitempty"`
	Reason                string     `json:"reason"`
	Serious               bool       `json:"serious"` // hid the ad on report
	Notes                 *string    `json:"notes,omitempty"`
	Status                string     `json:"status"` // PENDING | REVIEWED | DISMISSED
	ReportedByName        *string    `json:"reported_by_name,omitempty"`
	ReportedByNurseryName string     `json:"reported_by_nursery_name"`
	ReviewedByName        *string    `json:"reviewed_by_name,omitempty"`
	ReviewedAt            *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
}

type ReviewReportRequest struct {
	Decision string `json:"decision"` // REMOVE (report upheld) or DISMISS
}

// ReviewOutcome tells the admin what the decision did to the seller.
type ReviewOutcome struct {
	Strikes      int        `json:"strikes"`
	BlockedUntil *time.Time `json:"blocked_until,omitempty"`
}

type moderationHandler struct {
	db      *sql.DB
	jwt     *jwtplatform.Service
	service *Service
}

func isAdmin(actor ActorContext) bool {
	return actor.HasRole("ADMIN") || actor.HasRole("SUPER_ADMIN")
}

func (m *moderationHandler) actor(w http.ResponseWriter, r *http.Request) (ActorContext, bool) {
	return (&Handler{jwt: m.jwt}).actor(w, r)
}

// ListReports godoc
//
//	@Summary	Reported market ads (admin)
//	@Tags		Market
//	@Security	BearerAuth
//	@Router		/api/v1/market/reports [get]
func (m *moderationHandler) ListReports(w http.ResponseWriter, r *http.Request) {
	actor, ok := m.actor(w, r)
	if !ok {
		return
	}
	if !isAdmin(actor) {
		writeError(w, ErrForbidden)
		return
	}
	status := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))
	page, perPage := intParam(r, "page", 1), intParam(r, "per_page", 20)
	if perPage > 100 {
		perPage = 100
	}
	reports, total, err := m.listReports(r.Context(), status, page, perPage)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, map[string]any{
		"reports":    reports,
		"pagination": map[string]any{"page": page, "per_page": perPage, "total": total, "total_pages": (total + int64(perPage) - 1) / int64(perPage)},
	})
}

// ReviewReport godoc
//
//	@Summary	Close a report as reviewed or dismissed, optionally archiving the ad (admin)
//	@Tags		Market
//	@Security	BearerAuth
//	@Router		/api/v1/market/reports/{id}/review [post]
func (m *moderationHandler) ReviewReport(w http.ResponseWriter, r *http.Request) {
	actor, ok := m.actor(w, r)
	if !ok {
		return
	}
	if !isAdmin(actor) {
		writeError(w, ErrForbidden)
		return
	}
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req ReviewReportRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Decision = strings.ToUpper(strings.TrimSpace(req.Decision))
	if req.Decision != "REMOVE" && req.Decision != "DISMISS" {
		writeError(w, fmt.Errorf("%w: decision must be REMOVE or DISMISS", ErrInvalidInput))
		return
	}
	report, err := m.getReport(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	if report.Status != "PENDING" {
		response.Error(w, http.StatusConflict, "already_reviewed", "this report was already "+strings.ToLower(report.Status))
		return
	}
	outcome, err := m.decide(r.Context(), actor, report, req.Decision == "REMOVE")
	if err != nil {
		writeError(w, err)
		return
	}
	updated, err := m.getReport(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, map[string]any{"report": updated, "outcome": outcome})
}

func (m *moderationHandler) decide(ctx context.Context, actor ActorContext, report AdReport, remove bool) (ReviewOutcome, error) {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return ReviewOutcome{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var nurseryID int64
	if err := tx.QueryRowContext(ctx,
		`SELECT nursery_id FROM public.market_ads WHERE ad_id = $1 FOR UPDATE`, report.AdID).Scan(&nurseryID); err != nil {
		return ReviewOutcome{}, err
	}
	var outcome ReviewOutcome
	if remove {
		outcome, err = removeAd(ctx, tx, report.AdID, nurseryID, actor.UserID,
			"Removed by GreenRoot: "+reasonLabel(report.Reason)+".")
		if err != nil {
			return ReviewOutcome{}, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			UPDATE public.market_ad_reports SET status = 'DISMISSED', reviewed_by_user_id = $2, reviewed_at = NOW()
			WHERE report_id = $1 AND status = 'PENDING'`, report.ID, actor.UserID); err != nil {
			return ReviewOutcome{}, err
		}
		// A suspended ad goes live again once no serious report is left open.
		if _, err := tx.ExecContext(ctx, `
			UPDATE public.market_ads SET status = 'PUBLISHED', moderation_note = NULL, moderated_at = NOW(), updated_at = NOW()
			WHERE ad_id = $1 AND status = 'SUSPENDED'
			  AND NOT EXISTS (SELECT 1 FROM public.market_ad_reports
			                  WHERE ad_id = $1 AND status = 'PENDING' AND reason = ANY($2))`,
			report.AdID, seriousReasonCodes()); err != nil {
			return ReviewOutcome{}, err
		}
	}
	return outcome, tx.Commit()
}

// removeAd takes an ad down for good inside tx: REMOVED with the seller's
// note, every open report on it closed as REVIEWED, and a strike for the
// nursery (the count of its removed ads) with the matching posting block.
func removeAd(ctx context.Context, tx *sql.Tx, adID, nurseryID, adminID int64, note string) (ReviewOutcome, error) {
	var outcome ReviewOutcome
	if _, err := tx.ExecContext(ctx, `
		UPDATE public.market_ads SET status = 'REMOVED', moderation_note = $2, moderated_at = NOW(), updated_at = NOW()
		WHERE ad_id = $1`, adID, note); err != nil {
		return outcome, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE public.market_ad_reports SET status = 'REVIEWED', reviewed_by_user_id = $2, reviewed_at = NOW()
		WHERE ad_id = $1 AND status = 'PENDING'`, adID, adminID); err != nil {
		return outcome, err
	}
	if err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM public.market_ads WHERE nursery_id = $1 AND status = 'REMOVED'`, nurseryID).Scan(&outcome.Strikes); err != nil {
		return outcome, err
	}
	if days := blockDays(outcome.Strikes); days > 0 {
		if err := tx.QueryRowContext(ctx, `
			UPDATE public.nurseries
			SET market_blocked_until_at = GREATEST(COALESCE(market_blocked_until_at, NOW()), NOW() + make_interval(days => $2))
			WHERE nursery_id = $1 RETURNING market_blocked_until_at`, nurseryID, days).Scan(&outcome.BlockedUntil); err != nil {
			return outcome, err
		}
	}
	return outcome, nil
}

// blockDays is the posting block for a nursery's nth removed ad: a warning
// first, then 30 days, then 90.
func blockDays(strikes int) int {
	switch {
	case strikes >= 3:
		return 90
	case strikes == 2:
		return 30
	default:
		return 0
	}
}

func seriousReasonCodes() []string {
	codes := make([]string, 0, len(seriousReasons))
	for code := range seriousReasons {
		codes = append(codes, code)
	}
	return codes
}

func reasonLabel(code string) string {
	if what, ok := seriousReasons[code]; ok {
		return what
	}
	switch code {
	case "SCAM":
		return "a scam"
	case "FRAUD":
		return "fake information"
	case "SPAM":
		return "spam"
	case "WRONG_PLANT":
		return "wrong or misleading plant details"
	case "DUPLICATE":
		return "a duplicate listing"
	default:
		return "breaking the market rules"
	}
}

const reportSelect = `
	SELECT rp.report_id, a.ad_id, a.ad_code, a.title, a.status, an.nursery_name,
		a.description, a.photos,
		(SELECT count(*) FROM public.market_ads x WHERE x.nursery_id = a.nursery_id AND x.status = 'REMOVED'),
		CASE WHEN an.market_blocked_until_at > NOW() THEN an.market_blocked_until_at END,
		rp.reason, rp.notes, rp.status,
		NULLIF(TRIM(CONCAT_WS(' ', ru.first_name, ru.last_name)), ''), rn.nursery_name,
		NULLIF(TRIM(CONCAT_WS(' ', vu.first_name, vu.last_name)), ''), rp.reviewed_at, rp.created_at
	FROM public.market_ad_reports rp
	JOIN public.market_ads a ON a.ad_id = rp.ad_id
	JOIN public.nurseries an ON an.nursery_id = a.nursery_id
	JOIN public.nurseries rn ON rn.nursery_id = rp.reported_by_nursery_id
	LEFT JOIN public.users ru ON ru.user_id = rp.reported_by_user_id
	LEFT JOIN public.users vu ON vu.user_id = rp.reviewed_by_user_id`

func scanReport(row interface{ Scan(...any) error }) (AdReport, error) {
	var rep AdReport
	var notes, reportedBy, reviewedBy sql.NullString
	var reviewedAt sql.NullTime
	var photos []byte
	if err := row.Scan(&rep.ID, &rep.AdID, &rep.AdCode, &rep.AdTitle, &rep.AdStatus, &rep.AdNurseryName,
		&rep.AdDescription, &photos, &rep.AdNurseryStrikes, &rep.AdNurseryBlockedUntil,
		&rep.Reason, &notes, &rep.Status, &reportedBy, &rep.ReportedByNurseryName, &reviewedBy, &reviewedAt, &rep.CreatedAt); err != nil {
		return AdReport{}, err
	}
	rep.AdPhotos = []string{}
	_ = json.Unmarshal(photos, &rep.AdPhotos)
	_, rep.Serious = seriousReasons[rep.Reason]
	if notes.Valid {
		rep.Notes = &notes.String
	}
	if reportedBy.Valid {
		rep.ReportedByName = &reportedBy.String
	}
	if reviewedBy.Valid {
		rep.ReviewedByName = &reviewedBy.String
	}
	if reviewedAt.Valid {
		rep.ReviewedAt = &reviewedAt.Time
	}
	return rep, nil
}

func (m *moderationHandler) listReports(ctx context.Context, status string, page, perPage int) ([]AdReport, int64, error) {
	var total int64
	if err := m.db.QueryRowContext(ctx, `SELECT count(*) FROM public.market_ad_reports WHERE $1 = '' OR status = $1`, status).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := m.db.QueryContext(ctx, reportSelect+`
		WHERE $1 = '' OR rp.status = $1
		ORDER BY (rp.status = 'PENDING') DESC, (rp.reason = ANY($4)) DESC, rp.created_at DESC
		LIMIT $2 OFFSET $3`, status, perPage, (page-1)*perPage, seriousReasonCodes())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	reports := []AdReport{}
	for rows.Next() {
		rep, err := scanReport(rows)
		if err != nil {
			return nil, 0, err
		}
		reports = append(reports, rep)
	}
	return reports, total, rows.Err()
}

func (m *moderationHandler) getReport(ctx context.Context, id int64) (AdReport, error) {
	rep, err := scanReport(m.db.QueryRowContext(ctx, reportSelect+` WHERE rp.report_id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return AdReport{}, ErrNotFound
	}
	return rep, err
}
