package assistant

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/config"
)

const (
	kindSTT = "STT"
	kindLLM = "LLM"
	kindTTS = "TTS"
)

// ledger records every provider call and refuses new ones past a limit.
// The figures are estimates from configured prices; the provider's own
// dashboard is the bill.
type ledger struct {
	db  *sql.DB
	cfg config.AIConfig
}

type spend struct {
	UserMinuteCalls int     `json:"user_minute_calls"`
	UserDailyCalls  int     `json:"user_daily_calls"`
	UserDailyUSD    float64 `json:"user_daily_usd"`
	GlobalDailyUSD  float64 `json:"global_daily_usd"`
	GlobalMonthUSD  float64 `json:"global_month_usd"`
}

// Day and month follow the database session zone (the business calendar).
func (l ledger) spent(ctx context.Context, userID int64) (spend, error) {
	var s spend
	err := l.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE user_id = $1 AND created_at > now() - interval '1 minute'),
			COUNT(*) FILTER (WHERE user_id = $1 AND created_at >= date_trunc('day', now())),
			COALESCE(SUM(estimated_cost_usd) FILTER (WHERE user_id = $1 AND created_at >= date_trunc('day', now())), 0)::float8,
			COALESCE(SUM(estimated_cost_usd) FILTER (WHERE created_at >= date_trunc('day', now())), 0)::float8,
			COALESCE(SUM(estimated_cost_usd), 0)::float8
		FROM public.assistant_usage_events
		WHERE created_at >= date_trunc('month', now())`, userID).
		Scan(&s.UserMinuteCalls, &s.UserDailyCalls, &s.UserDailyUSD, &s.GlobalDailyUSD, &s.GlobalMonthUSD)
	return s, err
}

// allow says whether the user may start another request. Requests already
// running can pass a limit by a few calls; the limits are set well below
// the budget for that reason.
func (l ledger) allow(ctx context.Context, userID int64) *AIError {
	s, err := l.spent(ctx, userID)
	if err != nil {
		return &AIError{http.StatusInternalServerError, "assistant_error", "could not check Mango's limits"}
	}
	switch {
	case s.GlobalMonthUSD >= l.cfg.GlobalMonthlyUSD || s.GlobalDailyUSD >= l.cfg.GlobalDailyUSD:
		return &AIError{http.StatusServiceUnavailable, "ai_budget_reached", "Mango has reached today's limit for everyone. Please try tomorrow."}
	case s.UserDailyCalls >= l.cfg.UserDailyCalls || s.UserDailyUSD >= l.cfg.UserDailyUSD:
		return &AIError{http.StatusTooManyRequests, "ai_daily_limit", "You have used Mango a lot today. Please try tomorrow."}
	case s.UserMinuteCalls >= l.cfg.UserMinuteCalls:
		return &AIError{http.StatusTooManyRequests, "ai_slow_down", "Please wait a minute before asking Mango again."}
	}
	return nil
}

func (l ledger) cost(kind string, u Usage) float64 {
	c := l.cfg
	switch kind {
	case kindSTT:
		if u.InputTokens+u.OutputTokens > 0 {
			return (float64(u.InputTokens)*c.PriceSTTInputPerM + float64(u.OutputTokens)*c.PriceSTTOutputPerM) / 1e6
		}
		return u.AudioSeconds / 60 * c.PriceSTTPerMinute
	case kindLLM:
		fresh := max(u.InputTokens-u.CachedInputTokens, 0)
		return (float64(fresh)*c.PriceLLMInputPerM + float64(u.CachedInputTokens)*c.PriceLLMCachedPerM + float64(u.OutputTokens)*c.PriceLLMOutputPerM) / 1e6
	case kindTTS:
		return u.AudioSeconds / 60 * c.PriceTTSPerMinute
	}
	return 0
}

// record keeps one call. A failure to write is logged, never shown: the
// user's answer is already paid for.
func (l ledger) record(ctx context.Context, log *slog.Logger, userID int64, kind string, u Usage, requestID string) {
	if l.db == nil { // unit tests run without a database
		return
	}
	model := u.Model
	if model == "" {
		model = "unknown"
	}
	_, err := l.db.ExecContext(context.WithoutCancel(ctx), `
		INSERT INTO public.assistant_usage_events
			(user_id, usage_kind, model, input_tokens, output_tokens, cached_input_tokens, audio_seconds, characters, estimated_cost_usd, request_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		userID, kind, model, u.InputTokens, u.OutputTokens, u.CachedInputTokens, u.AudioSeconds, u.Characters, l.cost(kind, u), requestID)
	if err != nil {
		log.Error("assistant usage not recorded", "kind", kind, "request_id", requestID, "error", err)
	}
}

// StartPurge deletes usage rows past the retention period and settled
// proposals after a week, once a day.
func StartPurge(ctx context.Context, db *sql.DB, retentionDays int, log *slog.Logger) {
	purge := func() {
		if _, err := db.ExecContext(ctx, `DELETE FROM public.assistant_usage_events WHERE created_at < now() - make_interval(days => $1)`, retentionDays); err != nil {
			log.Error("assistant usage purge failed", "error", err)
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM public.assistant_actions WHERE created_at < now() - interval '7 days'`); err != nil {
			log.Error("assistant actions purge failed", "error", err)
		}
	}
	purge()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			purge()
		}
	}
}
