package users

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrNotFound = errors.New("not found")

type Repository interface {
	FindUserByID(ctx context.Context, userID int64) (*User, error)
	UpdateProfile(ctx context.Context, userID int64, input UpdateProfileRequest, at time.Time) (*User, error)
	ListAddresses(ctx context.Context, userID int64) ([]Address, error)
	CreateAddress(ctx context.Context, userID int64, input CreateAddressRequest) (*Address, error)
	UpdateAddress(ctx context.Context, userID int64, addressID int64, input UpdateAddressRequest) (*Address, error)
	DeleteAddress(ctx context.Context, userID int64, addressID int64) error
	ListRoles(ctx context.Context, userID int64) ([]Role, error)
	ListSessions(ctx context.Context, userID int64) ([]Session, error)
	CreateUserActivity(ctx context.Context, input CreateActivityInput) error
	GetAccountDeletionBlockers(ctx context.Context, userID int64) (AccountDeletionBlockers, error)
	SoftDeleteAccount(ctx context.Context, userID int64) error
}

type AccountDeletionBlockers struct {
	OwnedNurseries   int64
	ActiveOrders     int64
	ActiveQuotations int64
}

func (b AccountDeletionBlockers) HasAny() bool {
	return b.OwnedNurseries > 0 || b.ActiveOrders > 0 || b.ActiveQuotations > 0
}

type CreateActivityInput struct {
	UserID   int64
	Type     string
	Entity   string
	EntityID int64
	DataJSON string
	At       time.Time
}

type PostgresRepository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) Repository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) FindUserByID(ctx context.Context, userID int64) (*User, error) {
	user, err := r.scanUser(ctx, `WHERE user_id = $1 AND deleted_at IS NULL`, userID)
	if err != nil {
		return nil, err
	}
	user.Roles, _ = r.ListRoles(ctx, user.ID)
	return user, nil
}

func (r *PostgresRepository) UpdateProfile(ctx context.Context, userID int64, input UpdateProfileRequest, at time.Time) (*User, error) {
	const query = `
		UPDATE public.users
		SET first_name = $2,
			last_name = NULLIF($3, ''),
			gender = $4::public.gender_type,
			email = NULLIF($5, ''),
			profile_image_url = NULLIF($6, ''),
			updated_at = $7,
			updated_by = $1
		WHERE user_id = $1 AND deleted_at IS NULL
		RETURNING user_id, user_code, first_name, last_name, gender::text, mobile, email, profile_image_url,
			mobile_verified, email_verified, status::text, last_login_at, created_at, updated_at
	`
	user, err := scanUserRow(r.db.QueryRowContext(
		ctx,
		query,
		userID,
		input.FirstName,
		stringOrEmpty(input.LastName),
		nullableStringValue(input.Gender),
		stringOrEmpty(input.Email),
		stringOrEmpty(input.ProfileImageURL),
		at,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	user.Roles, _ = r.ListRoles(ctx, user.ID)
	return user, nil
}

func (r *PostgresRepository) ListAddresses(ctx context.Context, userID int64) ([]Address, error) {
	const query = `
		SELECT address_id, user_id, address_type, contact_name, contact_mobile,
			address_line1, address_line2, city, state, country, postal_code,
			latitude, longitude, COALESCE(is_default, false), created_at, updated_at
		FROM public.user_addresses
		WHERE user_id = $1
		ORDER BY is_default DESC, address_id DESC
	`
	rows, err := r.db.QueryContext(ctx, query, userID)
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

func (r *PostgresRepository) CreateAddress(ctx context.Context, userID int64, input CreateAddressRequest) (*Address, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if input.IsDefault {
		if _, err := tx.ExecContext(ctx, `UPDATE public.user_addresses SET is_default = false WHERE user_id = $1`, userID); err != nil {
			return nil, err
		}
	}

	const query = `
		INSERT INTO public.user_addresses (
			user_id, address_type, contact_name, contact_mobile, address_line1, address_line2,
			city, state, country, postal_code, latitude, longitude, is_default, created_at, updated_at
		)
		VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), $5, NULLIF($6, ''),
			NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''), NULLIF($10, ''), $11, $12, $13, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING address_id, user_id, address_type, contact_name, contact_mobile,
			address_line1, address_line2, city, state, country, postal_code,
			latitude, longitude, COALESCE(is_default, false), created_at, updated_at
	`
	address, err := scanAddressRow(tx.QueryRowContext(ctx, query,
		userID,
		stringOrEmpty(input.AddressType),
		stringOrEmpty(input.ContactName),
		stringOrEmpty(input.ContactMobile),
		input.AddressLine1,
		stringOrEmpty(input.AddressLine2),
		stringOrEmpty(input.City),
		stringOrEmpty(input.State),
		stringOrEmpty(input.Country),
		stringOrEmpty(input.PostalCode),
		floatOrNil(input.Latitude),
		floatOrNil(input.Longitude),
		input.IsDefault,
	))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return address, nil
}

func (r *PostgresRepository) UpdateAddress(ctx context.Context, userID int64, addressID int64, input UpdateAddressRequest) (*Address, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if input.IsDefault {
		if _, err := tx.ExecContext(ctx, `UPDATE public.user_addresses SET is_default = false WHERE user_id = $1 AND address_id <> $2`, userID, addressID); err != nil {
			return nil, err
		}
	}

	const query = `
		UPDATE public.user_addresses
		SET address_type = NULLIF($3, ''),
			contact_name = NULLIF($4, ''),
			contact_mobile = NULLIF($5, ''),
			address_line1 = $6,
			address_line2 = NULLIF($7, ''),
			city = NULLIF($8, ''),
			state = NULLIF($9, ''),
			country = NULLIF($10, ''),
			postal_code = NULLIF($11, ''),
			latitude = $12,
			longitude = $13,
			is_default = $14,
			updated_at = CURRENT_TIMESTAMP
		WHERE user_id = $1 AND address_id = $2
		RETURNING address_id, user_id, address_type, contact_name, contact_mobile,
			address_line1, address_line2, city, state, country, postal_code,
			latitude, longitude, COALESCE(is_default, false), created_at, updated_at
	`
	address, err := scanAddressRow(tx.QueryRowContext(ctx, query,
		userID,
		addressID,
		stringOrEmpty(input.AddressType),
		stringOrEmpty(input.ContactName),
		stringOrEmpty(input.ContactMobile),
		input.AddressLine1,
		stringOrEmpty(input.AddressLine2),
		stringOrEmpty(input.City),
		stringOrEmpty(input.State),
		stringOrEmpty(input.Country),
		stringOrEmpty(input.PostalCode),
		floatOrNil(input.Latitude),
		floatOrNil(input.Longitude),
		input.IsDefault,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return address, nil
}

func (r *PostgresRepository) DeleteAddress(ctx context.Context, userID int64, addressID int64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM public.user_addresses WHERE user_id = $1 AND address_id = $2`, userID, addressID)
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

func (r *PostgresRepository) ListRoles(ctx context.Context, userID int64) ([]Role, error) {
	const query = `
		SELECT r.role_id, r.role_code, r.role_name
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

	roles := make([]Role, 0)
	for rows.Next() {
		var role Role
		if err := rows.Scan(&role.ID, &role.Code, &role.Name); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

func (r *PostgresRepository) ListSessions(ctx context.Context, userID int64) ([]Session, error) {
	const query = `
		SELECT session_id, user_id, login_time, last_activity_at, COALESCE(session_status::text, ''),
			device_type, os_name, app_version, ip_address, user_agent, created_at
		FROM public.user_sessions
		WHERE user_id = $1
		ORDER BY last_activity_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := make([]Session, 0)
	for rows.Next() {
		var session Session
		var deviceType, osName, appVersion, ipAddress, userAgent sql.NullString
		var createdAt sql.NullTime
		if err := rows.Scan(
			&session.ID,
			&session.UserID,
			&session.LoginTime,
			&session.LastActivityAt,
			&session.Status,
			&deviceType,
			&osName,
			&appVersion,
			&ipAddress,
			&userAgent,
			&createdAt,
		); err != nil {
			return nil, err
		}
		session.DeviceType = nullableString(deviceType)
		session.OSName = nullableString(osName)
		session.AppVersion = nullableString(appVersion)
		session.IPAddress = nullableString(ipAddress)
		session.UserAgent = nullableString(userAgent)
		if createdAt.Valid {
			session.CreatedAt = &createdAt.Time
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func (r *PostgresRepository) CreateUserActivity(ctx context.Context, input CreateActivityInput) error {
	const query = `
		INSERT INTO public.user_activities (
			user_id, activity_type, entity_type, entity_id, activity_data, activity_timestamp
		)
		VALUES ($1, $2, NULLIF($3, ''), $4, NULLIF($5, '')::jsonb, $6)
	`
	_, err := r.db.ExecContext(ctx, query, input.UserID, input.Type, input.Entity, input.EntityID, input.DataJSON, input.At)
	return err
}

func (r *PostgresRepository) GetAccountDeletionBlockers(ctx context.Context, userID int64) (AccountDeletionBlockers, error) {
	const query = `
		SELECT
			(SELECT COUNT(*)
			   FROM public.nurseries
			  WHERE owner_user_id = $1
			    AND COALESCE(status::text, '') NOT IN ('DELETED', 'REJECTED')) AS owned_nurseries,
			(SELECT COUNT(*)
			   FROM public.orders
			  WHERE deleted_at IS NULL
			    AND COALESCE(order_status::text, '') NOT IN ('COMPLETED', 'CANCELLED')
			    AND (
			    	created_by_user_id = $1 OR assigned_manager_user_id = $1 OR
			    	customer_user_id = $1 OR buyer_user_id = $1
			    )) AS active_orders,
			(SELECT COUNT(*)
			   FROM public.quotations
			  WHERE deleted_at IS NULL
			    AND COALESCE(status::text, '') IN (
			    	'INTERNAL_DRAFT', 'CUSTOMER_DRAFT', 'CUSTOMER_SENT', 'CUSTOMER_ACCEPTED'
			    )
			    AND (
			    	created_by_user_id = $1 OR assigned_manager_user_id = $1 OR customer_user_id = $1
			    )) AS active_quotations
	`
	var blockers AccountDeletionBlockers
	err := r.db.QueryRowContext(ctx, query, userID).Scan(
		&blockers.OwnedNurseries,
		&blockers.ActiveOrders,
		&blockers.ActiveQuotations,
	)
	return blockers, err
}

func (r *PostgresRepository) SoftDeleteAccount(ctx context.Context, userID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Anonymize PII and mark deleted
	_, err = tx.ExecContext(ctx, `
		UPDATE public.users SET
			first_name        = 'Deleted',
			last_name         = NULL,
			email             = NULL,
			profile_image_url = NULL,
			gender            = NULL,
			status            = 'DELETED',
			deleted_at        = CURRENT_TIMESTAMP,
			updated_at        = CURRENT_TIMESTAMP
		WHERE user_id = $1 AND deleted_at IS NULL`, userID)
	if err != nil {
		return err
	}

	// 2. Revoke all active sessions — next refresh attempt will fail
	_, err = tx.ExecContext(ctx, `
		UPDATE public.user_sessions
		SET session_status = 'LOGGED_OUT', session_token = NULL, last_activity_at = CURRENT_TIMESTAMP
		WHERE user_id = $1 AND session_status = 'ACTIVE'`, userID)
	if err != nil {
		return err
	}

	// 3. Deactivate all nursery memberships (manager roles)
	_, err = tx.ExecContext(ctx, `
		UPDATE public.nursery_users
		SET is_active = false, status = 'INACTIVE', updated_at = CURRENT_TIMESTAMP
		WHERE user_id = $1 AND COALESCE(is_active, true) = true`, userID)
	if err != nil {
		return err
	}

	// 4. Disconnect any active driver profiles
	_, err = tx.ExecContext(ctx, `
		UPDATE public.nursery_drivers
		SET connection_status = 'DISCONNECTED', disconnected_at = CURRENT_TIMESTAMP, disconnected_by = $1
		WHERE driver_user_id = $1 AND connection_status IN ('REQUESTED','CONNECTED','APPROVED')`, userID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *PostgresRepository) scanUser(ctx context.Context, where string, args ...any) (*User, error) {
	query := `
		SELECT user_id, user_code, first_name, last_name, gender::text, mobile, email, profile_image_url,
			COALESCE(mobile_verified, false), COALESCE(email_verified, false), COALESCE(status::text, ''),
			last_login_at, created_at, updated_at
		FROM public.users
		` + where

	user, err := scanUserRow(r.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return user, err
}

func scanUserRow(row interface{ Scan(dest ...any) error }) (*User, error) {
	var user User
	var lastName, gender, email, profileImageURL sql.NullString
	var lastLoginAt, createdAt, updatedAt sql.NullTime
	if err := row.Scan(
		&user.ID,
		&user.UserCode,
		&user.FirstName,
		&lastName,
		&gender,
		&user.Mobile,
		&email,
		&profileImageURL,
		&user.MobileVerified,
		&user.EmailVerified,
		&user.Status,
		&lastLoginAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	user.LastName = nullableString(lastName)
	user.Gender = nullableString(gender)
	user.Email = nullableString(email)
	user.ProfileImageURL = nullableString(profileImageURL)
	if lastLoginAt.Valid {
		user.LastLoginAt = &lastLoginAt.Time
	}
	if createdAt.Valid {
		user.CreatedAt = createdAt.Time
	}
	if updatedAt.Valid {
		user.UpdatedAt = updatedAt.Time
	}
	return &user, nil
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
	var addressType, contactName, contactMobile, addressLine2, city, state, country, postalCode sql.NullString
	var latitude, longitude sql.NullFloat64
	var createdAt, updatedAt sql.NullTime
	err := row.Scan(
		&address.ID,
		&address.UserID,
		&addressType,
		&contactName,
		&contactMobile,
		&address.AddressLine1,
		&addressLine2,
		&city,
		&state,
		&country,
		&postalCode,
		&latitude,
		&longitude,
		&address.IsDefault,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return Address{}, err
	}
	address.AddressType = nullableString(addressType)
	address.ContactName = nullableString(contactName)
	address.ContactMobile = nullableString(contactMobile)
	address.AddressLine2 = nullableString(addressLine2)
	address.City = nullableString(city)
	address.State = nullableString(state)
	address.Country = nullableString(country)
	address.PostalCode = nullableString(postalCode)
	if latitude.Valid {
		address.Latitude = &latitude.Float64
	}
	if longitude.Valid {
		address.Longitude = &longitude.Float64
	}
	if createdAt.Valid {
		address.CreatedAt = &createdAt.Time
	}
	if updatedAt.Valid {
		address.UpdatedAt = &updatedAt.Time
	}
	return address, nil
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nullableStringValue(value *string) any {
	if value == nil || *value == "" {
		return nil
	}
	return *value
}

func floatOrNil(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}
