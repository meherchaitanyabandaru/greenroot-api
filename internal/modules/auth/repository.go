package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/publiccode"
)

type Repository interface {
	FindUserByMobile(ctx context.Context, mobile string) (*User, error)
	FindUserByID(ctx context.Context, userID int64) (*User, error)
	CreateUser(ctx context.Context, mobile string) (*User, error)
	UpdateLastLogin(ctx context.Context, userID int64, at time.Time) error
	GetUserRoles(ctx context.Context, userID int64) ([]string, error)
	AssignDefaultRole(ctx context.Context, userID int64) error
	CreateSession(ctx context.Context, input CreateSessionInput) (int64, error)
	StoreRefreshToken(ctx context.Context, sessionID int64, refreshToken string) error
	FindActiveSessionByToken(ctx context.Context, refreshToken string) (*Session, error)
	LogoutSession(ctx context.Context, sessionID int64) error
	CreateUserActivity(ctx context.Context, input CreateActivityInput) error
	CreateAuditLog(ctx context.Context, input CreateAuditInput) error
}

type CreateSessionInput struct {
	UserID     int64
	DeviceType string
	OSName     string
	AppVersion string
	IPAddress  string
	UserAgent  string
	LoginTime  time.Time
}

type CreateActivityInput struct {
	UserID    int64
	SessionID int64
	Type      string
	DataJSON  string
	At        time.Time
}

type CreateAuditInput struct {
	TableName string
	RecordID  int64
	Action    string
	ChangedBy int64
	SourceIP  string
	UserAgent string
	NewJSON   string
	At        time.Time
}

type PostgresRepository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) Repository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) FindUserByMobile(ctx context.Context, mobile string) (*User, error) {
	return r.scanUser(ctx, `WHERE mobile = $1 AND deleted_at IS NULL`, mobile)
}

func (r *PostgresRepository) FindUserByID(ctx context.Context, userID int64) (*User, error) {
	return r.scanUser(ctx, `WHERE user_id = $1 AND deleted_at IS NULL`, userID)
}

func (r *PostgresRepository) CreateUser(ctx context.Context, mobile string) (*User, error) {
	userCode, err := publiccode.Next(ctx, r.db, publiccode.Users, time.Now())
	if err != nil {
		return nil, err
	}

	const query = `
		INSERT INTO public.users (
			user_code,
			first_name,
			mobile,
			mobile_verified,
			email_verified,
			status,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, true, false, 'ACTIVE', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING user_id, user_code, first_name, last_name, mobile, email, profile_image_url,
			mobile_verified, email_verified, status::text, last_login_at, created_at, updated_at
	`

	user, err := scanUserRow(r.db.QueryRowContext(ctx, query, userCode, defaultUserFirstName, mobile))
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (r *PostgresRepository) UpdateLastLogin(ctx context.Context, userID int64, at time.Time) error {
	const query = `
		UPDATE public.users
		SET last_login_at = $2, updated_at = $2
		WHERE user_id = $1
	`
	_, err := r.db.ExecContext(ctx, query, userID, at)
	return err
}

func (r *PostgresRepository) GetUserRoles(ctx context.Context, userID int64) ([]string, error) {
	const query = `
		SELECT r.role_code
		FROM public.user_roles ur
		JOIN public.roles r ON r.role_id = ur.role_id
		WHERE ur.user_id = $1 AND COALESCE(r.is_active, true) = true
		ORDER BY r.role_code
	`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	roles := make([]string, 0)
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return roles, nil
}

func (r *PostgresRepository) AssignDefaultRole(ctx context.Context, userID int64) error {
	const query = `
		INSERT INTO public.user_roles (user_id, role_id, assigned_at)
		SELECT $1, role_id, CURRENT_TIMESTAMP
		FROM public.roles
		WHERE role_code = $2
		ON CONFLICT DO NOTHING
	`
	_, err := r.db.ExecContext(ctx, query, userID, defaultUserRole)
	return err
}

func (r *PostgresRepository) CreateSession(ctx context.Context, input CreateSessionInput) (int64, error) {
	const query = `
		INSERT INTO public.user_sessions (
			user_id,
			login_time,
			last_activity_at,
			session_status,
			device_type,
			os_name,
			app_version,
			ip_address,
			user_agent,
			created_at
		)
		VALUES ($1, $2, $2, 'ACTIVE', $3, $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), $2)
		RETURNING session_id
	`
	var sessionID int64
	err := r.db.QueryRowContext(
		ctx,
		query,
		input.UserID,
		input.LoginTime,
		nullableText(input.DeviceType),
		nullableText(input.OSName),
		input.AppVersion,
		input.IPAddress,
		input.UserAgent,
	).Scan(&sessionID)
	return sessionID, err
}

func (r *PostgresRepository) StoreRefreshToken(ctx context.Context, sessionID int64, refreshToken string) error {
	const query = `
		UPDATE public.user_sessions
		SET session_token = $2, last_activity_at = CURRENT_TIMESTAMP
		WHERE session_id = $1
	`
	_, err := r.db.ExecContext(ctx, query, sessionID, hashRefreshToken(refreshToken))
	return err
}

func (r *PostgresRepository) FindActiveSessionByToken(ctx context.Context, refreshToken string) (*Session, error) {
	const query = `
		SELECT session_id, user_id, session_status::text, login_time, last_activity_at
		FROM public.user_sessions
		WHERE session_token = $1 AND session_status = 'ACTIVE'
	`
	var session Session
	err := r.db.QueryRowContext(ctx, query, hashRefreshToken(refreshToken)).Scan(
		&session.ID,
		&session.UserID,
		&session.Status,
		&session.LoginTime,
		&session.LastActivityAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidRefreshToken
	}
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *PostgresRepository) LogoutSession(ctx context.Context, sessionID int64) error {
	const query = `
		UPDATE public.user_sessions
		SET session_status = 'LOGGED_OUT', session_token = NULL, last_activity_at = CURRENT_TIMESTAMP
		WHERE session_id = $1
	`
	_, err := r.db.ExecContext(ctx, query, sessionID)
	return err
}

func (r *PostgresRepository) CreateUserActivity(ctx context.Context, input CreateActivityInput) error {
	const query = `
		INSERT INTO public.user_activities (
			user_id,
			session_id,
			activity_type,
			entity_type,
			entity_id,
			activity_data,
			activity_timestamp
		)
		VALUES ($1, $2, $3, 'USER', $1, NULLIF($4, '')::jsonb, $5)
	`
	_, err := r.db.ExecContext(ctx, query, input.UserID, input.SessionID, input.Type, input.DataJSON, input.At)
	return err
}

func (r *PostgresRepository) CreateAuditLog(ctx context.Context, input CreateAuditInput) error {
	const query = `
		INSERT INTO public.audit_logs (
			table_name,
			record_id,
			action_type,
			old_data,
			new_data,
			changed_by,
			source_ip,
			user_agent,
			changed_at
		)
		VALUES ($1, $2, $3, NULL, NULLIF($4, '')::jsonb, $5, NULLIF($6, ''), NULLIF($7, ''), $8)
	`
	_, err := r.db.ExecContext(ctx, query, input.TableName, input.RecordID, input.Action, input.NewJSON, input.ChangedBy, input.SourceIP, input.UserAgent, input.At)
	return err
}

func (r *PostgresRepository) scanUser(ctx context.Context, where string, args ...any) (*User, error) {
	query := fmt.Sprintf(`
		SELECT user_id, user_code, first_name, last_name, mobile, email, profile_image_url,
			mobile_verified, email_verified, status::text, last_login_at, created_at, updated_at
		FROM public.users
		%s
	`, where)

	user, err := scanUserRow(r.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	roles, err := r.GetUserRoles(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	user.Roles = roles

	return user, nil
}

func scanUserRow(row interface {
	Scan(dest ...any) error
}) (*User, error) {
	var user User
	var lastName sql.NullString
	var email sql.NullString
	var profileImageURL sql.NullString
	var lastLoginAt sql.NullTime

	err := row.Scan(
		&user.ID,
		&user.UserCode,
		&user.FirstName,
		&lastName,
		&user.Mobile,
		&email,
		&profileImageURL,
		&user.MobileVerified,
		&user.EmailVerified,
		&user.Status,
		&lastLoginAt,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if lastName.Valid {
		user.LastName = &lastName.String
	}
	if email.Valid {
		user.Email = &email.String
	}
	if profileImageURL.Valid {
		user.ProfileImageURL = &profileImageURL.String
	}
	if lastLoginAt.Valid {
		user.LastLoginAt = &lastLoginAt.Time
	}

	return &user, nil
}

func parseUserID(userID string) (int64, error) {
	id, err := strconv.ParseInt(userID, 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrInvalidToken
	}
	return id, nil
}

func hashRefreshToken(refreshToken string) string {
	sum := sha256.Sum256([]byte(refreshToken))
	return hex.EncodeToString(sum[:])
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}
