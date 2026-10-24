package notifications

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/meherchaitanyabandaru/greenroot-api/platform/fcm"
)

// Pusher is the FCM client (faked in tests).
type Pusher interface {
	Send(ctx context.Context, m fcm.Message) error
}

// PushStore is what PushSender needs from the database (NewPushStore).
type PushStore interface {
	ListDevices(ctx context.Context, userID int64) ([]Device, error)
	DeactivateDeviceToken(ctx context.Context, token string) error
	MarkDelivery(ctx context.Context, notificationID int64, status string) error
}

// Delivery outcomes recorded on the notification (it stays in the in-app
// inbox either way).
const (
	DeliverySent     = "SENT"
	DeliveryNoDevice = "NO_DEVICE"
	DeliveryFailed   = "FAILED"
)

// PushSender delivers a notification to every active device of its user. A
// token FCM reports as dead is retired so it is never tried again.
type PushSender struct {
	push  Pusher
	store PushStore
	log   *slog.Logger
}

func NewPushSender(push Pusher, store PushStore, log *slog.Logger) *PushSender {
	if log == nil {
		log = slog.Default()
	}
	return &PushSender{push: push, store: store, log: log}
}

func (p *PushSender) Send(ctx context.Context, n Notification) error {
	if n.UserID == nil {
		return nil // broadcast/admin rows without a recipient: inbox only
	}
	devices, err := p.store.ListDevices(ctx, *n.UserID)
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		return p.store.MarkDelivery(ctx, n.ID, DeliveryNoDevice)
	}
	msg := fcm.Message{
		Title: deref(n.Title), Body: deref(n.Message),
		Data: map[string]string{"notification_id": strconv.FormatInt(n.ID, 10), "type": n.Type},
	}
	sent, failed := 0, 0
	var lastErr error
	for _, d := range devices {
		msg.Token = d.FCMToken
		switch err := p.push.Send(ctx, msg); {
		case err == nil:
			sent++
		case errors.Is(err, fcm.ErrUnregistered):
			if derr := p.store.DeactivateDeviceToken(ctx, d.FCMToken); derr != nil {
				p.log.Warn("push: could not retire dead device token", "device_id", d.ID, "error", derr)
			}
		default:
			failed++
			lastErr = err
		}
	}
	status := DeliverySent
	switch {
	case sent == 0 && failed > 0:
		status = DeliveryFailed
	case sent == 0:
		status = DeliveryNoDevice // every token was dead
	}
	if err := p.store.MarkDelivery(ctx, n.ID, status); err != nil {
		return err
	}
	if status == DeliveryFailed {
		return fmt.Errorf("push: %d device(s) failed: %w", failed, lastErr)
	}
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// NewPushStore is the Postgres PushStore.
func NewPushStore(db *sql.DB) PushStore { return &PostgresRepository{db: db} }
