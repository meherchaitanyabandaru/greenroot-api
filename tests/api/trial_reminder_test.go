package apitest

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/subscriptions"
)

// Owners whose free trial ends within 2 days are told once to choose a plan;
// later trials, paid plans and cancelled trials are left alone.
func TestTrialReminder_OnceForTrialsEndingSoon(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres:///greenroot?host=/tmp"
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() }) // after the row cleanups below (they run first)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// A fixed "now" far from real data: 10:00 IST on 1 June 2031.
	now := time.Date(2031, 6, 1, 4, 30, 0, 0, time.UTC)
	var trialPlan, paidPlan int64
	if err := db.QueryRowContext(ctx, `SELECT plan_id FROM public.subscription_plans WHERE plan_code = 'TRIAL'`).Scan(&trialPlan); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT plan_id FROM public.subscription_plans WHERE plan_code = 'GROWTH'`).Scan(&paidPlan); err != nil {
		t.Fatal(err)
	}
	add := func(user int64, plan int64, status string, end string) int64 {
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO public.user_subscriptions (user_id, plan_id, start_date, end_date, status, is_auto_renew, created_at, updated_at)
			VALUES ($1, $2, $3::date - 29, $3::date, $4, false, NOW(), NOW()) RETURNING user_subscription_id`,
			user, plan, end, status).Scan(&id); err != nil {
			t.Fatalf("insert subscription: %v", err)
		}
		t.Cleanup(func() {
			_, _ = db.Exec(`DELETE FROM public.notifications WHERE notification_type = 'TRIAL_ENDING' AND data->>'id' = $1`, strconv.FormatInt(id, 10))
			_, _ = db.Exec(`DELETE FROM public.user_subscriptions WHERE user_subscription_id = $1`, id)
		})
		return id
	}
	endsToday := add(2, trialPlan, "ACTIVE", "2031-06-01")
	endsIn2 := add(3, trialPlan, "ACTIVE", "2031-06-03")
	endsIn3 := add(4, trialPlan, "ACTIVE", "2031-06-04")      // too early
	paidEndsSoon := add(5, paidPlan, "ACTIVE", "2031-06-02")  // not a trial
	cancelled := add(6, trialPlan, "CANCELLED", "2031-06-02") // not running

	n, err := subscriptions.RemindEndingTrials(ctx, db, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("told %d owners, want 2", n)
	}
	told := func(sub int64) (string, bool) {
		var msg string
		err := db.QueryRowContext(ctx, `SELECT message FROM public.notifications WHERE notification_type = 'TRIAL_ENDING' AND data->>'id' = $1 AND data->>'entity' = 'subscription'`, strconv.FormatInt(sub, 10)).Scan(&msg)
		return msg, err == nil
	}
	if msg, ok := told(endsToday); !ok || msg == "" {
		t.Error("the trial ending today must be reminded")
	}
	if _, ok := told(endsIn2); !ok {
		t.Error("the trial ending in 2 days must be reminded")
	}
	for name, id := range map[string]int64{"3 days left": endsIn3, "paid plan": paidEndsSoon, "cancelled trial": cancelled} {
		if _, ok := told(id); ok {
			t.Errorf("%s must not be reminded", name)
		}
	}
	if n, err := subscriptions.RemindEndingTrials(ctx, db, nil, now); err != nil || n != 0 {
		t.Fatalf("second run told %d (%v), want 0: once per trial", n, err)
	}
}
