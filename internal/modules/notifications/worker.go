package notifications

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/notifyqueue"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/redis/go-redis/v9"
)

const notificationWorkerGroup = "api-notification-workers"

func StartQueueWorker(ctx context.Context, db *sql.DB, rdb redis.Cmdable, sender Sender, log *slog.Logger) {
	if db == nil || rdb == nil {
		return
	}
	if sender == nil {
		sender = MockSender{}
	}
	if log == nil {
		log = slog.Default()
	}
	repo := NewRepository(db)
	if err := rdb.XGroupCreateMkStream(ctx, redisutil.KeyNotifications, notificationWorkerGroup, "0").Err(); err != nil && !isBusyGroup(err) {
		log.Warn("notification queue group setup failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		streams, err := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    notificationWorkerGroup,
			Consumer: "api",
			Streams:  []string{redisutil.KeyNotifications, ">"},
			Count:    10,
			Block:    5 * time.Second,
		}).Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			log.Warn("notification queue read failed", "error", err)
			time.Sleep(time.Second)
			continue
		}
		for _, stream := range streams {
			for _, msg := range stream.Messages {
				if processNotificationEvent(ctx, repo, sender, msg, log) {
					if err := rdb.XAck(ctx, redisutil.KeyNotifications, notificationWorkerGroup, msg.ID).Err(); err != nil {
						log.Warn("notification queue ack failed", "message_id", msg.ID, "error", err)
					}
					if err := rdb.XDel(ctx, redisutil.KeyNotifications, msg.ID).Err(); err != nil {
						log.Warn("notification queue delete failed", "message_id", msg.ID, "error", err)
					}
				}
			}
		}
	}
}

func processNotificationEvent(ctx context.Context, repo Repository, sender Sender, msg redis.XMessage, log *slog.Logger) bool {
	event, err := notifyqueue.Parse(msg.Values)
	if err != nil {
		log.Warn("invalid notification queue event", "message_id", msg.ID, "error", err)
		return true
	}
	title := event.Title
	message := event.Message
	dataJSON := "{}"
	if raw, ok := msg.Values["data"]; ok && raw != nil {
		dataJSON = fmt.Sprint(raw)
	}
	created, err := repo.Create(ctx, CreateNotificationInput{
		UserID:   &event.UserID,
		Type:     event.Type,
		Title:    &title,
		Message:  &message,
		Channel:  "IN_APP",
		Status:   "PENDING",
		DataJSON: dataJSON,
	})
	if err != nil {
		log.Warn("notification queue event create failed", "message_id", msg.ID, "type", event.Type, "user_id", event.UserID, "error", err)
		return false
	}
	if err := sender.Send(ctx, *created); err != nil {
		log.Warn("notification queue send failed", "notification_id", created.ID, "error", err)
	}
	return true
}

func isBusyGroup(err error) bool {
	return strings.Contains(strings.ToUpper(err.Error()), "BUSYGROUP")
}
