package notifyqueue

import (
	"context"
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
)

var ErrUnavailable = errors.New("notification queue unavailable")

type Event struct {
	UserID  int64
	Type    string
	Title   string
	Message string
	Data    map[string]any
}

func Enqueue(ctx context.Context, rdb redis.Cmdable, event Event) error {
	if rdb == nil {
		return ErrUnavailable
	}
	data := "{}"
	if len(event.Data) > 0 {
		raw, err := json.Marshal(event.Data)
		if err != nil {
			return err
		}
		data = string(raw)
	}
	return rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: redisutil.KeyNotifications,
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
