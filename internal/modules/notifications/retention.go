package notifications

import (
	"context"
	"database/sql"
	"time"
)

// ReadRetention is how long a read notification is kept. Unread ones are
// kept until the person reads or deletes them.
const ReadRetention = 90 * 24 * time.Hour

const purgeBatch = 5000

// PurgeReadBefore deletes notifications read before cutoff (so also created
// before it), in batches so a large backlog never holds a long lock. Unread
// notifications (read_at IS NULL) are never touched. Safe to run on several
// API instances at once: a row is deleted only once.
func PurgeReadBefore(ctx context.Context, db *sql.DB, cutoff time.Time) (int64, error) {
	var total int64
	for {
		res, err := db.ExecContext(ctx, `
			DELETE FROM public.notifications
			WHERE notification_id IN (
				SELECT notification_id FROM public.notifications
				WHERE read_at IS NOT NULL AND read_at < $1
				LIMIT $2
			)`, cutoff, purgeBatch)
		if err != nil {
			return total, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += n
		if n < purgeBatch {
			return total, nil
		}
	}
}
