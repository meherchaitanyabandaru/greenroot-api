package messaging

import (
	"context"
	"database/sql"
	"errors"
)

var ErrUnknownUser = errors.New("unknown user")

// UserRecipients reads addresses from users; unverified emails are dropped.
type UserRecipients struct{ db *sql.DB }

func NewUserRecipients(db *sql.DB) *UserRecipients { return &UserRecipients{db: db} }

func (r *UserRecipients) Recipient(ctx context.Context, userID int64) (Recipient, error) {
	var mobile, email sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT mobile, CASE WHEN COALESCE(is_email_verified, false) THEN email END
		FROM public.users WHERE user_id = $1 AND deleted_at IS NULL`, userID).Scan(&mobile, &email)
	if errors.Is(err, sql.ErrNoRows) {
		return Recipient{}, ErrUnknownUser
	}
	if err != nil {
		return Recipient{}, err
	}
	return Recipient{UserID: userID, Mobile: mobile.String, Email: email.String}, nil
}
