package assistant

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
)

// Admin settings in platform_config; these defaults apply when a key is missing.
const (
	keyEnabled        = "assistant_enabled"
	keyDailyQuestions = "assistant_daily_questions"
	defaultQuestions  = 5
	maxQuestions      = 50
)

type settings struct {
	Enabled        bool `json:"enabled"`
	DailyQuestions int  `json:"daily_question_limit"`
}

func (h *Handler) settings(ctx context.Context) (settings, error) {
	s := settings{Enabled: true, DailyQuestions: defaultQuestions}
	rows, err := h.db.QueryContext(ctx, `SELECT config_key, config_value FROM public.platform_config
		WHERE config_key IN ($1, $2) AND is_active`, keyEnabled, keyDailyQuestions)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return s, err
		}
		switch key {
		case keyEnabled:
			s.Enabled = value == "true"
		case keyDailyQuestions:
			if n, err := strconv.Atoi(value); err == nil && n >= 0 {
				s.DailyQuestions = n
			}
		}
	}
	return s, rows.Err()
}

// Quota is a user's questions today. The day is India time (the business
// calendar), computed here so neither the phone nor the DB session decides.
type Quota struct {
	Used     int       `json:"questions_used"`
	Limit    int       `json:"questions_limit"`
	Left     int       `json:"questions_left"`
	ResetsAt time.Time `json:"resets_at"`
}

func (h *Handler) today() (string, time.Time) {
	now := h.now().In(h.loc)
	y, m, d := now.Date()
	return now.Format("2006-01-02"), time.Date(y, m, d+1, 0, 0, 0, 0, h.loc)
}

func quota(used, limit int, resets time.Time) Quota {
	return Quota{Used: used, Limit: limit, Left: max(limit-used, 0), ResetsAt: resets}
}

func (h *Handler) quota(ctx context.Context, userID int64, limit int) (Quota, error) {
	day, resets := h.today()
	var used int
	err := h.db.QueryRowContext(ctx, `SELECT questions_used FROM public.assistant_question_days WHERE user_id = $1 AND question_date = $2::date`,
		userID, day).Scan(&used)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return quota(used, limit, resets), err
}

// reserve takes one question slot in a single statement, so requests at the
// same moment cannot pass the limit. ok is false when none is left.
func (h *Handler) reserve(ctx context.Context, userID int64, limit int) (q Quota, day string, ok bool, err error) {
	day, resets := h.today()
	if limit <= 0 {
		return quota(0, limit, resets), day, false, nil
	}
	var used int
	err = h.db.QueryRowContext(ctx, `
		INSERT INTO public.assistant_question_days (user_id, question_date, questions_used) VALUES ($1, $2::date, 1)
		ON CONFLICT (user_id, question_date) DO UPDATE
			SET questions_used = assistant_question_days.questions_used + 1, updated_at = now()
			WHERE assistant_question_days.questions_used < $3
		RETURNING questions_used`, userID, day, limit).Scan(&used)
	if errors.Is(err, sql.ErrNoRows) {
		return quota(limit, limit, resets), day, false, nil
	}
	if err != nil {
		return Quota{}, day, false, err
	}
	return quota(used, limit, resets), day, true, nil
}

// refund gives the slot back when no answer was delivered.
func (h *Handler) refund(ctx context.Context, userID int64, day string) {
	_, err := h.db.ExecContext(context.WithoutCancel(ctx), `UPDATE public.assistant_question_days
		SET questions_used = questions_used - 1, updated_at = now()
		WHERE user_id = $1 AND question_date = $2::date AND questions_used > 0`, userID, day)
	if err != nil {
		h.log.Error("assistant question not refunded", "error", err)
	}
}

func limitReached(q Quota) *AIError {
	asked := "your " + strconv.Itoa(q.Limit) + " questions"
	if q.Limit == 1 {
		asked = "your question"
	}
	return &AIError{http.StatusTooManyRequests, "ai_daily_questions_used", "You have asked " + asked + " for today. Mango is back after midnight."}
}

func (h *Handler) admin(w http.ResponseWriter, r *http.Request) (authctx.Actor, bool) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return actor, false
	}
	if !actor.HasRole("ADMIN", "SUPER_ADMIN") {
		response.Error(w, http.StatusForbidden, "forbidden", "admins only")
		return actor, false
	}
	return actor, true
}

// AdminSettings: Mango on/off and the daily question limit.
func (h *Handler) AdminSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.admin(w, r); !ok {
		return
	}
	s, err := h.settings(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "assistant_error", "could not read Mango settings")
		return
	}
	response.OK(w, map[string]any{"settings": s, "server_ai_enabled": h.cfg.Enabled, "max_daily_question_limit": maxQuestions})
}

func (h *Handler) UpdateAdminSettings(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.admin(w, r)
	if !ok {
		return
	}
	var body settings
	if !response.DecodeJSON(w, r, &body) {
		return
	}
	if body.DailyQuestions < 0 || body.DailyQuestions > maxQuestions {
		response.Error(w, http.StatusBadRequest, "invalid_limit", "the daily limit must be 0 to "+strconv.Itoa(maxQuestions))
		return
	}
	old, err := h.settings(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "assistant_error", "could not read Mango settings")
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "assistant_error", "could not save")
		return
	}
	defer func() { _ = tx.Rollback() }()
	for _, c := range [][3]string{
		{keyEnabled, strconv.FormatBool(body.Enabled), "boolean"},
		{keyDailyQuestions, strconv.Itoa(body.DailyQuestions), "integer"},
	} {
		_, err = tx.ExecContext(r.Context(), `
			INSERT INTO public.platform_config (config_key, config_value, data_type, updated_by_user_id)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (config_key) DO UPDATE SET config_value = EXCLUDED.config_value, is_active = true,
				updated_by_user_id = EXCLUDED.updated_by_user_id, updated_at = now()`, c[0], c[1], c[2], actor.UserID)
		if err != nil {
			break
		}
	}
	if err != nil || tx.Commit() != nil {
		response.Error(w, http.StatusInternalServerError, "assistant_error", "could not save")
		return
	}
	h.audit.Log(r.Context(), auditlog.Entry{UserID: actor.UserID, Module: auditlog.ModuleAssistant, EntityType: "assistant_settings",
		Action: auditlog.ActionUpdate, Description: "Mango AI settings changed", OldValue: old, NewValue: body,
		IPAddress: actor.IPAddress, DeviceInfo: actor.UserAgent})
	response.OK(w, map[string]any{"settings": body})
}
