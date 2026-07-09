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
	GetWorkspaces(ctx context.Context, userID int64) ([]Workspace, error)
	GetOwnerDashboard(ctx context.Context, userID int64) (*OwnerDashboard, error)
	GetTokenContext(ctx context.Context, userID int64) (TokenContext, error)
}

// TokenContext is the user's runtime state embedded into the JWT at issue time.
// Fetched once per login/refresh; zero DB queries on each API request.
type TokenContext struct {
	UserStatus      string // ACTIVE | SUSPENDED | DELETED
	NurseryID       int64  // 0 = not nursery-affiliated
	NurseryStatus   string // ACTIVE | SUSPENDED | PENDING_APPROVAL
	SubTier         string // TRIAL | GROWTH | ENTERPRISE | ""
	SubExpiresEpoch int64  // Unix epoch of subscription end_date; 0 = no expiry
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

// GetTokenContext fetches the user's status, nursery affiliation, and active subscription
// in a single query. Called once at login and at each token refresh — never on every request.
func (r *PostgresRepository) GetTokenContext(ctx context.Context, userID int64) (TokenContext, error) {
	var tc TokenContext

	// User status
	if err := r.db.QueryRowContext(ctx,
		`SELECT COALESCE(status, 'ACTIVE') FROM public.users WHERE user_id = $1`,
		userID,
	).Scan(&tc.UserStatus); err != nil {
		tc.UserStatus = "ACTIVE"
	}

	// Nursery: owner first, then manager. Whichever matches first wins.
	// Also captures the nursery owner's user_id so we can look up their subscription.
	const nurseryQuery = `
		SELECT n.nursery_id, COALESCE(n.status::text, 'PENDING_APPROVAL'), n.owner_user_id
		FROM public.nurseries n
		WHERE n.owner_user_id = $1
		  AND COALESCE(n.status::text, '') NOT IN ('DELETED')
		UNION ALL
		SELECT n.nursery_id, COALESCE(n.status::text, 'ACTIVE'), n.owner_user_id
		FROM public.nursery_users nu
		JOIN public.nurseries n ON n.nursery_id = nu.nursery_id
		WHERE nu.user_id = $1
		  AND nu.status = 'ACTIVE'
		  AND COALESCE(n.status::text, '') NOT IN ('DELETED')
		LIMIT 1
	`
	var ownerUserID int64
	if err := r.db.QueryRowContext(ctx, nurseryQuery, userID).Scan(
		&tc.NurseryID, &tc.NurseryStatus, &ownerUserID,
	); err == nil && tc.NurseryID > 0 {
		// Subscription belongs to the nursery owner (covers both owner and manager tokens).
		const subQuery = `
			SELECT sp.plan_code, us.end_date
			FROM public.user_subscriptions us
			JOIN public.subscription_plans sp ON sp.plan_id = us.plan_id
			WHERE us.user_id = $1
			  AND us.subscription_status IN ('ACTIVE', 'TRIAL', 'EXPIRED')
			  AND (us.end_date IS NULL OR us.end_date >= CURRENT_DATE - INTERVAL '7 days')
			ORDER BY us.end_date DESC NULLS FIRST
			LIMIT 1
		`
		var endDate sql.NullTime
		if err2 := r.db.QueryRowContext(ctx, subQuery, ownerUserID).Scan(&tc.SubTier, &endDate); err2 == nil {
			if endDate.Valid {
				// Normalise to midnight UTC of the end date so the math is stable.
				d := endDate.Time
				tc.SubExpiresEpoch = time.Date(d.Year(), d.Month(), d.Day(), 23, 59, 59, 0, time.UTC).Unix()
			}
		}
	}

	return tc, nil
}

// GetWorkspaces returns all workspaces the user can operate in.
// Always includes PERSONAL. Also includes OWNED_NURSERY (via nurseries.owner_user_id),
// MANAGER_NURSERY (via nursery_users.status='ACTIVE'), and DRIVER (via drivers.approval_status).
func (r *PostgresRepository) GetWorkspaces(ctx context.Context, userID int64) ([]Workspace, error) {
	workspaces := []Workspace{
		{Type: "PERSONAL", Role: "CUSTOMER"},
	}

	// Nursery owned by this user — V1 tracks ownership via nurseries.owner_user_id
	const ownedQuery = `
		SELECT nursery_id, nursery_name, COALESCE(status::text, 'PENDING') AS nursery_status
		FROM public.nurseries
		WHERE owner_user_id = $1
		  AND status NOT IN ('DELETED', 'SUSPENDED')
		ORDER BY nursery_id
		LIMIT 1
	`
	var ownedNurseryID int64
	var ownedNurseryName, ownedNurseryStatus string
	if err := r.db.QueryRowContext(ctx, ownedQuery, userID).Scan(&ownedNurseryID, &ownedNurseryName, &ownedNurseryStatus); err == nil {
		workspaces = append(workspaces, Workspace{
			Type:          "OWNED_NURSERY",
			Role:          "OWNER",
			NurseryID:     &ownedNurseryID,
			NurseryName:   &ownedNurseryName,
			NurseryStatus: &ownedNurseryStatus,
		})
	}

	// Nurseries where this user is an active manager — V1 tracks via nursery_users.status
	const managerQuery = `
		SELECT n.nursery_id, n.nursery_name, nu.role
		FROM public.nursery_users nu
		JOIN public.nurseries n ON n.nursery_id = nu.nursery_id
		WHERE nu.user_id = $1
		  AND nu.status = 'ACTIVE'
		  AND n.status NOT IN ('DELETED', 'SUSPENDED')
		ORDER BY n.nursery_id
	`
	rows, err := r.db.QueryContext(ctx, managerQuery, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var nid int64
		var nname, role string
		if err := rows.Scan(&nid, &nname, &role); err != nil {
			return nil, err
		}
		workspaces = append(workspaces, Workspace{
			Type:        "MANAGER_NURSERY",
			Role:        role,
			NurseryID:   &nid,
			NurseryName: &nname,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Driver workspace
	const driverQuery = `
		SELECT 1 FROM public.drivers
		WHERE user_id = $1 AND approval_status = 'APPROVED'
		LIMIT 1
	`
	var exists int
	if err := r.db.QueryRowContext(ctx, driverQuery, userID).Scan(&exists); err == nil {
		workspaces = append(workspaces, Workspace{Type: "DRIVER", Role: "DRIVER"})
	}

	return workspaces, nil
}

func (r *PostgresRepository) GetOwnerDashboard(ctx context.Context, userID int64) (*OwnerDashboard, error) {
	d := &OwnerDashboard{}

	// Resolve owned nursery
	var nurseryID int64
	var nurseryName string
	err := r.db.QueryRowContext(ctx, `
		SELECT nursery_id, nursery_name FROM public.nurseries
		WHERE owner_user_id = $1 AND COALESCE(status::text,'') <> 'DELETED'
		LIMIT 1
	`, userID).Scan(&nurseryID, &nurseryName)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if err == nil {
		d.NurseryID = &nurseryID
		d.NurseryName = &nurseryName
	}

	// Sell orders (nursery is seller)
	if nurseryID > 0 {
		rows, _ := r.db.QueryContext(ctx, `
			SELECT COALESCE(order_status,'PENDING'), COUNT(*) FROM public.orders
			WHERE nursery_id = $1 GROUP BY order_status
		`, nurseryID)
		if rows != nil {
			for rows.Next() {
				var status string
				var cnt int
				if err := rows.Scan(&status, &cnt); err == nil {
					d.SellOrders.Total += cnt
					switch status {
					case "PENDING":
						d.SellOrders.Pending += cnt
					case "CONFIRMED":
						d.SellOrders.Confirmed += cnt
					case "DELIVERED":
						d.SellOrders.Delivered += cnt
					case "CANCELLED":
						d.SellOrders.Cancelled += cnt
					}
				}
			}
			rows.Close()
		}

		// Buy orders (nursery is buyer)
		rows2, _ := r.db.QueryContext(ctx, `
			SELECT COALESCE(order_status,'PENDING'), COUNT(*) FROM public.orders
			WHERE buyer_nursery_id = $1 GROUP BY order_status
		`, nurseryID)
		if rows2 != nil {
			for rows2.Next() {
				var status string
				var cnt int
				if err := rows2.Scan(&status, &cnt); err == nil {
					d.BuyOrders.Total += cnt
					switch status {
					case "PENDING":
						d.BuyOrders.Pending += cnt
					case "CONFIRMED":
						d.BuyOrders.Confirmed += cnt
					case "DELIVERED":
						d.BuyOrders.Delivered += cnt
					case "CANCELLED":
						d.BuyOrders.Cancelled += cnt
					}
				}
			}
			rows2.Close()
		}

		// Sell quotations
		rows3, _ := r.db.QueryContext(ctx, `
			SELECT COALESCE(status,'PENDING'), COUNT(*) FROM public.quotations
			WHERE nursery_id = $1 GROUP BY status
		`, nurseryID)
		if rows3 != nil {
			for rows3.Next() {
				var status string
				var cnt int
				if err := rows3.Scan(&status, &cnt); err == nil {
					d.SellQuotes.Total += cnt
					switch status {
					case "PENDING", "SENT":
						d.SellQuotes.Pending += cnt
					case "APPROVED":
						d.SellQuotes.Approved += cnt
					case "REJECTED":
						d.SellQuotes.Rejected += cnt
					}
				}
			}
			rows3.Close()
		}

		// Buy quotations
		rows4, _ := r.db.QueryContext(ctx, `
			SELECT COALESCE(status,'PENDING'), COUNT(*) FROM public.quotations
			WHERE buyer_nursery_id = $1 GROUP BY status
		`, nurseryID)
		if rows4 != nil {
			for rows4.Next() {
				var status string
				var cnt int
				if err := rows4.Scan(&status, &cnt); err == nil {
					d.BuyQuotes.Total += cnt
					switch status {
					case "PENDING", "SENT":
						d.BuyQuotes.Pending += cnt
					case "APPROVED":
						d.BuyQuotes.Approved += cnt
					case "REJECTED":
						d.BuyQuotes.Rejected += cnt
					}
				}
			}
			rows4.Close()
		}

		// Inventory
		_ = r.db.QueryRowContext(ctx, `
			SELECT COUNT(*), COALESCE(SUM(CASE WHEN COALESCE(status::text,'') = 'AVAILABLE' THEN 1 ELSE 0 END), 0)
			FROM public.nursery_inventory WHERE nursery_id = $1
		`, nurseryID).Scan(&d.Inventory.TotalItems, &d.Inventory.Available)

		// Connections
		_ = r.db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM public.nursery_users
			WHERE nursery_id = $1 AND role = 'MANAGER' AND status = 'ACTIVE'
		`, nurseryID).Scan(&d.Connections.Managers)

		_ = r.db.QueryRowContext(ctx, `
			SELECT COUNT(DISTINCT driver_id) FROM public.dispatches
			WHERE nursery_id = $1 AND driver_id IS NOT NULL
		`, nurseryID).Scan(&d.Connections.Drivers)

		_ = r.db.QueryRowContext(ctx, `
			SELECT COUNT(DISTINCT buyer_user_id) FROM public.orders
			WHERE nursery_id = $1 AND buyer_user_id IS NOT NULL
		`, nurseryID).Scan(&d.Connections.Customers)
	}

	return d, nil
}
