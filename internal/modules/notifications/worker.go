package notifications

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/notifyqueue"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/redisutil"
	"github.com/redis/go-redis/v9"
)

const (
	notificationWorkerGroup   = "api-notification-workers"
	notificationMaxRetries    = 3
	notificationPendingIdle   = time.Minute
	notificationReadBatchSize = 10
)

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
	consumer := notificationConsumerName()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		processClaimedNotificationEvents(ctx, repo, rdb, sender, log, consumer)
		streams, err := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    notificationWorkerGroup,
			Consumer: consumer,
			Streams:  []string{redisutil.KeyNotifications, ">"},
			Count:    notificationReadBatchSize,
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
				handleNotificationMessage(ctx, repo, rdb, sender, msg, log)
			}
		}
	}
}

func processClaimedNotificationEvents(ctx context.Context, repo Repository, rdb redis.Cmdable, sender Sender, log *slog.Logger, consumer string) {
	messages, _, err := rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   redisutil.KeyNotifications,
		Group:    notificationWorkerGroup,
		Consumer: consumer,
		MinIdle:  notificationPendingIdle,
		Start:    "0-0",
		Count:    notificationReadBatchSize,
	}).Result()
	if errors.Is(err, redis.Nil) {
		return
	}
	if err != nil {
		log.Warn("notification queue pending claim failed", "error", err)
		return
	}
	for _, msg := range messages {
		handleNotificationMessage(ctx, repo, rdb, sender, msg, log)
	}
}

func handleNotificationMessage(ctx context.Context, repo Repository, rdb redis.Cmdable, sender Sender, msg redis.XMessage, log *slog.Logger) {
	if processNotificationEvent(ctx, repo, sender, msg, log) {
		ackNotification(ctx, rdb, msg.ID, log)
		return
	}
	attempts, err := rdb.Incr(ctx, redisutil.KeyNotificationRetry+msg.ID).Result()
	if err != nil {
		log.Warn("notification queue retry count failed", "message_id", msg.ID, "error", err)
		return
	}
	if attempts < notificationMaxRetries {
		log.Warn("notification queue event will retry", "message_id", msg.ID, "attempts", attempts)
		return
	}
	if err := rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: redisutil.KeyNotificationsDLQ,
		Values: deadLetterValues(msg, attempts),
	}).Err(); err != nil {
		log.Warn("notification queue dead-letter write failed", "message_id", msg.ID, "error", err)
		return
	}
	log.Warn("notification queue event moved to dead letter", "message_id", msg.ID, "attempts", attempts)
	ackNotification(ctx, rdb, msg.ID, log)
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

func deadLetterValues(msg redis.XMessage, attempts int64) map[string]any {
	values := make(map[string]any, len(msg.Values)+2)
	for k, v := range msg.Values {
		values[k] = v
	}
	values["original_message_id"] = msg.ID
	values["attempts"] = attempts
	return values
}

func ackNotification(ctx context.Context, rdb redis.Cmdable, messageID string, log *slog.Logger) {
	if err := rdb.XAck(ctx, redisutil.KeyNotifications, notificationWorkerGroup, messageID).Err(); err != nil {
		log.Warn("notification queue ack failed", "message_id", messageID, "error", err)
	}
	if err := rdb.XDel(ctx, redisutil.KeyNotifications, messageID).Err(); err != nil {
		log.Warn("notification queue delete failed", "message_id", messageID, "error", err)
	}
	if err := rdb.Del(ctx, redisutil.KeyNotificationRetry+messageID).Err(); err != nil {
		log.Warn("notification queue retry cleanup failed", "message_id", messageID, "error", err)
	}
}

func notificationConsumerName() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "api"
	}
	return host + "-" + strconv.Itoa(os.Getpid())
}

func isBusyGroup(err error) bool {
	return strings.Contains(strings.ToUpper(err.Error()), "BUSYGROUP")
}
