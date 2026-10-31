package assistant

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/auditlog"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/authctx"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/config"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/response"
	jwtplatform "github.com/meherchaitanyabandaru/greenroot-api/platform/jwt"
)

// NoticeVersion is the AI notice the app must have shown (header
// X-Mango-Notice). A new notice text means a new version.
const NoticeVersion = "1"

var audioTypes = map[string]bool{
	"audio/mp4": true, "audio/m4a": true, "audio/x-m4a": true, "audio/aac": true,
	"audio/wav": true, "audio/x-wav": true, "audio/wave": true,
}

type Handler struct {
	db     *sql.DB
	jwt    *jwtplatform.Service
	audit  *auditlog.Service
	cfg    config.AIConfig
	ai     aiService
	ledger ledger
	api    http.Handler
	loc    *time.Location
	log    *slog.Logger
	now    func() time.Time
}

type Module struct{ h *Handler }

// NewModule: api is the API's own router, for tools that call endpoints as
// the user; timeZone is the business calendar (DB_TIMEZONE).
func NewModule(db *sql.DB, jwt *jwtplatform.Service, audit *auditlog.Service, cfg config.AIConfig, timeZone string, api http.Handler, log *slog.Logger) Module {
	h := newHandler(db, jwt, cfg, timeZone, api, log, newHTTPAIService(cfg.ServiceURL, cfg.ServiceToken, cfg.Timeout))
	h.audit = audit
	return Module{h: h}
}

func newHandler(db *sql.DB, jwt *jwtplatform.Service, cfg config.AIConfig, timeZone string, api http.Handler, log *slog.Logger, ai aiService) *Handler {
	loc, err := time.LoadLocation(timeZone)
	if err != nil {
		loc = time.UTC
	}
	return &Handler{db: db, jwt: jwt, cfg: cfg, ai: ai, ledger: ledger{db: db, cfg: cfg}, api: api, loc: loc, log: log, now: time.Now}
}

func (m Module) RegisterRoutes(r chi.Router) {
	r.Get("/assistant/status", m.h.Status)
	r.Post("/assistant/transcribe", m.h.Transcribe)
	r.Post("/assistant/chat", m.h.Chat)
	r.Post("/assistant/speech", m.h.Speech)
	r.Post("/assistant/actions/{id}/confirm", m.h.ConfirmAction)
	r.Post("/assistant/actions/{id}/cancel", m.h.CancelAction)
	r.Delete("/assistant/data", m.h.DeleteData)
	r.Get("/admin/assistant/usage", m.h.AdminUsage)
	r.Get("/admin/assistant/settings", m.h.AdminSettings)
	r.Put("/admin/assistant/settings", m.h.UpdateAdminSettings)
}

func writeErr(w http.ResponseWriter, e *AIError) { response.Error(w, e.Status, e.Code, e.Message) }

// user: a nursery owner, manager, customer or driver. Admin tokens see
// every nursery, which must not reach the model.
func (h *Handler) user(w http.ResponseWriter, r *http.Request) (authctx.Actor, bool) {
	actor, ok := authctx.FromRequest(w, r, h.jwt)
	if !ok {
		return actor, false
	}
	if actor.HasRole("ADMIN", "SUPER_ADMIN") {
		response.Error(w, http.StatusForbidden, "assistant_not_for_admins", "Mango is for nursery owners, managers, customers and drivers")
		return actor, false
	}
	if !actor.HasRole("NURSERY_OWNER", "MANAGER", "BUYER", "DRIVER") {
		response.Error(w, http.StatusForbidden, "assistant_role_not_allowed", "Mango is for nursery owners, managers, customers and drivers")
		return actor, false
	}
	return actor, true
}

// begin is the gate before any AI call: user, notice, both switches
// (server env and admin setting), cost limits.
func (h *Handler) begin(w http.ResponseWriter, r *http.Request) (authctx.Actor, settings, bool) {
	actor, ok := h.user(w, r)
	if !ok {
		return actor, settings{}, false
	}
	if r.Header.Get("X-Mango-Notice") != NoticeVersion {
		response.Error(w, http.StatusPreconditionRequired, "ai_notice_required", "show the Mango AI notice first")
		return actor, settings{}, false
	}
	if !h.cfg.Enabled {
		response.Error(w, http.StatusServiceUnavailable, "ai_disabled", "Mango AI is switched off for now")
		return actor, settings{}, false
	}
	s, err := h.settings(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "assistant_error", "could not read Mango settings")
		return actor, s, false
	}
	if !s.Enabled {
		response.Error(w, http.StatusServiceUnavailable, "ai_disabled", "Mango AI is switched off for now")
		return actor, s, false
	}
	if e := h.ledger.allow(r.Context(), actor.UserID); e != nil {
		writeErr(w, e)
		return actor, s, false
	}
	// Model answers can take longer than the server's default write timeout.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(h.cfg.Timeout*4 + 10*time.Second))
	return actor, s, true
}

func (h *Handler) fail(w http.ResponseWriter, endpoint, requestID string, err error) {
	var e *AIError
	if !errors.As(err, &e) {
		e = &AIError{http.StatusBadGateway, "ai_provider_error", "Mango could not answer, try again"}
	}
	h.log.Warn("assistant call failed", "endpoint", endpoint, "request_id", requestID, "code", e.Code)
	writeErr(w, e)
}

// Status: what the app needs before showing Mango.
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.user(w, r)
	if !ok {
		return
	}
	s, err := h.ledger.spent(r.Context(), actor.UserID)
	set, errS := h.settings(r.Context())
	q, errQ := h.quota(r.Context(), actor.UserID, set.DailyQuestions)
	if err != nil || errS != nil || errQ != nil {
		response.Error(w, http.StatusInternalServerError, "assistant_error", "could not read Mango's status")
		return
	}
	response.OK(w, map[string]any{
		"enabled":           h.cfg.Enabled && set.Enabled,
		"notice_version":    NoticeVersion,
		"max_audio_seconds": h.cfg.MaxAudioSeconds,
		"max_text_chars":    h.cfg.MaxTextChars,
		"calls_left_today":  max(h.cfg.UserDailyCalls-s.UserDailyCalls, 0),
		"questions":         q,
	})
}

// Transcribe: one recording to text. The audio stays in memory for this
// request only and is never written to disk or stored.
func (h *Handler) Transcribe(w http.ResponseWriter, r *http.Request) {
	actor, set, ok := h.begin(w, r)
	if !ok {
		return
	}
	// A recording becomes a question only when it is sent to chat, but none
	// is transcribed (and paid for) once today's questions are used.
	if q, err := h.quota(r.Context(), actor.UserID, set.DailyQuestions); err != nil {
		response.Error(w, http.StatusInternalServerError, "assistant_error", "could not read Mango's limits")
		return
	} else if q.Left == 0 {
		writeErr(w, limitReached(q))
		return
	}
	rid := middleware.GetReqID(r.Context())
	limit := h.cfg.MaxAudioBytes + 64<<10
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	// maxMemory covers the whole body, so no part spills to a temp file.
	if err := r.ParseMultipartForm(limit); err != nil {
		response.Error(w, http.StatusRequestEntityTooLarge, "audio_too_large", "the recording is too large")
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	file, header, err := r.FormFile("audio")
	if err != nil {
		response.Error(w, http.StatusUnprocessableEntity, "audio_required", "send the recording as 'audio'")
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, h.cfg.MaxAudioBytes+1))
	_ = file.Close()
	switch {
	case err != nil:
		response.Error(w, http.StatusUnprocessableEntity, "corrupt_audio", "the recording could not be read")
		return
	case len(data) == 0:
		response.Error(w, http.StatusUnprocessableEntity, "empty_audio", "the recording is empty")
		return
	case int64(len(data)) > h.cfg.MaxAudioBytes:
		response.Error(w, http.StatusRequestEntityTooLarge, "audio_too_large", "the recording is too large")
		return
	}
	declared := strings.ToLower(strings.TrimSpace(strings.Split(header.Header.Get("Content-Type"), ";")[0]))
	if !audioTypes[declared] {
		response.Error(w, http.StatusUnsupportedMediaType, "unsupported_audio", "record as M4A (AAC) or WAV")
		return
	}
	contentType, seconds, err := audioInfo(data)
	switch {
	case errors.Is(err, errUnsupportedAudio):
		response.Error(w, http.StatusUnsupportedMediaType, "unsupported_audio", "record as M4A (AAC) or WAV")
		return
	case err != nil:
		response.Error(w, http.StatusUnprocessableEntity, "corrupt_audio", "the recording could not be read")
		return
	case seconds > h.cfg.MaxAudioSeconds+0.5:
		response.Error(w, http.StatusRequestEntityTooLarge, "audio_too_long", "keep the question under "+strconv.Itoa(int(h.cfg.MaxAudioSeconds))+" seconds")
		return
	case seconds < 0.3:
		response.Error(w, http.StatusUnprocessableEntity, "audio_too_short", "the recording is too short")
		return
	}
	language := strings.ToLower(r.FormValue("language"))
	if language == "auto" {
		language = ""
	}
	if !validLanguage(language) {
		response.Error(w, http.StatusUnprocessableEntity, "invalid_language", "language must be te, hi, kn, mr, ta, ur, en or auto")
		return
	}
	name := "speech.m4a"
	if contentType == "audio/wav" {
		name = "speech.wav"
	}
	text, usage, err := h.ai.transcribe(r.Context(), rid, data, name, contentType, language)
	if err != nil {
		h.fail(w, "transcribe", rid, err)
		return
	}
	if usage.AudioSeconds == 0 {
		usage.AudioSeconds = seconds
	}
	h.ledger.record(r.Context(), h.log, actor.UserID, kindSTT, usage, rid)
	response.OK(w, map[string]any{"text": strings.TrimSpace(text), "seconds": seconds, "request_id": rid})
}

type chatRequest struct {
	Message string `json:"message"`
	History []turn `json:"history"`
	// The language the user chose in the app (te, hi, kn, mr, ta, ur, en);
	// the script they wrote in still decides, so English gets English.
	Language string `json:"language"`
}

// Chat answers one typed or corrected message.
func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
	actor, set, ok := h.begin(w, r)
	if !ok {
		return
	}
	rid := middleware.GetReqID(r.Context())
	var body chatRequest
	if !response.DecodeJSON(w, r, &body) {
		return
	}
	body.Message = strings.TrimSpace(body.Message)
	if body.Message == "" {
		response.Error(w, http.StatusUnprocessableEntity, "message_required", "type or say your question")
		return
	}
	if len([]rune(body.Message)) > h.cfg.MaxTextChars {
		response.Error(w, http.StatusUnprocessableEntity, "message_too_long", "keep the question under "+strconv.Itoa(h.cfg.MaxTextChars)+" characters")
		return
	}
	// The app keeps the conversation; the server stores none of it. Only the
	// last few turns are used, as plain text.
	history := make([]turn, 0, 6)
	for _, t := range body.History[max(len(body.History)-6, 0):] {
		if (t.Role == "user" || t.Role == "assistant") && strings.TrimSpace(t.Text) != "" {
			history = append(history, turn{Role: t.Role, Text: clip(t.Text, 600)})
		}
	}
	// One question counts once it is answered: the slot is taken before the
	// model runs (so parallel requests cannot pass the limit) and given back
	// if no answer is delivered.
	q, day, ok, err := h.reserve(r.Context(), actor.UserID, set.DailyQuestions)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "assistant_error", "could not check Mango's limits")
		return
	}
	if !ok {
		writeErr(w, limitReached(q))
		return
	}
	env := &toolEnv{
		actor: actor, db: h.db, loc: h.loc, now: h.now(),
		api: caller{api: h.api, auth: r.Header.Get("Authorization"), remoteAddr: r.RemoteAddr, userAgent: r.UserAgent(), requestID: rid},
	}
	started := time.Now()
	reply, used, err := h.converse(r.Context(), env, rid, history, body.Message, body.Language)
	if err != nil {
		h.refund(r.Context(), actor.UserID, day)
		h.fail(w, "chat", rid, err)
		return
	}
	if reply == "" {
		h.refund(r.Context(), actor.UserID, day)
		q = quota(q.Used-1, q.Limit, q.ResetsAt)
		reply = "Sorry, I could not answer that. Please try asking differently."
	}
	h.log.Info("assistant chat", "request_id", rid, "tools", used, "proposal", env.proposal != nil, "ms", time.Since(started).Milliseconds())
	out := map[string]any{"reply": reply, "used_tools": used, "request_id": rid, "questions": q}
	if env.proposal != nil {
		out["proposal"] = env.proposal
	}
	response.OK(w, out)
}

// Speech reads a reply aloud (MP3).
func (h *Handler) Speech(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := h.begin(w, r)
	if !ok {
		return
	}
	rid := middleware.GetReqID(r.Context())
	var body struct {
		Text     string `json:"text"`
		Language string `json:"language"` // only tells Hindi from Marathi
	}
	if !response.DecodeJSON(w, r, &body) {
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		response.Error(w, http.StatusUnprocessableEntity, "empty_text", "nothing to read aloud")
		return
	}
	if len([]rune(text)) > h.cfg.MaxSpeechChars {
		response.Error(w, http.StatusUnprocessableEntity, "text_too_long", "too long to read aloud")
		return
	}
	// Same wording as the app's greeting clips (greenroot-ai scripts/make_greetings.py).
	instructions := voiceInstructions(replyLang(text, body.Language))
	audio, usage, err := h.ai.speak(r.Context(), rid, text, instructions)
	if err != nil {
		h.fail(w, "speech", rid, err)
		return
	}
	h.ledger.record(r.Context(), h.log, actor.UserID, kindTTS, usage, rid)
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Request-Id", rid)
	_, _ = w.Write(audio)
}

// ConfirmAction runs a proposal: once, by the user who got it, before it
// expires, through the normal endpoint (which checks permission and the
// order's state again).
func (h *Handler) ConfirmAction(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.user(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.Error(w, http.StatusNotFound, "action_not_found", "no such Mango action")
		return
	}
	var kind string
	var target int64
	err = h.db.QueryRowContext(r.Context(), `
		UPDATE public.assistant_actions SET status = 'DONE', decided_at = now()
		WHERE assistant_action_id = $1 AND user_id = $2 AND status = 'PENDING' AND expires_at > now()
		RETURNING action_type, target_id`, id, actor.UserID).Scan(&kind, &target)
	if errors.Is(err, sql.ErrNoRows) {
		h.whyNot(w, r, id, actor.UserID)
		return
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "assistant_error", "could not run this action")
		return
	}
	api := caller{api: h.api, auth: r.Header.Get("Authorization"), remoteAddr: r.RemoteAddr, userAgent: r.UserAgent(), requestID: middleware.GetReqID(r.Context())}
	var status int
	var body []byte
	if kind == "CONFIRM_ORDER" {
		status, body = api.call(r.Context(), http.MethodPost, "/orders/"+strconv.FormatInt(target, 10)+"/confirm", nil, nil)
	}
	if status != http.StatusOK {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &e)
		msg := e.Error.Message
		if msg == "" {
			msg = "GreenRoot could not do this"
		}
		_, _ = h.db.ExecContext(r.Context(), `UPDATE public.assistant_actions SET status = 'FAILED', result_message = $2 WHERE assistant_action_id = $1`, id, msg)
		response.Error(w, http.StatusConflict, "action_failed", msg)
		return
	}
	response.OK(w, map[string]any{"status": "DONE", "message": "Done: the order is confirmed."})
}

func (h *Handler) whyNot(w http.ResponseWriter, r *http.Request, id, userID int64) {
	var status string
	var expired bool
	err := h.db.QueryRowContext(r.Context(), `SELECT status, expires_at <= now() FROM public.assistant_actions WHERE assistant_action_id = $1 AND user_id = $2`, id, userID).Scan(&status, &expired)
	switch {
	case err != nil:
		response.Error(w, http.StatusNotFound, "action_not_found", "no such Mango action")
	case status == "PENDING" && expired:
		response.Error(w, http.StatusConflict, "action_expired", "this suggestion expired; ask Mango again")
	default:
		response.Error(w, http.StatusConflict, "action_already_decided", "this suggestion was already answered")
	}
}

func (h *Handler) CancelAction(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.user(w, r)
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	res, err := h.db.ExecContext(r.Context(), `
		UPDATE public.assistant_actions SET status = 'CANCELLED', decided_at = now()
		WHERE assistant_action_id = $1 AND user_id = $2 AND status = 'PENDING'`, id, actor.UserID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "assistant_error", "could not cancel")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		h.whyNot(w, r, id, actor.UserID)
		return
	}
	response.OK(w, map[string]any{"status": "CANCELLED"})
}

// DeleteData removes what Mango keeps about the user: proposals are
// deleted; usage rows lose the user (the totals stay for the budget).
func (h *Handler) DeleteData(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.user(w, r)
	if !ok {
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "assistant_error", "could not delete")
		return
	}
	defer func() { _ = tx.Rollback() }()
	a, errA := tx.ExecContext(r.Context(), `DELETE FROM public.assistant_actions WHERE user_id = $1`, actor.UserID)
	u, errU := tx.ExecContext(r.Context(), `UPDATE public.assistant_usage_events SET user_id = NULL WHERE user_id = $1`, actor.UserID)
	if errA != nil || errU != nil || tx.Commit() != nil {
		response.Error(w, http.StatusInternalServerError, "assistant_error", "could not delete")
		return
	}
	actions, _ := a.RowsAffected()
	usage, _ := u.RowsAffected()
	response.OK(w, map[string]any{"deleted_actions": actions, "anonymized_usage_rows": usage})
}

// AdminUsage: spend against the budget, by kind, today and this month.
func (h *Handler) AdminUsage(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.admin(w, r); !ok {
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT usage_kind,
			COUNT(*) FILTER (WHERE created_at >= date_trunc('day', now())),
			COALESCE(SUM(estimated_cost_usd) FILTER (WHERE created_at >= date_trunc('day', now())), 0)::float8,
			COUNT(*),
			COALESCE(SUM(estimated_cost_usd), 0)::float8,
			COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0), COALESCE(SUM(audio_seconds), 0)::float8
		FROM public.assistant_usage_events
		WHERE created_at >= date_trunc('month', now())
		GROUP BY usage_kind ORDER BY usage_kind`)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "assistant_error", "could not read usage")
		return
	}
	defer rows.Close()
	type kindUsage struct {
		Kind         string  `json:"kind"`
		CallsToday   int     `json:"calls_today"`
		USDToday     float64 `json:"estimated_usd_today"`
		CallsMonth   int     `json:"calls_month"`
		USDMonth     float64 `json:"estimated_usd_month"`
		InputTokens  int64   `json:"input_tokens_month"`
		OutputTokens int64   `json:"output_tokens_month"`
		AudioSeconds float64 `json:"audio_seconds_month"`
	}
	kinds := []kindUsage{}
	for rows.Next() {
		var k kindUsage
		if err := rows.Scan(&k.Kind, &k.CallsToday, &k.USDToday, &k.CallsMonth, &k.USDMonth, &k.InputTokens, &k.OutputTokens, &k.AudioSeconds); err != nil {
			response.Error(w, http.StatusInternalServerError, "assistant_error", "could not read usage")
			return
		}
		kinds = append(kinds, k)
	}
	if rows.Err() != nil {
		response.Error(w, http.StatusInternalServerError, "assistant_error", "could not read usage")
		return
	}
	response.OK(w, map[string]any{
		"enabled": h.cfg.Enabled,
		"usage":   kinds,
		"limits": map[string]any{
			"global_daily_usd": h.cfg.GlobalDailyUSD, "global_monthly_usd": h.cfg.GlobalMonthlyUSD,
			"user_daily_usd": h.cfg.UserDailyUSD, "user_daily_calls": h.cfg.UserDailyCalls, "user_minute_calls": h.cfg.UserMinuteCalls,
			"max_audio_seconds": h.cfg.MaxAudioSeconds, "max_audio_bytes": h.cfg.MaxAudioBytes,
		},
		"note": "Estimates from configured prices; the provider's dashboard is the bill.",
	})
}
