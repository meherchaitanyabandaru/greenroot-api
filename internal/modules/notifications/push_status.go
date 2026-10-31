package notifications

import (
	"context"
	"net/http"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
)

// Push health for admins. Configuration (is a Firebase service account
// loaded?) and delivery (what happened to recent pushes) are reported apart:
// a configured sender can still be failing, and an unconfigured one sends
// nothing at all.
const (
	PushHealthNotConfigured = "NOT_CONFIGURED" // no service account: in-app only
	PushHealthNoTraffic     = "NO_TRAFFIC"     // configured, nothing tried in 24 h
	PushHealthOK            = "OK"
	PushHealthDegraded      = "DEGRADED" // over 10% of tries failed in 24 h
	PushHealthFailing       = "FAILING"  // half or more failed in 24 h
)

type PushWindow struct {
	Sent     int64 `json:"sent"`
	Failed   int64 `json:"failed"`
	NoDevice int64 `json:"no_device"`
}

type PushStats struct {
	ActiveDevices  int64            `json:"active_devices"`
	DevicesByOS    map[string]int64 `json:"active_devices_by_platform"`
	UsersWithPush  int64            `json:"users_with_active_device"`
	RetiredDevices int64            `json:"retired_devices"` // signed out or token reported dead
	LastDeviceSeen *time.Time       `json:"last_device_seen_at"`
	Last24h        PushWindow       `json:"last_24h"`
	Last7d         PushWindow       `json:"last_7d"`
	LastSentAt     *time.Time       `json:"last_sent_at"`
	LastFailedAt   *time.Time       `json:"last_failed_at"`
}

type PushStatusResponse struct {
	Configured bool      `json:"configured"`
	ProjectID  string    `json:"project_id,omitempty"`
	Health     string    `json:"health"`
	Stats      PushStats `json:"stats"`
	Note       string    `json:"note"`
}

// pushHealth reads the last 24 h: no-device outcomes are not delivery
// failures (the user simply has no phone signed in).
func pushHealth(configured bool, w PushWindow) string {
	tried := w.Sent + w.Failed
	switch {
	case !configured:
		return PushHealthNotConfigured
	case tried == 0:
		return PushHealthNoTraffic
	case w.Failed*2 >= tried:
		return PushHealthFailing
	case w.Failed*10 > tried:
		return PushHealthDegraded
	}
	return PushHealthOK
}

func (s *Service) PushStatus(ctx context.Context, actor ActorContext) (PushStatusResponse, error) {
	if !actor.HasRole("ADMIN") {
		return PushStatusResponse{}, ErrForbidden
	}
	stats, err := s.repository.PushStats(ctx)
	if err != nil {
		return PushStatusResponse{}, err
	}
	out := PushStatusResponse{Stats: stats,
		Note: "Counts are push attempts by the API's push sender; in-app inbox notifications are not counted. Last delivered and last failure cover the last 7 days."}
	if ps, ok := s.sender.(*PushSender); ok {
		out.Configured = true
		if p, ok := ps.push.(interface{ ProjectID() string }); ok {
			out.ProjectID = p.ProjectID()
		}
	}
	out.Health = pushHealth(out.Configured, stats.Last24h)
	return out, nil
}

// PushStats counts push outcomes (push_status), never the inbox status, which
// is SENT on creation and READ once opened.
func (r *PostgresRepository) PushStats(ctx context.Context) (PushStats, error) {
	st := PushStats{DevicesByOS: map[string]int64{}}
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FILTER (WHERE is_active),
		       COUNT(DISTINCT user_id) FILTER (WHERE is_active),
		       COUNT(*) FILTER (WHERE NOT is_active),
		       MAX(COALESCE(last_seen_at, updated_at)) FILTER (WHERE is_active)
		FROM public.user_notification_devices`).
		Scan(&st.ActiveDevices, &st.UsersWithPush, &st.RetiredDevices, &st.LastDeviceSeen)
	if err != nil {
		return st, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT COALESCE(UPPER(platform), 'UNKNOWN'), COUNT(*)
		FROM public.user_notification_devices WHERE is_active GROUP BY 1`)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var os string
		var n int64
		if err := rows.Scan(&os, &n); err != nil {
			return st, err
		}
		st.DevicesByOS[os] = n
	}
	if err := rows.Err(); err != nil {
		return st, err
	}
	err = r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FILTER (WHERE push_status = 'SENT' AND push_attempted_at >= NOW() - INTERVAL '24 hours'),
		       COUNT(*) FILTER (WHERE push_status = 'FAILED' AND push_attempted_at >= NOW() - INTERVAL '24 hours'),
		       COUNT(*) FILTER (WHERE push_status = 'NO_DEVICE' AND push_attempted_at >= NOW() - INTERVAL '24 hours'),
		       COUNT(*) FILTER (WHERE push_status = 'SENT'),
		       COUNT(*) FILTER (WHERE push_status = 'FAILED'),
		       COUNT(*) FILTER (WHERE push_status = 'NO_DEVICE'),
		       MAX(push_attempted_at) FILTER (WHERE push_status = 'SENT'),
		       MAX(push_attempted_at) FILTER (WHERE push_status = 'FAILED')
		FROM public.notifications
		WHERE push_attempted_at >= NOW() - INTERVAL '7 days'`).
		Scan(&st.Last24h.Sent, &st.Last24h.Failed, &st.Last24h.NoDevice,
			&st.Last7d.Sent, &st.Last7d.Failed, &st.Last7d.NoDevice, &st.LastSentAt, &st.LastFailedAt)
	return st, err
}

func (h *Handler) PushStatus(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	status, err := h.service.PushStatus(r.Context(), actor)
	if err != nil {
		writeError(w, err)
		return
	}
	response.OK(w, status)
}
