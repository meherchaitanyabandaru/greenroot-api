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
	FindOwnedByUser(ctx context.Context, ownerUserID int64) (*Nursery, error)
	UserOwnsANursery(ctx context.Context, userID int64) (bool, error)
	UserIsManager(ctx context.Context, userID int64) (bool, error)
	UserIsApprovedDriver(ctx context.Context, userID int64) (bool, error)
	IsNurseryOwner(ctx context.Context, nurseryID int64, userID int64) (bool, error)
	Create(ctx context.Context, actorID int64, input CreateNurseryRequest) (*Nursery, error)
	Update(ctx context.Context, actorID int64, nurseryID int64, input UpdateNurseryRequest) (*Nursery, error)
	UpdateStatusOnly(ctx context.Context, actorID int64, nurseryID int64, status string) (*Nursery, error)
	Delete(ctx context.Context, actorID int64, nurseryID int64) error
	ListAddresses(ctx context.Context, nurseryID int64) ([]Address, error)
	CreateAddress(ctx context.Context, nurseryID int64, input AddressRequest) (*Address, error)
	UpdateAddress(ctx context.Context, addressID int64, input AddressRequest) (*Address, error)
	DeleteAddress(ctx context.Context, addressID int64) error
	ListManagers(ctx context.Context, nurseryID int64) ([]UserLink, error)
	ListUsers(ctx context.Context, nurseryID int64) ([]UserLink, error)
	AddUser(ctx context.Context, nurseryID int64, input AddUserRequest) (*UserLink, error)
	AddManager(ctx context.Context, nurseryID int64, invitedByUserID int64, input AddManagerRequest) (*UserLink, error)
	RemoveUser(ctx context.Context, nurseryID int64, userID int64) error
	IsNurseryMember(ctx context.Context, nurseryID int64, userID int64) (bool, error)
	ListByUserID(ctx context.Context, userID int64) ([]Nursery, error)
	ConnectDriver(ctx context.Context, nurseryID int64, driverUserID int64, invitedByUserID int64) (*NurseryDriver, error)
	ApproveDriverConnection(ctx context.Context, nurseryID int64, driverUserID int64, approvedByUserID int64) error
	ListConnectedDrivers(ctx context.Context, nurseryID int64) ([]NurseryDriver, error)
	GetCustomers(ctx context.Context, nurseryID int64) ([]Customer, error)
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
			n.email, n.website, n.description, COALESCE(n.status::text, ''), n.owner_user_id,
			n.created_at, n.updated_at, n.created_by, n.updated_by, n.rejection_reason, n.rejected_at
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

func (r *PostgresRepository) FindOwnedByUser(ctx context.Context, ownerUserID int64) (*Nursery, error) {
	nursery, err := r.scanNursery(ctx, `WHERE n.owner_user_id = $1 AND COALESCE(n.status::text,'') <> 'DELETED'`, ownerUserID)
	if err != nil {
		return nil, err
	}
	nursery.Addresses, _ = r.ListAddresses(ctx, nursery.ID)
	return nursery, nil
}

func (r *PostgresRepository) UserOwnsANursery(ctx context.Context, userID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM public.nurseries
			WHERE owner_user_id = $1
			  AND COALESCE(status::text, '') <> 'DELETED'
		)`,
		userID,
	).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) UserIsManager(ctx context.Context, userID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM public.nursery_users
			WHERE user_id = $1 AND nursery_role_id = 3
			  AND COALESCE(is_active, true) = true
		)`,
		userID,
	).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) UserIsApprovedDriver(ctx context.Context, userID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM public.drivers
			WHERE user_id = $1 AND approval_status = 'APPROVED'
		)`,
		userID,
	).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) IsNurseryOwner(ctx context.Context, nurseryID int64, userID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM public.nurseries
			WHERE nursery_id = $1 AND owner_user_id = $2
			  AND COALESCE(status::text, '') <> 'DELETED'
		)`,
		nurseryID, userID,
	).Scan(&exists)
	return exists, err
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
			status, owner_user_id, created_at, updated_at, created_by, updated_by
		)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''),
			NULLIF($6, ''), NULLIF($7, ''), $8, $9, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, $10, $10)
		RETURNING nursery_id
	`
	var ownerUserID any
	if input.OwnerUserID != nil {
		ownerUserID = *input.OwnerUserID
	}
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
		ownerUserID,
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

func (r *PostgresRepository) UpdateStatusOnly(ctx context.Context, actorID int64, nurseryID int64, status string) (*Nursery, error) {
	result, err := r.db.ExecContext(ctx,
		`UPDATE public.nurseries SET status = $2, updated_at = CURRENT_TIMESTAMP, updated_by = $3 WHERE nursery_id = $1 AND COALESCE(status::text, '') <> 'DELETED'`,
		nurseryID, status, actorID,
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

// ListManagers returns all active managers for a nursery using the V1 text role column.
func (r *PostgresRepository) ListManagers(ctx context.Context, nurseryID int64) ([]UserLink, error) {
	const query = `
		SELECT nu.nursery_user_id, nu.nursery_id, nu.user_id, u.first_name, u.mobile, u.email,
			COALESCE(nu.nursery_role_id, 0),
			COALESCE(nu.role, 'MANAGER'), COALESCE(nu.role, 'MANAGER'),
			nu.role, COALESCE(nu.status, 'ACTIVE'),
			nu.joined_at, COALESCE(nu.is_active, true)
		FROM public.nursery_users nu
		JOIN public.users u ON u.user_id = nu.user_id
		WHERE nu.nursery_id = $1 AND COALESCE(nu.status, 'ACTIVE') = 'ACTIVE'
		ORDER BY u.first_name
	`
	rows, err := r.db.QueryContext(ctx, query, nurseryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]UserLink, 0)
	for rows.Next() {
		user, err := scanManagerLinkRows(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (r *PostgresRepository) ListUsers(ctx context.Context, nurseryID int64) ([]UserLink, error) {
	return r.ListManagers(ctx, nurseryID)
}

// AddManager adds a manager to a nursery using the V1 text role column.
func (r *PostgresRepository) AddManager(ctx context.Context, nurseryID int64, invitedByUserID int64, input AddManagerRequest) (*UserLink, error) {
	role := strings.ToUpper(strings.TrimSpace(input.Role))
	if role == "" {
		role = "MANAGER"
	}
	roleID, err := r.roleID(ctx, AddUserRequest{UserID: input.UserID, RoleCode: role})
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if role == "MANAGER" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE public.nursery_users
			SET status = 'INACTIVE', is_active = false, updated_at = CURRENT_TIMESTAMP
			WHERE user_id = $1 AND nursery_id <> $2 AND COALESCE(status, 'ACTIVE') = 'ACTIVE'
		`, input.UserID, nurseryID); err != nil {
			return nil, err
		}
	}
	const query = `
		INSERT INTO public.nursery_users (nursery_id, user_id, nursery_role_id, role, status, invited_by_user_id, joined_at, is_active)
		VALUES ($1, $2, $3, $4, 'ACTIVE', $5, CURRENT_TIMESTAMP, true)
		ON CONFLICT (nursery_id, user_id, nursery_role_id)
		DO UPDATE SET role = $4, status = 'ACTIVE', is_active = true, updated_at = CURRENT_TIMESTAMP
		RETURNING nursery_user_id
	`
	var nurseryUserID int64
	if err := tx.QueryRowContext(ctx, query, nurseryID, input.UserID, roleID, role, invitedByUserID).Scan(&nurseryUserID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.findManagerLink(ctx, nurseryUserID)
}

func (r *PostgresRepository) AddUser(ctx context.Context, nurseryID int64, input AddUserRequest) (*UserLink, error) {
	roleID, err := r.roleID(ctx, input)
	if err != nil {
		return nil, err
	}
	const query = `
		INSERT INTO public.nursery_users (nursery_id, user_id, nursery_role_id, role, status, joined_at, is_active)
		VALUES ($1, $2, $3, 'MANAGER', 'ACTIVE', CURRENT_TIMESTAMP, true)
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

// ConnectDriver creates a nursery-driver connection request.
func (r *PostgresRepository) ConnectDriver(ctx context.Context, nurseryID int64, driverUserID int64, invitedByUserID int64) (*NurseryDriver, error) {
	status := "REQUESTED"
	if invitedByUserID > 0 {
		status = "INVITED"
	}
	const query = `
		INSERT INTO public.nursery_drivers (nursery_id, driver_user_id, invited_by_user_id, connection_status, created_at, updated_at)
		VALUES ($1, $2, NULLIF($3, 0), $4, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (nursery_id, driver_user_id) DO UPDATE
		SET connection_status = $4, updated_at = CURRENT_TIMESTAMP
		RETURNING id
	`
	var id int64
	if err := r.db.QueryRowContext(ctx, query, nurseryID, driverUserID, invitedByUserID, status).Scan(&id); err != nil {
		return nil, err
	}
	return r.findNurseryDriver(ctx, id)
}

// ApproveDriverConnection approves a nursery-driver connection.
func (r *PostgresRepository) ApproveDriverConnection(ctx context.Context, nurseryID int64, driverUserID int64, approvedByUserID int64) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE public.nursery_drivers
		SET connection_status = 'APPROVED', approved_by_user_id = $3, connected_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE nursery_id = $1 AND driver_user_id = $2
	`, nurseryID, driverUserID, approvedByUserID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// ListConnectedDrivers returns all approved drivers for a nursery.
func (r *PostgresRepository) ListConnectedDrivers(ctx context.Context, nurseryID int64) ([]NurseryDriver, error) {
	const query = `
		SELECT nd.id, nd.nursery_id, nd.driver_user_id,
			u.first_name, u.mobile,
			d.vehicle_number, d.vehicle_type,
			nd.connection_status, nd.connected_at, nd.created_at
		FROM public.nursery_drivers nd
		JOIN public.users u ON u.user_id = nd.driver_user_id
		LEFT JOIN public.drivers d ON d.user_id = nd.driver_user_id
		WHERE nd.nursery_id = $1
		ORDER BY nd.created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, nurseryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	drivers := make([]NurseryDriver, 0)
	for rows.Next() {
		var nd NurseryDriver
		var firstName, mobile string
		var vehicleNumber, vehicleType sql.NullString
		var connectedAt sql.NullTime
		if err := rows.Scan(&nd.ID, &nd.NurseryID, &nd.DriverUserID, &firstName, &mobile, &vehicleNumber, &vehicleType, &nd.ConnectionStatus, &connectedAt, &nd.CreatedAt); err != nil {
			return nil, err
		}
		nd.DriverName = &firstName
		nd.DriverMobile = &mobile
		nd.VehicleNumber = nullableString(vehicleNumber)
		nd.VehicleType = nullableString(vehicleType)
		if connectedAt.Valid {
			nd.ConnectedAt = &connectedAt.Time
		}
		drivers = append(drivers, nd)
	}
	return drivers, rows.Err()
}

func (r *PostgresRepository) findNurseryDriver(ctx context.Context, id int64) (*NurseryDriver, error) {
	const query = `
		SELECT nd.id, nd.nursery_id, nd.driver_user_id,
			u.first_name, u.mobile,
			d.vehicle_number, d.vehicle_type,
			nd.connection_status, nd.connected_at, nd.created_at
		FROM public.nursery_drivers nd
		JOIN public.users u ON u.user_id = nd.driver_user_id
		LEFT JOIN public.drivers d ON d.user_id = nd.driver_user_id
		WHERE nd.id = $1
	`
	var nd NurseryDriver
	var firstName, mobile string
	var vehicleNumber, vehicleType sql.NullString
	var connectedAt sql.NullTime
	if err := r.db.QueryRowContext(ctx, query, id).Scan(&nd.ID, &nd.NurseryID, &nd.DriverUserID, &firstName, &mobile, &vehicleNumber, &vehicleType, &nd.ConnectionStatus, &connectedAt, &nd.CreatedAt); err != nil {
		return nil, err
	}
	nd.DriverName = &firstName
	nd.DriverMobile = &mobile
	nd.VehicleNumber = nullableString(vehicleNumber)
	nd.VehicleType = nullableString(vehicleType)
	if connectedAt.Valid {
		nd.ConnectedAt = &connectedAt.Time
	}
	return &nd, nil
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
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM public.nursery_users
		WHERE nursery_id = $1 AND user_id = $2 AND COALESCE(is_active, true) = true
		UNION ALL
		SELECT 1 FROM public.nurseries
		WHERE nursery_id = $1 AND owner_user_id = $2 AND COALESCE(status::text, '') <> 'DELETED'
	)`, nurseryID, userID).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) ListByUserID(ctx context.Context, userID int64) ([]Nursery, error) {
	const query = `
		SELECT DISTINCT n.nursery_id, n.nursery_code, n.nursery_name, n.gst_number, n.mobile,
			n.email, n.website, n.description, COALESCE(n.status::text, ''), n.owner_user_id,
			n.created_at, n.updated_at, n.created_by, n.updated_by, n.rejection_reason, n.rejected_at
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


// GetCustomers returns all buyers who have accepted a CUSTOMER_INVITE for this nursery.
func (r *PostgresRepository) GetCustomers(ctx context.Context, nurseryID int64) ([]Customer, error) {
	const query = `
		SELECT u.user_id, u.first_name, u.mobile, u.email, i.accepted_at
		FROM public.invites i
		JOIN public.users u ON u.user_id = i.accepted_by_user_id
		WHERE i.nursery_id = $1
		  AND i.invite_type = 'CUSTOMER_INVITE'
		  AND i.status = 'ACCEPTED'
		ORDER BY i.accepted_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, nurseryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	customers := make([]Customer, 0)
	for rows.Next() {
		var c Customer
		var email sql.NullString
		var acceptedAt sql.NullTime
		if err := rows.Scan(&c.UserID, &c.FirstName, &c.Mobile, &email, &acceptedAt); err != nil {
			return nil, err
		}
		c.Email = nullableString(email)
		if acceptedAt.Valid {
			c.AcceptedAt = &acceptedAt.Time
		}
		customers = append(customers, c)
	}
	return customers, rows.Err()
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
			n.email, n.website, n.description, COALESCE(n.status::text, ''), n.owner_user_id,
			n.created_at, n.updated_at, n.created_by, n.updated_by, n.rejection_reason, n.rejected_at
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
		roleCode = "MANAGER"
	}
	var roleID int16
	err := r.db.QueryRowContext(ctx, `SELECT nursery_role_id FROM public.nursery_roles WHERE role_code = $1 AND is_active = true`, roleCode).Scan(&roleID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return roleID, err
}

func (r *PostgresRepository) findUserLink(ctx context.Context, nurseryUserID int64) (*UserLink, error) {
	return r.findManagerLink(ctx, nurseryUserID)
}

func (r *PostgresRepository) findManagerLink(ctx context.Context, nurseryUserID int64) (*UserLink, error) {
	const query = `
		SELECT nu.nursery_user_id, nu.nursery_id, nu.user_id, u.first_name, u.mobile, u.email,
			COALESCE(nu.nursery_role_id, 0),
			COALESCE(nu.role, 'MANAGER'), COALESCE(nu.role, 'MANAGER'),
			COALESCE(nu.role, 'MANAGER'), COALESCE(nu.status, 'ACTIVE'),
			nu.joined_at, COALESCE(nu.is_active, true)
		FROM public.nursery_users nu
		JOIN public.users u ON u.user_id = nu.user_id
		WHERE nu.nursery_user_id = $1
	`
	user, err := scanManagerLinkRow(r.db.QueryRowContext(ctx, query, nurseryUserID))
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
	var code, gst, mobile, email, website, description, rejectionReason sql.NullString
	var ownerUserID, createdBy, updatedBy sql.NullInt64
	var rejectedAt sql.NullTime
	if err := row.Scan(&nursery.ID, &code, &nursery.Name, &gst, &mobile, &email, &website, &description, &nursery.Status, &ownerUserID, &nursery.CreatedAt, &nursery.UpdatedAt, &createdBy, &updatedBy, &rejectionReason, &rejectedAt); err != nil {
		return Nursery{}, err
	}
	nursery.Code = nullableString(code)
	nursery.NurseryCode = nullableString(code)
	nursery.GSTNumber = nullableString(gst)
	nursery.Mobile = nullableString(mobile)
	nursery.Email = nullableString(email)
	nursery.Website = nullableString(website)
	nursery.Description = nullableString(description)
	nursery.RejectionReason = nullableString(rejectionReason)
	if rejectedAt.Valid {
		nursery.RejectedAt = &rejectedAt.Time
	}
	nursery.OwnerUserID = nullableInt64(ownerUserID)
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
	return scanManagerLinkRow(row)
}

func scanUserLinkRows(rows *sql.Rows) (UserLink, error) {
	return scanManagerLink(rows)
}

func scanManagerLinkRow(row interface{ Scan(dest ...any) error }) (*UserLink, error) {
	user, err := scanManagerLink(row)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func scanManagerLinkRows(rows *sql.Rows) (UserLink, error) {
	return scanManagerLink(rows)
}

func scanManagerLink(row interface{ Scan(dest ...any) error }) (UserLink, error) {
	var user UserLink
	var email sql.NullString
	var joinedAt sql.NullTime
	// columns: id, nursery_id, user_id, first_name, mobile, email,
	//          role_id(legacy), role_code, role_name, role(v1), status, joined_at, is_active
	if err := row.Scan(&user.ID, &user.NurseryID, &user.UserID, &user.FirstName, &user.Mobile, &email,
		&user.RoleID, &user.RoleCode, &user.RoleName, &user.Role, &user.Status, &joinedAt, &user.IsActive); err != nil {
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
