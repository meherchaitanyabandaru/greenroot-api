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
}
type PostgresRepository struct{ db *sql.DB }

func NewRepository(db *sql.DB) Repository { return &PostgresRepository{db: db} }
func (r *PostgresRepository) Summary(ctx context.Context) (Summary, error) {
	var s Summary
	queries := []struct {
		name string
		dest *int64
	}{
		{"SELECT COUNT(*) FROM public.users", &s.Users}, {"SELECT COUNT(*) FROM public.nurseries", &s.Nurseries}, {"SELECT COUNT(*) FROM public.plants", &s.Plants}, {"SELECT COUNT(*) FROM public.nursery_inventory", &s.InventoryItems}, {"SELECT COUNT(*) FROM public.plant_requests", &s.PlantRequests}, {"SELECT COUNT(*) FROM public.orders", &s.Orders}, {"SELECT COUNT(*) FROM public.payments", &s.Payments}, {"SELECT COUNT(*) FROM public.dispatches", &s.Dispatches}, {"SELECT COUNT(*) FROM public.notifications", &s.Notifications},
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
