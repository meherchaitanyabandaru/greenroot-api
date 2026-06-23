package publiccode

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type QueryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Spec struct {
	Key       string
	Prefix    string
	Width     int
	DateBased bool
}

var (
	Users             = Spec{Key: "users", Prefix: "USR", Width: 6}
	Plants            = Spec{Key: "plants", Prefix: "PLT", Width: 6}
	Nurseries         = Spec{Key: "nurseries", Prefix: "NUR", Width: 6}
	Inventory         = Spec{Key: "nursery_inventory", Prefix: "INV", Width: 6}
	Requests          = Spec{Key: "plant_requests", Prefix: "REQ", Width: 4, DateBased: true}
	Orders            = Spec{Key: "orders", Prefix: "ORD", Width: 4, DateBased: true}
	Dispatches        = Spec{Key: "dispatches", Prefix: "DSP", Width: 4, DateBased: true}
	Payments          = Spec{Key: "payments", Prefix: "PAY", Width: 4, DateBased: true}
	Drivers           = Spec{Key: "drivers", Prefix: "DRV", Width: 6}
	Vehicles          = Spec{Key: "vehicles", Prefix: "VEH", Width: 6}
	Attachments       = Spec{Key: "attachments", Prefix: "ATT", Width: 6}
	Notifications     = Spec{Key: "notifications", Prefix: "NTF", Width: 6}
	UserSubscriptions = Spec{Key: "user_subscriptions", Prefix: "SUB", Width: 6}
)

func Format(spec Spec, seq int64, at time.Time) string {
	if spec.DateBased {
		return fmt.Sprintf("%s-%s-%0*d", spec.Prefix, at.Format("20060102"), spec.Width, seq)
	}
	return fmt.Sprintf("%s-%0*d", spec.Prefix, spec.Width, seq)
}

func Next(ctx context.Context, q QueryRower, spec Spec, at time.Time) (string, error) {
	if spec.Key == "" || spec.Prefix == "" || spec.Width <= 0 {
		return "", fmt.Errorf("invalid public code spec")
	}

	dateKey := ""
	if spec.DateBased {
		dateKey = at.Format("20060102")
	}

	var seq int64
	err := q.QueryRowContext(ctx, `
		INSERT INTO public.public_code_sequences (code_key, date_key, last_value)
		VALUES ($1, $2, 1)
		ON CONFLICT (code_key, date_key)
		DO UPDATE SET last_value = public.public_code_sequences.last_value + 1,
		              updated_at = CURRENT_TIMESTAMP
		RETURNING last_value
	`, spec.Key, dateKey).Scan(&seq)
	if err != nil {
		return "", err
	}

	return Format(spec, seq, at), nil
}
