package apitest

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/modules/notifications"
)

// The daily cleanup deletes only notifications read more than 90 days ago.
// Unread ones, however old, and recently read ones stay.
func TestNotificationRetention_OnlyOldReadOnesGo(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres:///greenroot?host=/tmp"
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const user = 3 // fixture buyer
	add := func(title string, createdDaysAgo int, readDaysAgo *int, status string) int64 {
		var readAt any
		if readDaysAgo != nil {
			readAt = time.Now().AddDate(0, 0, -*readDaysAgo)
		}
		var id int64
		err := db.QueryRowContext(ctx, `
			INSERT INTO public.notifications (user_id, notification_type, title, channel, status, read_at, created_at, updated_at)
			VALUES ($1, 'SYSTEM', $2, 'IN_APP', $3, $4, $5, CURRENT_TIMESTAMP) RETURNING notification_id`,
			user, title, status, readAt, time.Now().AddDate(0, 0, -createdDaysAgo)).Scan(&id)
		if err != nil {
			t.Fatalf("insert %s: %v", title, err)
		}
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM public.notifications WHERE notification_id = $1`, id) })
		return id
	}
	days := func(n int) *int { return &n }

	oldRead := add("read 100 days ago", 120, days(100), "READ")
	recentRead := add("old, read 10 days ago", 200, days(10), "READ")
	oldUnread := add("unread for 200 days", 200, nil, "SENT")
	readStatusNoTime := add("status READ without read_at", 200, nil, "READ")
	justInside := add("read 89 days ago", 95, days(89), "READ")

	n, err := notifications.PurgeReadBefore(ctx, db, time.Now().Add(-notifications.ReadRetention))
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("deleted %d, want at least the one read 100 days ago", n)
	}
	exists := func(id int64) bool {
		var ok bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM public.notifications WHERE notification_id = $1)`, id).Scan(&ok); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if exists(oldRead) {
		t.Error("a notification read 100 days ago must be deleted")
	}
	for name, id := range map[string]int64{"recently read": recentRead, "unread": oldUnread, "READ status but unread": readStatusNoTime, "read 89 days ago": justInside} {
		if !exists(id) {
			t.Errorf("%s notification must be kept", name)
		}
	}
}
