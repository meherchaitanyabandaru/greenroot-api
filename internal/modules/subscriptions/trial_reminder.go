package subscriptions

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/notifyqueue"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/messaging"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/sms"
	"github.com/redis/go-redis/v9"
)

// TrialReminderDays: an owner hears that the free trial is ending once,
// when this many days or fewer are left (its last day included).
const TrialReminderDays = 2

const trialEndingType = "TRIAL_ENDING"

// EventNotifier raises SMS/email events (messaging.Notifier).
type EventNotifier interface {
	NotifyAsync(ev messaging.Event)
}

// RemindEndingTrials asks each owner whose free trial ends within
// TrialReminderDays to choose a plan: in-app and by push, once per trial.
// Safe to run often and on several API instances. Returns how many were told.
// events (may be nil) also sends it by SMS and email: a trial that lapses
// unnoticed stops a nursery from taking orders.
func RemindEndingTrials(ctx context.Context, db *sql.DB, rdb *redis.Client, now time.Time, events EventNotifier) (int, error) {
	today := businessDay(now)
	list, err := endingTrials(ctx, db, today)
	if err != nil || len(list) == 0 {
		return 0, err
	}

	var from sql.NullFloat64 // the cheapest paid plan, for the message
	_ = db.QueryRowContext(ctx, `SELECT MIN(monthly_price) FROM public.subscription_plans WHERE is_active AND monthly_price > 0`).Scan(&from)

	sent := 0
	for _, d := range list {
		// The queued notification is written a moment later by the worker:
		// claim the trial first so a second run cannot send it again.
		if rdb != nil {
			ok, err := rdb.SetNX(ctx, "notified:trial_ending:"+strconv.FormatInt(d.subID, 10), 1, 7*24*time.Hour).Result()
			if err == nil && !ok {
				continue
			}
		}
		daysLeft := int(d.end.Sub(today).Hours() / 24)
		if events != nil {
			id := d.subID
			events.NotifyAsync(messaging.Event{Code: sms.TemplateTrialEnding, UserID: d.userID,
				Values: map[string]string{"days_left": strconv.Itoa(daysLeft)}, ReferenceType: "SUBSCRIPTION", ReferenceID: &id})
		}
		ev := notifyqueue.Event{
			UserID: d.userID, Type: trialEndingType, Title: "Your free trial is ending",
			Message: trialEndingMessage(daysLeft, d.end, from),
			Data:    notifyqueue.Link("subscription", d.subID),
		}
		var queue redis.Cmdable // a nil *redis.Client in the interface is not nil
		if rdb != nil {
			queue = rdb
		}
		if err := notifyqueue.Enqueue(ctx, queue, ev); err != nil {
			// No queue: keep the in-app notification (no push).
			if _, err := db.ExecContext(ctx, `
				INSERT INTO public.notifications (user_id, notification_type, title, message, channel, status, data, sent_at, created_at, updated_at)
				VALUES ($1, $2, $3, $4, 'IN_APP', 'SENT', $5::jsonb, NOW(), NOW(), NOW())`,
				ev.UserID, ev.Type, ev.Title, ev.Message, notifyqueue.DataJSON(ev.Data)); err != nil {
				slog.Error("trial reminder not saved", "subscription_id", d.subID, "error", err)
				continue
			}
		}
		sent++
	}
	return sent, nil
}

type endingTrial struct {
	subID, userID int64
	end           time.Time
}

// endingTrials: running trials ending within TrialReminderDays whose owner
// has not been told yet.
func endingTrials(ctx context.Context, db *sql.DB, today time.Time) ([]endingTrial, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT s.user_subscription_id, s.user_id, s.end_date
		FROM public.user_subscriptions s
		JOIN public.subscription_plans p ON p.plan_id = s.plan_id
		WHERE p.plan_code = 'TRIAL'
		  AND s.status IN ('ACTIVE', 'TRIAL')
		  AND s.end_date BETWEEN $1::date AND $1::date + $2::int
		  AND NOT EXISTS (
		      SELECT 1 FROM public.notifications n
		      WHERE n.user_id = s.user_id AND n.notification_type = $3
		        AND n.data->>'id' = s.user_subscription_id::text)`,
		today, TrialReminderDays, trialEndingType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []endingTrial
	for rows.Next() {
		var d endingTrial
		if err := rows.Scan(&d.subID, &d.userID, &d.end); err != nil {
			return nil, err
		}
		list = append(list, d)
	}
	return list, rows.Err()
}

func trialEndingMessage(daysLeft int, end time.Time, from sql.NullFloat64) string {
	when := "on " + end.Format("2 Jan")
	switch daysLeft {
	case 0:
		when = "today"
	case 1:
		when = "tomorrow"
	}
	plans := "Choose a plan"
	if from.Valid {
		plans = fmt.Sprintf("Choose a plan from ₹%.0f a month", from.Float64)
	}
	return fmt.Sprintf("Your free GreenRoot trial ends %s. %s to keep quoting, taking orders and delivering without a break.", when, plans)
}
