package admin

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

type Repository interface {
	Summary(context.Context) (Summary, error)
	ListUsers(context.Context, ListUsersRequest) ([]User, int64, error)
	UpdateUserStatus(ctx context.Context, userID int64, status string) error
	UpdateNurseryStatus(ctx context.Context, nurseryID int64, status string) error
	WorkspaceUserIDs(ctx context.Context, nurseryID int64) ([]int64, error)
}
type PostgresRepository struct{ db *sql.DB }

func NewRepository(db *sql.DB) Repository { return &PostgresRepository{db: db} }
func (r *PostgresRepository) Summary(ctx context.Context) (Summary, error) {
	var s Summary
	queries := []struct {
		name string
		dest *int64
	}{
		{"SELECT COUNT(*) FROM public.users WHERE deleted_at IS NULL", &s.Users},
		{"SELECT COUNT(*) FROM public.nurseries WHERE COALESCE(status,'') <> 'DELETED'", &s.Nurseries},
		{"SELECT COUNT(*) FROM public.nurseries WHERE UPPER(COALESCE(status,'')) = 'PENDING'", &s.PendingNurseries},
		{"SELECT COUNT(*) FROM public.nurseries WHERE UPPER(COALESCE(status,'')) IN ('ACTIVE','APPROVED')", &s.ApprovedNurseries},
		{"SELECT COUNT(*) FROM public.nurseries WHERE UPPER(COALESCE(status,'')) = 'SUSPENDED'", &s.SuspendedNurseries},
		{"SELECT COUNT(*) FROM public.plants WHERE is_active = TRUE", &s.Plants},
		{"SELECT COUNT(*) FROM public.nursery_inventory", &s.InventoryItems},
		{"SELECT COUNT(*) FROM public.plant_requests WHERE COALESCE(status,'') <> 'CANCELLED'", &s.PlantRequests},
		{"SELECT COUNT(*) FROM public.orders WHERE UPPER(COALESCE(order_status,'')) <> 'CANCELLED'", &s.Orders},
		{"SELECT COUNT(*) FROM public.orders WHERE UPPER(COALESCE(order_status,'')) IN ('CONFIRMED','LOADING','LOADING_COMPLETED','DISPATCH_CREATED','DISPATCHED','IN_TRANSIT')", &s.ActiveOrders},
		{"SELECT COUNT(*) FROM public.payments", &s.Payments},
		{"SELECT COUNT(*) FROM public.dispatches WHERE COALESCE(dispatch_status,'') <> 'CANCELLED'", &s.Dispatches},
		{"SELECT COUNT(*) FROM public.notifications", &s.Notifications},
		{"SELECT COUNT(*) FROM public.drivers WHERE UPPER(COALESCE(approval_status,'')) = 'APPROVED' AND COALESCE(status::text,'') = 'ACTIVE'", &s.ActiveDrivers},
	}
	for _, q := range queries {
		if err := r.db.QueryRowContext(ctx, q.name).Scan(q.dest); err != nil {
			return s, err
		}
	}
	_ = r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM public.payments WHERE payment_status::text='SUCCESS'`).Scan(&s.Revenue)
	return s, nil
}

func (r *PostgresRepository) ListUsers(ctx context.Context, input ListUsersRequest) ([]User, int64, error) {
	page, perPage := normalizePagination(input.Page, input.PerPage)
	offset := (page - 1) * perPage
	search := strings.TrimSpace(input.Search)
	status := strings.TrimSpace(input.Status)
	role := strings.TrimSpace(input.Role)

	const countQuery = `
		SELECT COUNT(*)
		FROM public.users u
		WHERE u.deleted_at IS NULL
			AND ($1 = '' OR u.user_code ILIKE '%' || $1 || '%' OR u.first_name ILIKE '%' || $1 || '%' OR COALESCE(u.last_name, '') ILIKE '%' || $1 || '%' OR u.mobile ILIKE '%' || $1 || '%' OR COALESCE(u.email, '') ILIKE '%' || $1 || '%')
			AND ($2 = '' OR UPPER(u.status) = UPPER($2))
			AND ($3 = '' OR EXISTS (
				SELECT 1
				FROM public.user_roles ur
				JOIN public.roles r ON r.role_id = ur.role_id
				WHERE ur.user_id = u.user_id AND UPPER(r.role_code) = UPPER($3)
			))
	`
	var total int64
	if err := r.db.QueryRowContext(ctx, countQuery, search, status, role).Scan(&total); err != nil {
		return nil, 0, err
	}

	const listQuery = `
		SELECT
			u.user_id,
			u.user_code,
			u.first_name,
			u.last_name,
			u.mobile,
			u.email,
			COALESCE(u.status, ''),
			COALESCE(u.mobile_verified, false),
			COALESCE(u.email_verified, false),
			u.last_login_at,
			u.created_at,
			COALESCE(STRING_AGG(DISTINCT r.role_code, ',' ORDER BY r.role_code), '') AS roles,
			COUNT(DISTINCT us.session_id) AS session_count
		FROM public.users u
		LEFT JOIN public.user_roles ur ON ur.user_id = u.user_id
		LEFT JOIN public.roles r ON r.role_id = ur.role_id AND COALESCE(r.is_active, true) = true
		LEFT JOIN public.user_sessions us ON us.user_id = u.user_id
		WHERE u.deleted_at IS NULL
			AND ($1 = '' OR u.user_code ILIKE '%' || $1 || '%' OR u.first_name ILIKE '%' || $1 || '%' OR COALESCE(u.last_name, '') ILIKE '%' || $1 || '%' OR u.mobile ILIKE '%' || $1 || '%' OR COALESCE(u.email, '') ILIKE '%' || $1 || '%')
			AND ($2 = '' OR UPPER(u.status) = UPPER($2))
			AND ($3 = '' OR EXISTS (
				SELECT 1
				FROM public.user_roles role_filter_ur
				JOIN public.roles role_filter ON role_filter.role_id = role_filter_ur.role_id
				WHERE role_filter_ur.user_id = u.user_id AND UPPER(role_filter.role_code) = UPPER($3)
			))
		GROUP BY u.user_id, u.user_code, u.first_name, u.last_name, u.mobile, u.email, u.status, u.mobile_verified, u.email_verified, u.last_login_at, u.created_at
		ORDER BY u.created_at DESC, u.user_id DESC
		LIMIT $4 OFFSET $5
	`
	rows, err := r.db.QueryContext(ctx, listQuery, search, status, role, perPage, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	users := make([]User, 0)
	for rows.Next() {
		user, err := scanAdminUser(rows)
		if err != nil {
			return nil, 0, err
		}
		users = append(users, user)
	}
	return users, total, rows.Err()
}

func scanAdminUser(row interface{ Scan(dest ...any) error }) (User, error) {
	var user User
	var lastName, email, rolesCSV sql.NullString
	var lastLoginAt, createdAt sql.NullTime
	if err := row.Scan(
		&user.ID,
		&user.UserCode,
		&user.FirstName,
		&lastName,
		&user.Mobile,
		&email,
		&user.Status,
		&user.MobileVerified,
		&user.EmailVerified,
		&lastLoginAt,
		&createdAt,
		&rolesCSV,
		&user.SessionCount,
	); err != nil {
		return User{}, err
	}
	user.LastName = nullableString(lastName)
	user.Email = nullableString(email)
	user.LastLoginAt = nullableTime(lastLoginAt)
	user.CreatedAt = nullableTime(createdAt)
	user.Roles = splitCSV(rolesCSV)
	return user, nil
}

func normalizePagination(page, perPage int) (int, int) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 10
	}
	if perPage > 100 {
		perPage = 100
	}
	return page, perPage
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullableTime(value sql.NullTime) *string {
	if !value.Valid {
		return nil
	}
	formatted := value.Time.Format(time.RFC3339)
	return &formatted
}

func (r *PostgresRepository) UpdateUserStatus(ctx context.Context, userID int64, status string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE public.users SET status = $2, updated_at = CURRENT_TIMESTAMP WHERE user_id = $1`,
		userID, status,
	)
	return err
}

func (r *PostgresRepository) UpdateNurseryStatus(ctx context.Context, nurseryID int64, status string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE public.nurseries SET status = $2, updated_at = CURRENT_TIMESTAMP WHERE nursery_id = $1`,
		nurseryID, status,
	)
	return err
}

func (r *PostgresRepository) WorkspaceUserIDs(ctx context.Context, nurseryID int64) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT user_id
		FROM (
			SELECT owner_user_id AS user_id
			FROM public.nurseries
			WHERE nursery_id = $1 AND owner_user_id IS NOT NULL
			UNION ALL
			SELECT user_id
			FROM public.nursery_users
			WHERE nursery_id = $1 AND COALESCE(is_active, true) = true
			UNION ALL
			SELECT driver_user_id AS user_id
			FROM public.nursery_drivers
			WHERE nursery_id = $1 AND connection_status IN ('APPROVED', 'CONNECTED', 'ACTIVE')
		) users
		WHERE user_id IS NOT NULL
	`, nurseryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	userIDs := make([]int64, 0)
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		userIDs = append(userIDs, userID)
	}
	return userIDs, rows.Err()
}

func splitCSV(value sql.NullString) []string {
	if !value.Valid || value.String == "" {
		return []string{}
	}
	parts := strings.Split(value.String, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
