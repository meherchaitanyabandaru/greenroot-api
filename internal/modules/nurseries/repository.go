package nurseries

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/publiccode"
)

var ErrNotFound = errors.New("not found")

type Repository interface {
	List(ctx context.Context, input ListNurseriesRequest) ([]Nursery, int64, error)
	FindByID(ctx context.Context, nurseryID int64) (*Nursery, error)
	Create(ctx context.Context, actorID int64, input CreateNurseryRequest) (*Nursery, error)
	Update(ctx context.Context, actorID int64, nurseryID int64, input UpdateNurseryRequest) (*Nursery, error)
	Delete(ctx context.Context, actorID int64, nurseryID int64) error
	ListAddresses(ctx context.Context, nurseryID int64) ([]Address, error)
	CreateAddress(ctx context.Context, nurseryID int64, input AddressRequest) (*Address, error)
	UpdateAddress(ctx context.Context, addressID int64, input AddressRequest) (*Address, error)
	DeleteAddress(ctx context.Context, addressID int64) error
	ListUsers(ctx context.Context, nurseryID int64) ([]UserLink, error)
	AddUser(ctx context.Context, nurseryID int64, input AddUserRequest) (*UserLink, error)
	RemoveUser(ctx context.Context, nurseryID int64, userID int64) error
	IsNurseryMember(ctx context.Context, nurseryID int64, userID int64) (bool, error)
	ListByUserID(ctx context.Context, userID int64) ([]Nursery, error)
	CreateAuditLog(ctx context.Context, input CreateAuditInput) error
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

func (r *PostgresRepository) List(ctx context.Context, input ListNurseriesRequest) ([]Nursery, int64, error) {
	where, args := buildNurseryWhere(input)
	countQuery := `SELECT COUNT(DISTINCT n.nursery_id) FROM public.nurseries n ` + where
	var total int64
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (input.Page - 1) * input.PerPage
	args = append(args, input.PerPage, offset)
	query := fmt.Sprintf(`
		SELECT DISTINCT n.nursery_id, n.nursery_code, n.nursery_name, n.gst_number, n.mobile,
			n.email, n.website, n.description, COALESCE(n.status::text, ''), n.created_at,
			n.updated_at, n.created_by, n.updated_by
		FROM public.nurseries n
		%s
		ORDER BY n.nursery_id DESC
		LIMIT $%d OFFSET $%d
	`, where, len(args)-1, len(args))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	nurseries := make([]Nursery, 0)
	for rows.Next() {
		nursery, err := scanNurseryRows(rows)
		if err != nil {
			return nil, 0, err
		}
		nurseries = append(nurseries, nursery)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	for index := range nurseries {
		nurseries[index].Addresses, _ = r.ListAddresses(ctx, nurseries[index].ID)
	}
	return nurseries, total, nil
}

func (r *PostgresRepository) FindByID(ctx context.Context, nurseryID int64) (*Nursery, error) {
	nursery, err := r.scanNursery(ctx, `WHERE n.nursery_id = $1 AND COALESCE(n.status::text, '') <> 'DELETED'`, nurseryID)
	if err != nil {
		return nil, err
	}
	nursery.Addresses, _ = r.ListAddresses(ctx, nursery.ID)
	nursery.Users, _ = r.ListUsers(ctx, nursery.ID)
	return nursery, nil
}

func (r *PostgresRepository) Create(ctx context.Context, actorID int64, input CreateNurseryRequest) (*Nursery, error) {
	nurseryCode := strings.TrimSpace(stringOrEmpty(input.Code))
	if nurseryCode == "" {
		var err error
		nurseryCode, err = publiccode.Next(ctx, r.db, publiccode.Nurseries, time.Now())
		if err != nil {
			return nil, err
		}
	}

	const query = `
		INSERT INTO public.nurseries (
			nursery_code, nursery_name, gst_number, mobile, email, website, description,
			status, created_at, updated_at, created_by, updated_by
		)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''),
			NULLIF($6, ''), NULLIF($7, ''), $8, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, $9, $9)
		RETURNING nursery_id
	`
	var nurseryID int64
	if err := r.db.QueryRowContext(
		ctx,
		query,
		nurseryCode,
		input.Name,
		stringOrEmpty(input.GSTNumber),
		stringOrEmpty(input.Mobile),
		stringOrEmpty(input.Email),
		stringOrEmpty(input.Website),
		stringOrEmpty(input.Description),
		statusOrActive(input.Status),
		actorID,
	).Scan(&nurseryID); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, nurseryID)
}

func (r *PostgresRepository) Update(ctx context.Context, actorID int64, nurseryID int64, input UpdateNurseryRequest) (*Nursery, error) {
	const query = `
		UPDATE public.nurseries
		SET nursery_code = COALESCE(NULLIF($2, ''), nursery_code),
			nursery_name = $3,
			gst_number = NULLIF($4, ''),
			mobile = NULLIF($5, ''),
			email = NULLIF($6, ''),
			website = NULLIF($7, ''),
			description = NULLIF($8, ''),
			status = $9,
			updated_at = CURRENT_TIMESTAMP,
			updated_by = $10
		WHERE nursery_id = $1 AND COALESCE(status::text, '') <> 'DELETED'
	`
	result, err := r.db.ExecContext(
		ctx,
		query,
		nurseryID,
		stringOrEmpty(input.Code),
		input.Name,
		stringOrEmpty(input.GSTNumber),
		stringOrEmpty(input.Mobile),
		stringOrEmpty(input.Email),
		stringOrEmpty(input.Website),
		stringOrEmpty(input.Description),
		statusOrActive(input.Status),
		actorID,
	)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrNotFound
	}
	return r.FindByID(ctx, nurseryID)
}

func (r *PostgresRepository) Delete(ctx context.Context, actorID int64, nurseryID int64) error {
	result, err := r.db.ExecContext(ctx, `UPDATE public.nurseries SET status = 'DELETED', updated_at = CURRENT_TIMESTAMP, updated_by = $2 WHERE nursery_id = $1 AND COALESCE(status::text, '') <> 'DELETED'`, nurseryID, actorID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) ListAddresses(ctx context.Context, nurseryID int64) ([]Address, error) {
	const query = `
		SELECT nursery_address_id, nursery_id, address_type::text, address_line1, address_line2,
			city, state, country, postal_code, latitude, longitude, COALESCE(is_primary, false),
			created_at, updated_at
		FROM public.nursery_addresses
		WHERE nursery_id = $1
		ORDER BY is_primary DESC, nursery_address_id DESC
	`
	rows, err := r.db.QueryContext(ctx, query, nurseryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	addresses := make([]Address, 0)
	for rows.Next() {
		address, err := scanAddressRows(rows)
		if err != nil {
			return nil, err
		}
		addresses = append(addresses, address)
	}
	return addresses, rows.Err()
}

func (r *PostgresRepository) CreateAddress(ctx context.Context, nurseryID int64, input AddressRequest) (*Address, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if input.IsPrimary {
		if _, err := tx.ExecContext(ctx, `UPDATE public.nursery_addresses SET is_primary = false WHERE nursery_id = $1`, nurseryID); err != nil {
			return nil, err
		}
	}
	const query = `
		INSERT INTO public.nursery_addresses (
			nursery_id, address_type, address_line1, address_line2, city, state, country,
			postal_code, latitude, longitude, is_primary, created_at, updated_at
		)
		VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''),
			NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), $9, $10, $11, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING nursery_address_id, nursery_id, address_type::text, address_line1, address_line2,
			city, state, country, postal_code, latitude, longitude, COALESCE(is_primary, false),
			created_at, updated_at
	`
	address, err := scanAddressRow(tx.QueryRowContext(
		ctx,
		query,
		nurseryID,
		stringOrEmpty(input.AddressType),
		stringOrEmpty(input.AddressLine1),
		stringOrEmpty(input.AddressLine2),
		stringOrEmpty(input.City),
		stringOrEmpty(input.State),
		stringOrEmpty(input.Country),
		stringOrEmpty(input.PostalCode),
		floatOrNil(input.Latitude),
		floatOrNil(input.Longitude),
		input.IsPrimary,
	))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return address, nil
}

func (r *PostgresRepository) UpdateAddress(ctx context.Context, addressID int64, input AddressRequest) (*Address, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var nurseryID int64
	if err := tx.QueryRowContext(ctx, `SELECT nursery_id FROM public.nursery_addresses WHERE nursery_address_id = $1`, addressID).Scan(&nurseryID); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if input.IsPrimary {
		if _, err := tx.ExecContext(ctx, `UPDATE public.nursery_addresses SET is_primary = false WHERE nursery_id = $1 AND nursery_address_id <> $2`, nurseryID, addressID); err != nil {
			return nil, err
		}
	}
	const query = `
		UPDATE public.nursery_addresses
		SET address_type = NULLIF($2, ''),
			address_line1 = NULLIF($3, ''),
			address_line2 = NULLIF($4, ''),
			city = NULLIF($5, ''),
			state = NULLIF($6, ''),
			country = NULLIF($7, ''),
			postal_code = NULLIF($8, ''),
			latitude = $9,
			longitude = $10,
			is_primary = $11,
			updated_at = CURRENT_TIMESTAMP
		WHERE nursery_address_id = $1
		RETURNING nursery_address_id, nursery_id, address_type::text, address_line1, address_line2,
			city, state, country, postal_code, latitude, longitude, COALESCE(is_primary, false),
			created_at, updated_at
	`
	address, err := scanAddressRow(tx.QueryRowContext(
		ctx,
		query,
		addressID,
		stringOrEmpty(input.AddressType),
		stringOrEmpty(input.AddressLine1),
		stringOrEmpty(input.AddressLine2),
		stringOrEmpty(input.City),
		stringOrEmpty(input.State),
		stringOrEmpty(input.Country),
		stringOrEmpty(input.PostalCode),
		floatOrNil(input.Latitude),
		floatOrNil(input.Longitude),
		input.IsPrimary,
	))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return address, nil
}

func (r *PostgresRepository) DeleteAddress(ctx context.Context, addressID int64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM public.nursery_addresses WHERE nursery_address_id = $1`, addressID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) ListUsers(ctx context.Context, nurseryID int64) ([]UserLink, error) {
	const query = `
		SELECT nu.nursery_user_id, nu.nursery_id, nu.user_id, u.first_name, u.mobile, u.email,
			nr.nursery_role_id, nr.role_code, nr.role_name, nu.joined_at, COALESCE(nu.is_active, true)
		FROM public.nursery_users nu
		JOIN public.users u ON u.user_id = nu.user_id
		JOIN public.nursery_roles nr ON nr.nursery_role_id = nu.nursery_role_id
		WHERE nu.nursery_id = $1 AND COALESCE(nu.is_active, true) = true
		ORDER BY nr.nursery_role_id, u.first_name
	`
	rows, err := r.db.QueryContext(ctx, query, nurseryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]UserLink, 0)
	for rows.Next() {
		user, err := scanUserLinkRows(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (r *PostgresRepository) AddUser(ctx context.Context, nurseryID int64, input AddUserRequest) (*UserLink, error) {
	roleID, err := r.roleID(ctx, input)
	if err != nil {
		return nil, err
	}
	const query = `
		INSERT INTO public.nursery_users (nursery_id, user_id, nursery_role_id, joined_at, is_active)
		VALUES ($1, $2, $3, CURRENT_TIMESTAMP, true)
		ON CONFLICT (nursery_id, user_id, nursery_role_id)
		DO UPDATE SET is_active = true
		RETURNING nursery_user_id
	`
	var nurseryUserID int64
	if err := r.db.QueryRowContext(ctx, query, nurseryID, input.UserID, roleID).Scan(&nurseryUserID); err != nil {
		return nil, err
	}
	return r.findUserLink(ctx, nurseryUserID)
}

func (r *PostgresRepository) RemoveUser(ctx context.Context, nurseryID int64, userID int64) error {
	result, err := r.db.ExecContext(ctx, `UPDATE public.nursery_users SET is_active = false WHERE nursery_id = $1 AND user_id = $2 AND COALESCE(is_active, true) = true`, nurseryID, userID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) IsNurseryMember(ctx context.Context, nurseryID int64, userID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM public.nursery_users WHERE nursery_id = $1 AND user_id = $2 AND COALESCE(is_active, true) = true)`, nurseryID, userID).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) ListByUserID(ctx context.Context, userID int64) ([]Nursery, error) {
	const query = `
		SELECT DISTINCT n.nursery_id, n.nursery_code, n.nursery_name, n.gst_number, n.mobile,
			n.email, n.website, n.description, COALESCE(n.status::text, ''), n.created_at,
			n.updated_at, n.created_by, n.updated_by
		FROM public.nurseries n
		JOIN public.nursery_users nu ON nu.nursery_id = n.nursery_id
		WHERE nu.user_id = $1
		  AND COALESCE(nu.is_active, true) = true
		  AND COALESCE(n.status::text, '') <> 'DELETED'
		ORDER BY n.nursery_id
	`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	nurseries := make([]Nursery, 0)
	for rows.Next() {
		nursery, err := scanNurseryRows(rows)
		if err != nil {
			return nil, err
		}
		nurseries = append(nurseries, nursery)
	}
	return nurseries, rows.Err()
}

func (r *PostgresRepository) CreateAuditLog(ctx context.Context, input CreateAuditInput) error {
	const query = `
		INSERT INTO public.audit_logs (
			table_name, record_id, action_type, old_data, new_data, changed_by, source_ip, user_agent, changed_at
		)
		VALUES ($1, $2, $3, NULL, NULLIF($4, '')::jsonb, $5, NULLIF($6, ''), NULLIF($7, ''), $8)
	`
	_, err := r.db.ExecContext(ctx, query, input.TableName, input.RecordID, input.Action, input.NewJSON, input.ChangedBy, input.SourceIP, input.UserAgent, input.At)
	return err
}

func buildNurseryWhere(input ListNurseriesRequest) (string, []any) {
	clauses := []string{"COALESCE(n.status::text, '') <> 'DELETED'"}
	args := make([]any, 0)
	if input.Search != "" {
		args = append(args, input.Search)
		index := len(args)
		clauses = append(clauses, fmt.Sprintf(`(n.nursery_name ILIKE '%%' || $%d || '%%' OR n.nursery_code ILIKE '%%' || $%d || '%%' OR n.description ILIKE '%%' || $%d || '%%')`, index, index, index))
	}
	if input.City != "" {
		args = append(args, input.City)
		clauses = append(clauses, fmt.Sprintf(`EXISTS (SELECT 1 FROM public.nursery_addresses na WHERE na.nursery_id = n.nursery_id AND na.city ILIKE $%d)`, len(args)))
	}
	if input.State != "" {
		args = append(args, input.State)
		clauses = append(clauses, fmt.Sprintf(`EXISTS (SELECT 1 FROM public.nursery_addresses na WHERE na.nursery_id = n.nursery_id AND na.state ILIKE $%d)`, len(args)))
	}
	status := input.NurseryStatus
	if status == "" {
		status = input.VerificationStatus
	}
	if status != "" {
		args = append(args, status)
		clauses = append(clauses, fmt.Sprintf(`n.status::text = $%d`, len(args)))
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func (r *PostgresRepository) scanNursery(ctx context.Context, where string, args ...any) (*Nursery, error) {
	query := `
		SELECT n.nursery_id, n.nursery_code, n.nursery_name, n.gst_number, n.mobile,
			n.email, n.website, n.description, COALESCE(n.status::text, ''), n.created_at,
			n.updated_at, n.created_by, n.updated_by
		FROM public.nurseries n
		` + where
	nursery, err := scanNurseryRow(r.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return nursery, err
}

func (r *PostgresRepository) roleID(ctx context.Context, input AddUserRequest) (int16, error) {
	if input.RoleID > 0 {
		return input.RoleID, nil
	}
	roleCode := strings.ToUpper(strings.TrimSpace(input.RoleCode))
	if roleCode == "" {
		roleCode = "OWNER"
	}
	var roleID int16
	err := r.db.QueryRowContext(ctx, `SELECT nursery_role_id FROM public.nursery_roles WHERE role_code = $1 AND is_active = true`, roleCode).Scan(&roleID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return roleID, err
}

func (r *PostgresRepository) findUserLink(ctx context.Context, nurseryUserID int64) (*UserLink, error) {
	const query = `
		SELECT nu.nursery_user_id, nu.nursery_id, nu.user_id, u.first_name, u.mobile, u.email,
			nr.nursery_role_id, nr.role_code, nr.role_name, nu.joined_at, COALESCE(nu.is_active, true)
		FROM public.nursery_users nu
		JOIN public.users u ON u.user_id = nu.user_id
		JOIN public.nursery_roles nr ON nr.nursery_role_id = nu.nursery_role_id
		WHERE nu.nursery_user_id = $1
	`
	user, err := scanUserLinkRow(r.db.QueryRowContext(ctx, query, nurseryUserID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return user, err
}

func scanNurseryRow(row interface{ Scan(dest ...any) error }) (*Nursery, error) {
	nursery, err := scanNursery(row)
	if err != nil {
		return nil, err
	}
	return &nursery, nil
}

func scanNurseryRows(rows *sql.Rows) (Nursery, error) {
	return scanNursery(rows)
}

func scanNursery(row interface{ Scan(dest ...any) error }) (Nursery, error) {
	var nursery Nursery
	var code, gst, mobile, email, website, description sql.NullString
	var createdBy, updatedBy sql.NullInt64
	if err := row.Scan(&nursery.ID, &code, &nursery.Name, &gst, &mobile, &email, &website, &description, &nursery.Status, &nursery.CreatedAt, &nursery.UpdatedAt, &createdBy, &updatedBy); err != nil {
		return Nursery{}, err
	}
	nursery.Code = nullableString(code)
	nursery.NurseryCode = nullableString(code)
	nursery.GSTNumber = nullableString(gst)
	nursery.Mobile = nullableString(mobile)
	nursery.Email = nullableString(email)
	nursery.Website = nullableString(website)
	nursery.Description = nullableString(description)
	nursery.CreatedBy = nullableInt64(createdBy)
	nursery.UpdatedBy = nullableInt64(updatedBy)
	return nursery, nil
}

func scanAddressRow(row interface{ Scan(dest ...any) error }) (*Address, error) {
	address, err := scanAddress(row)
	if err != nil {
		return nil, err
	}
	return &address, nil
}

func scanAddressRows(rows *sql.Rows) (Address, error) {
	return scanAddress(rows)
}

func scanAddress(row interface{ Scan(dest ...any) error }) (Address, error) {
	var address Address
	var addressType, line1, line2, city, state, country, postal sql.NullString
	var lat, lon sql.NullFloat64
	var createdAt, updatedAt sql.NullTime
	if err := row.Scan(&address.ID, &address.NurseryID, &addressType, &line1, &line2, &city, &state, &country, &postal, &lat, &lon, &address.IsPrimary, &createdAt, &updatedAt); err != nil {
		return Address{}, err
	}
	address.AddressType = nullableString(addressType)
	address.AddressLine1 = nullableString(line1)
	address.AddressLine2 = nullableString(line2)
	address.City = nullableString(city)
	address.State = nullableString(state)
	address.Country = nullableString(country)
	address.PostalCode = nullableString(postal)
	address.Latitude = nullableFloat64(lat)
	address.Longitude = nullableFloat64(lon)
	if createdAt.Valid {
		address.CreatedAt = &createdAt.Time
	}
	if updatedAt.Valid {
		address.UpdatedAt = &updatedAt.Time
	}
	return address, nil
}

func scanUserLinkRow(row interface{ Scan(dest ...any) error }) (*UserLink, error) {
	user, err := scanUserLink(row)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func scanUserLinkRows(rows *sql.Rows) (UserLink, error) {
	return scanUserLink(rows)
}

func scanUserLink(row interface{ Scan(dest ...any) error }) (UserLink, error) {
	var user UserLink
	var email sql.NullString
	var joinedAt sql.NullTime
	if err := row.Scan(&user.ID, &user.NurseryID, &user.UserID, &user.FirstName, &user.Mobile, &email, &user.RoleID, &user.RoleCode, &user.RoleName, &joinedAt, &user.IsActive); err != nil {
		return UserLink{}, err
	}
	user.Email = nullableString(email)
	if joinedAt.Valid {
		user.JoinedAt = &joinedAt.Time
	}
	return user, nil
}

func statusOrActive(value *string) string {
	status := strings.ToUpper(strings.TrimSpace(stringOrEmpty(value)))
	if status == "" {
		return "ACTIVE"
	}
	return status
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func nullableFloat64(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}

func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func floatOrNil(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}
