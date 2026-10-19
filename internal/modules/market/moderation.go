package market

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

// Moderation of reported market ads (Admin → Local Market). Nurseries report
// ads with POST /market/ads/{id}/report; admins review each report and may
// archive the ad. Admin-only.

// AdReport is one report with enough context to decide without a second call.
type AdReport struct {
	ID                    int64      `json:"id"`
	AdID                  int64      `json:"ad_id"`
	AdCode                string     `json:"ad_code"`
	AdTitle               string     `json:"ad_title"`
	AdStatus              string     `json:"ad_status"`
	AdNurseryName         string     `json:"ad_nursery_name"`
	Reason                string     `json:"reason"`
	Notes                 *string    `json:"notes,omitempty"`
	Status                string     `json:"status"` // PENDING | REVIEWED | DISMISSED
	ReportedByName        *string    `json:"reported_by_name,omitempty"`
	ReportedByNurseryName string     `json:"reported_by_nursery_name"`
	ReviewedByName        *string    `json:"reviewed_by_name,omitempty"`
	ReviewedAt            *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
}

type ReviewReportRequest struct {
	Status    string `json:"status"`     // REVIEWED (action taken) or DISMISSED
	ArchiveAd bool   `json:"archive_ad"` // also take the ad down
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
	req.Status = strings.ToUpper(strings.TrimSpace(req.Status))
	if req.Status != "REVIEWED" && req.Status != "DISMISSED" {
		writeError(w, fmt.Errorf("%w: status must be REVIEWED or DISMISSED", ErrInvalidInput))
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
	if req.ArchiveAd && report.AdStatus != StatusArchived {
		if _, err := m.service.ArchiveAd(r.Context(), actor, report.AdID); err != nil {
			writeError(w, err)
			return
		}
	}
	if _, err := m.db.ExecContext(r.Context(), `
		UPDATE public.market_ad_reports
		SET status = $2, reviewed_by_user_id = $3, reviewed_at = CURRENT_TIMESTAMP
		WHERE report_id = $1 AND status = 'PENDING'`, id, req.Status, actor.UserID); err != nil {
		writeError(w, err)
		return
	}
	updated, err := m.getReport(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, map[string]any{"report": updated})
}

const reportSelect = `
	SELECT rp.report_id, a.ad_id, a.ad_code, a.title, a.status, an.nursery_name,
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
	if err := row.Scan(&rep.ID, &rep.AdID, &rep.AdCode, &rep.AdTitle, &rep.AdStatus, &rep.AdNurseryName,
		&rep.Reason, &notes, &rep.Status, &reportedBy, &rep.ReportedByNurseryName, &reviewedBy, &reviewedAt, &rep.CreatedAt); err != nil {
		return AdReport{}, err
	}
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
		ORDER BY (rp.status = 'PENDING') DESC, rp.created_at DESC
		LIMIT $2 OFFSET $3`, status, perPage, (page-1)*perPage)
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
