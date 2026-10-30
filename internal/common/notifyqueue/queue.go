package notifyqueue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/redis/go-redis/v9"
)

const (
	EventOrderDispatched   = "ORDER_DISPATCHED"
	EventDispatchAccepted  = "DISPATCH_ACCEPTED"
	EventQuotationSent     = "QUOTATION_SENT"
	EventQuotationAccepted = "QUOTATION_ACCEPTED"
	EventOrderDelivered    = "ORDER_DELIVERED"
	EventDeliveryIssue     = "DELIVERY_ISSUE"
	// GreenRoot decided on a driver's lorry link or on the lorry itself.
	EventVehicleLinkDecided = "VEHICLE_LINK_DECIDED"
	EventVehicleDecided     = "VEHICLE_DECIDED"
)

var ErrUnavailable = errors.New("notification queue unavailable")

// Link is a notification's data pointing at the record it is about; the app
// opens that record when the notification is tapped. entity is "quotation",
// "order" or "dispatch".
func Link(entity string, id int64) map[string]any {
	return map[string]any{"entity": entity, "id": id}
}

type Event struct {
	UserID  int64
	Type    string
	Title   string
	Message string
	Data    map[string]any
}

// DataJSON is data as stored in notifications.data ("{}" when empty).
func DataJSON(data map[string]any) string {
	if len(data) == 0 {
		return "{}"
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func Enqueue(ctx context.Context, rdb redis.Cmdable, event Event) error {
	if rdb == nil {
		return ErrUnavailable
	}
	data := DataJSON(event.Data)
	return rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: redisutil.KeyNotifications,
		// The worker deletes what it delivers; the cap only bounds a backlog
		// while the worker is down.
		MaxLen: 100000,
		Approx: true,
		Values: map[string]any{
			"user_id": event.UserID,
			"type":    event.Type,
			"title":   event.Title,
			"message": event.Message,
			"data":    data,
		},
	}).Err()
}

func Parse(fields map[string]any) (Event, error) {
	userID, err := parseInt64(fields["user_id"])
	if err != nil {
		return Event{}, err
	}
	return Event{
		UserID:  userID,
		Type:    stringField(fields["type"]),
		Title:   stringField(fields["title"]),
		Message: stringField(fields["message"]),
		Data:    map[string]any{},
	}, nil
}

func stringField(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func parseInt64(value any) (int64, error) {
	switch v := value.(type) {
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	case string:
		return strconv.ParseInt(v, 10, 64)
	default:
		return 0, strconv.ErrSyntax
	}
}

// EnqueueOrStore queues event for the worker (push + in-app) and, when the
// queue is unavailable, stores it straight in notifications so the user still
// sees it in the app.
func EnqueueOrStore(ctx context.Context, rdb redis.Cmdable, db *sql.DB, event Event) error {
	if err := Enqueue(ctx, rdb, event); err == nil {
		return nil
	} else if db == nil {
		return err
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO public.notifications (user_id, notification_type, title, message, channel, status, data)
		 VALUES ($1, $2, $3, $4, 'IN_APP', 'PENDING', $5::jsonb)`,
		event.UserID, event.Type, event.Title, event.Message, DataJSON(event.Data))
	return err
}
