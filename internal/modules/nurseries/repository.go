package nurseries

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	apperrs "github.com/meherchaitanyabandaru/greenroot-api/internal/common/errors"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/publiccode"
)

var ErrNotFound = apperrs.ErrNotFound

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
	UpdateStatusOnly(ctx context.Context, actorID int64, nurseryID int64, status, reason string) (*Nursery, error)
	ResubmitByOwner(ctx context.Context, actorID int64, ownerUserID int64, input CreateNurseryRequest) (*Nursery, error)
	Delete(ctx context.Context, actorID int64, nurseryID int64) error
	ListAddresses(ctx context.Context, nurseryID int64) ([]Address, error)
	CreateAddress(ctx context.Context, nurseryID int64, input AddressRequest) (*Address, error)
	UpdateAddress(ctx context.Context, addressID int64, input AddressRequest) (*Address, error)
	DeleteAddress(ctx context.Context, addressID int64) error
	AddressNurseryID(ctx context.Context, addressID int64) (int64, error)
	ListManagers(ctx context.Context, nurseryID int64) ([]UserLink, error)
	ListUsers(ctx context.Context, nurseryID int64) ([]UserLink, error)
	RemoveUser(ctx context.Context, nurseryID int64, userID int64) error
	IsNurseryMember(ctx context.Context, nurseryID int64, userID int64) (bool, error)
	ListByUserID(ctx context.Context, userID int64) ([]Nursery, error)
	ConnectDriver(ctx context.Context, nurseryID int64, driverUserID int64, invitedByUserID int64) (*NurseryDriver, error)
	ApproveDriverConnection(ctx context.Context, nurseryID int64, driverUserID int64, approvedByUserID int64) error
	ListConnectedDrivers(ctx context.Context, nurseryID int64) ([]NurseryDriver, error)
	GetCustomers(ctx context.Context, nurseryID int64) ([]Customer, error)
	GrantOwnerRole(ctx context.Context, ownerUserID int64, assignedByUserID int64) error
	InvalidateUserSessions(ctx context.Context, userID int64) error
	DisconnectDriver(ctx context.Context, nurseryID int64, driverUserID int64, actorID int64) error
	FindActiveManagerNursery(ctx context.Context, userID int64) (int64, error)
	CancelPendingInvitesForUser(ctx context.Context, nurseryID int64, userID int64) error
	WorkspaceUserIDs(ctx context.Context, nurseryID int64) ([]int64, error)
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
	// #nosec G201 -- WHERE/ORDER BY fragments are fixed strings with $n placeholders (ORDER BY from an allowlist); values are bound args.
	query := fmt.Sprintf(`
		SELECT DISTINCT n.nursery_id, n.nursery_code, n.nursery_name, n.mobile,
			n.email, n.website, n.description, COALESCE(n.status::text, ''), n.owner_user_id,
			n.created_at, n.updated_at, n.created_by_user_id, n.updated_by_user_id, n.rejection_reason, n.rejected_at,
			n.logo_url, n.brand_icon_key, n.brand_color,
			(SELECT NULLIF(TRIM(CONCAT_WS(' ', ou.first_name, ou.last_name)), '') FROM public.users ou WHERE ou.user_id = n.owner_user_id)
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
			WHERE user_id = $1 AND role = 'MANAGER' AND status = 'ACTIVE'
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
			  AND COALESCE(status::text, 'ACTIVE') <> 'INACTIVE' -- stopped driving
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
			nursery_code, nursery_name, mobile, email, website, description,
			status, owner_user_id, created_at, updated_at, created_by_user_id, updated_by_user_id
		)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''),
			NULLIF($5, ''), NULLIF($6, ''), $7, $8, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, $9, $9)
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
			nursery_name = COALESCE(NULLIF($3, ''), nursery_name),
			mobile = COALESCE(NULLIF($4, ''), mobile),
			email = COALESCE(NULLIF($5, ''), email),
			website = COALESCE(NULLIF($6, ''), website),
			description = COALESCE(NULLIF($7, ''), description),
			logo_url = COALESCE($9, logo_url),
			brand_icon_key = COALESCE($10, brand_icon_key),
			brand_color = COALESCE($11, brand_color),
			updated_at = CURRENT_TIMESTAMP,
			updated_by_user_id = $8
		WHERE nursery_id = $1 AND COALESCE(status::text, '') <> 'DELETED'
	`
	result, err := r.db.ExecContext(
		ctx,
		query,
		nurseryID,
		stringOrEmpty(input.Code),
		input.Name,
		stringOrEmpty(input.Mobile),
		stringOrEmpty(input.Email),
		stringOrEmpty(input.Website),
		stringOrEmpty(input.Description),
		actorID,
		nullOrString(input.LogoURL),
		nullOrString(input.BrandIconKey),
		nullOrString(input.BrandColor),
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

func (r *PostgresRepository) UpdateStatusOnly(ctx context.Context, actorID int64, nurseryID int64, status, reason string) (*Nursery, error) {
	var result sql.Result
	var err error
	if status == "APPROVED" {
		result, err = r.db.ExecContext(ctx,
			`UPDATE public.nurseries SET status = 'APPROVED', approved_by_user_id = $2, approved_at = CURRENT_TIMESTAMP,
			 rejection_reason = NULL, updated_at = CURRENT_TIMESTAMP, updated_by_user_id = $2
			 WHERE nursery_id = $1 AND status = 'PENDING'`,
			nurseryID, actorID,
		)
	} else {
		result, err = r.db.ExecContext(ctx,
			`UPDATE public.nurseries SET status = 'REJECTED', rejected_by_user_id = $2, rejected_at = CURRENT_TIMESTAMP,
			 rejection_reason = $3, updated_at = CURRENT_TIMESTAMP, updated_by_user_id = $2
			 WHERE nursery_id = $1 AND status = 'PENDING'`,
			nurseryID, actorID, reason,
		)
	}
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

// ResubmitByOwner resets a REJECTED nursery to PENDING with fresh details.
// Only rows matching owner_user_id AND status=REJECTED are updated; returns
// ErrNotFound if no matching row exists (already approved / deleted / wrong owner).
func (r *PostgresRepository) ResubmitByOwner(ctx context.Context, actorID int64, ownerUserID int64, input CreateNurseryRequest) (*Nursery, error) {
	result, err := r.db.ExecContext(ctx,
		`UPDATE public.nurseries
		 SET nursery_name     = $3,
		     mobile           = $4,
		     email            = $5,
		     description      = $6,
		     status           = 'PENDING',
		     rejected_by_user_id      = NULL,
		     rejected_at      = NULL,
		     rejection_reason = NULL,
		     updated_at       = CURRENT_TIMESTAMP,
		     updated_by_user_id       = $2
		 WHERE owner_user_id = $1
		   AND status::text   = 'REJECTED'`,
		ownerUserID, actorID,
		input.Name, input.Mobile, input.Email, input.Description,
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
	return r.FindOwnedByUser(ctx, ownerUserID)
}

// GrantOwnerRole upserts the Nursery Owner system role (role_id=3) for the user.
// Called when admin approves a nursery so the owner's JWT gains NURSERY_OWNER on next login.
func (r *PostgresRepository) GrantOwnerRole(ctx context.Context, ownerUserID int64, assignedByUserID int64) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO public.user_roles (user_id, role_id, assigned_by_user_id, assigned_at)
		 VALUES ($1, 3, $2, CURRENT_TIMESTAMP)
		 ON CONFLICT (user_id, role_id) DO NOTHING`,
		ownerUserID, assignedByUserID,
	)
	return err
}

// Delete closes a nursery and releases everyone attached to it, so the owner
// and managers can take other roles later on the same account. Refused while
// orders, trips or customer quotations are still open.
func (r *PostgresRepository) Delete(ctx context.Context, actorID int64, nurseryID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var open bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM public.orders WHERE seller_nursery_id = $1
		               AND status NOT IN ('COMPLETED', 'CANCELLED', 'PARTIALLY_FULFILLED'))
		    OR EXISTS (SELECT 1 FROM public.dispatches d JOIN public.orders o ON o.order_id = d.order_id
		               WHERE o.seller_nursery_id = $1 AND d.status NOT IN ('DELIVERED', 'CANCELLED'))
		    OR EXISTS (SELECT 1 FROM public.quotations WHERE nursery_id = $1 AND deleted_at IS NULL
		               AND status IN ('CUSTOMER_SENT', 'CUSTOMER_ACCEPTED'))`, nurseryID).Scan(&open); err != nil {
		return err
	}
	if open {
		return ErrNurseryHasOpenWork
	}
	var ownerID sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		UPDATE public.nurseries SET status = 'DELETED', updated_at = CURRENT_TIMESTAMP, updated_by_user_id = $2
		WHERE nursery_id = $1 AND COALESCE(status::text, '') <> 'DELETED'
		RETURNING owner_user_id`, nurseryID, actorID).Scan(&ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	managerIDs, err := queryIDs(ctx, tx, `
		UPDATE public.nursery_users SET is_active = false, status = 'INACTIVE', updated_at = CURRENT_TIMESTAMP
		WHERE nursery_id = $1 AND status = 'ACTIVE' RETURNING user_id`, nurseryID)
	if err != nil {
		return err
	}
	steps := []struct {
		q    string
		args []any
	}{
		{`UPDATE public.nursery_drivers SET status = 'DISCONNECTED', disconnected_at = CURRENT_TIMESTAMP,
		    disconnected_by_user_id = $2, updated_at = CURRENT_TIMESTAMP
		  WHERE nursery_id = $1 AND status IN ('REQUESTED', 'CONNECTED', 'APPROVED')`, []any{nurseryID, actorID}},
		// Managers with no other active membership go back to BUYER.
		{`DELETE FROM public.user_roles WHERE user_id = ANY($1)
		  AND role_id = (SELECT role_id FROM public.roles WHERE role_code = 'MANAGER')
		  AND NOT EXISTS (SELECT 1 FROM public.nursery_users nu WHERE nu.user_id = user_roles.user_id AND nu.status = 'ACTIVE')`, []any{managerIDs}},
		{`INSERT INTO public.user_roles (user_id, role_id)
		  SELECT u, r.role_id FROM unnest($1::bigint[]) u, public.roles r WHERE r.role_code = 'BUYER'
		    AND NOT EXISTS (SELECT 1 FROM public.user_roles ur JOIN public.roles x ON x.role_id = ur.role_id
		                    WHERE ur.user_id = u AND x.role_code IN ('MANAGER', 'DRIVER'))
		  ON CONFLICT DO NOTHING`, []any{managerIDs}},
		// The owner stops being an owner (one nursery per owner) and stops paying.
		{`DELETE FROM public.user_roles WHERE user_id = $1
		  AND role_id = (SELECT role_id FROM public.roles WHERE role_code = 'NURSERY_OWNER')
		  AND NOT EXISTS (SELECT 1 FROM public.nurseries WHERE owner_user_id = $1 AND COALESCE(status::text, '') <> 'DELETED')`, []any{ownerID}},
		{`UPDATE public.user_subscriptions SET status = 'CANCELLED', is_auto_renew = false, updated_at = CURRENT_TIMESTAMP
		  WHERE user_id = $1 AND status IN ('ACTIVE', 'TRIAL', 'PAUSED')
		    AND NOT EXISTS (SELECT 1 FROM public.nurseries WHERE owner_user_id = $1 AND COALESCE(status::text, '') <> 'DELETED')`, []any{ownerID}},
		// Managers lose this workspace: sign them out (like a removal). The
		// owner keeps their session; stale claims make their token refresh.
		{`UPDATE public.user_sessions SET status = 'LOGGED_OUT', session_token = NULL, last_activity_at = CURRENT_TIMESTAMP
		  WHERE status = 'ACTIVE' AND user_id = ANY($1)`, []any{managerIDs}},
	}
	for _, st := range steps {
		if _, err := tx.ExecContext(ctx, st.q, st.args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *PostgresRepository) ListAddresses(ctx context.Context, nurseryID int64) ([]Address, error) {
	const query = `
		SELECT nursery_address_id, nursery_id, address_type::text, address_line1, address_line2,
			city, state, country, postal_code, latitude, longitude, COALESCE(is_primary, false),
			gps_accuracy_meters, landmark, location_source, location_confirmed_by_user_id, location_confirmed_at,
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
			postal_code, latitude, longitude, location, gps_accuracy_meters, landmark,
			location_source, location_confirmed_by_user_id, location_confirmed_at, is_primary, created_at, updated_at
		)
		VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''),
			NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), $9, $10,
			CASE WHEN $9::numeric IS NOT NULL AND $10::numeric IS NOT NULL
				THEN ST_SetSRID(ST_MakePoint($10::double precision, $9::double precision), 4326)::geography
				ELSE NULL
			END,
			$11, NULLIF($12, ''), NULLIF($13, ''), $14, $15,
			-- a nursery's first address is its primary (pickup) address
			$16 OR NOT EXISTS (SELECT 1 FROM public.nursery_addresses WHERE nursery_id = $1),
			CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING nursery_address_id, nursery_id, address_type::text, address_line1, address_line2,
			city, state, country, postal_code, latitude, longitude, COALESCE(is_primary, false),
			gps_accuracy_meters, landmark, location_source, location_confirmed_by_user_id, location_confirmed_at,
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
		floatOrNil(input.GPSAccuracyM),
		stringOrEmpty(input.Landmark),
		stringOrEmpty(input.LocationSource),
		int64OrNil(input.LocationConfirmedBy),
		timeOrNil(input.LocationConfirmedAt),
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
			location = CASE WHEN $9::numeric IS NOT NULL AND $10::numeric IS NOT NULL
				THEN ST_SetSRID(ST_MakePoint($10::double precision, $9::double precision), 4326)::geography
				ELSE NULL
			END,
			gps_accuracy_meters = $11,
			landmark = NULLIF($12, ''),
			location_source = NULLIF($13, ''),
			location_confirmed_by_user_id = $14,
			location_confirmed_at = $15,
			-- only another address can take the primary (pickup) away
			is_primary = CASE WHEN $16 THEN true ELSE is_primary END,
			updated_at = CURRENT_TIMESTAMP
		WHERE nursery_address_id = $1
		RETURNING nursery_address_id, nursery_id, address_type::text, address_line1, address_line2,
			city, state, country, postal_code, latitude, longitude, COALESCE(is_primary, false),
			gps_accuracy_meters, landmark, location_source, location_confirmed_by_user_id, location_confirmed_at,
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
		floatOrNil(input.GPSAccuracyM),
		stringOrEmpty(input.Landmark),
		stringOrEmpty(input.LocationSource),
		int64OrNil(input.LocationConfirmedBy),
		timeOrNil(input.LocationConfirmedAt),
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

func (r *PostgresRepository) AddressNurseryID(ctx context.Context, addressID int64) (int64, error) {
	var nurseryID int64
	err := r.db.QueryRowContext(ctx, `SELECT nursery_id FROM public.nursery_addresses WHERE nursery_address_id = $1`, addressID).Scan(&nurseryID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return nurseryID, err
}

func (r *PostgresRepository) DeleteAddress(ctx context.Context, addressID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var nurseryID int64
	var wasPrimary bool
	err = tx.QueryRowContext(ctx, `DELETE FROM public.nursery_addresses WHERE nursery_address_id = $1 RETURNING nursery_id, COALESCE(is_primary, false)`, addressID).Scan(&nurseryID, &wasPrimary)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var remaining int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM public.nursery_addresses WHERE nursery_id = $1`, nurseryID).Scan(&remaining); err != nil {
		return err
	}
	if remaining == 0 {
		return ErrLastAddress // rolls back: orders need somewhere to pick up from
	}
	// Orders pick up from the primary address: promote the newest remaining one.
	if wasPrimary {
		if _, err := tx.ExecContext(ctx, `
			UPDATE public.nursery_addresses SET is_primary = true, updated_at = CURRENT_TIMESTAMP
			WHERE nursery_address_id = (SELECT nursery_address_id FROM public.nursery_addresses WHERE nursery_id = $1 ORDER BY nursery_address_id DESC LIMIT 1)`, nurseryID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListManagers returns all active managers for a nursery using the V1 text role column.
func (r *PostgresRepository) ListManagers(ctx context.Context, nurseryID int64) ([]UserLink, error) {
	const query = `
		SELECT nu.nursery_user_id, nu.nursery_id, nu.user_id,
			u.first_name, u.last_name,
			NULLIF(TRIM(CONCAT_WS(' ', u.first_name, u.last_name)), ''),
			u.mobile, u.email,
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

// ConnectDriver creates a nursery-driver connection request.
func (r *PostgresRepository) ConnectDriver(ctx context.Context, nurseryID int64, driverUserID int64, invitedByUserID int64) (*NurseryDriver, error) {
	status := "REQUESTED"
	if invitedByUserID > 0 {
		status = "INVITED"
	}
	const query = `
		INSERT INTO public.nursery_drivers (nursery_id, driver_user_id, invited_by_user_id, status, created_at, updated_at)
		VALUES ($1, $2, NULLIF($3, 0), $4, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (nursery_id, driver_user_id) DO UPDATE
		SET status = $4, updated_at = CURRENT_TIMESTAMP
		RETURNING nursery_driver_id
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
		SET status = 'APPROVED', approved_by_user_id = $3, connected_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
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
		SELECT nd.nursery_driver_id, nd.nursery_id, nd.driver_user_id,
			u.first_name, u.mobile,
			d.vehicle_number, d.vehicle_type,
			nd.status, nd.connected_at, nd.created_at
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
		SELECT nd.nursery_driver_id, nd.nursery_id, nd.driver_user_id,
			u.first_name, u.mobile,
			d.vehicle_number, d.vehicle_type,
			nd.status, nd.connected_at, nd.created_at
		FROM public.nursery_drivers nd
		JOIN public.users u ON u.user_id = nd.driver_user_id
		LEFT JOIN public.drivers d ON d.user_id = nd.driver_user_id
		WHERE nd.nursery_driver_id = $1
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

// RemoveUser ends a membership. With no other active membership the user
// loses MANAGER and is a BUYER again (the two roles are exclusive).
func (r *PostgresRepository) RemoveUser(ctx context.Context, nurseryID int64, userID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx,
		`UPDATE public.nursery_users SET is_active = false, status = 'INACTIVE', updated_at = CURRENT_TIMESTAMP
		 WHERE nursery_id = $1 AND user_id = $2 AND status = 'ACTIVE'`,
		nurseryID, userID)
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
	// Their assigned work goes back to the owner: an assignment to someone who
	// left would lock the owner out (assigned-editor rule) and keep the
	// ex-manager's access to those records.
	if _, err := tx.ExecContext(ctx, `
		UPDATE public.quotations SET assigned_manager_user_id = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE nursery_id = $1 AND assigned_manager_user_id = $2`, nurseryID, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE public.orders SET assigned_manager_user_id = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE seller_nursery_id = $1 AND assigned_manager_user_id = $2`, nurseryID, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM public.user_roles
		WHERE user_id = $1
		  AND role_id = (SELECT role_id FROM public.roles WHERE role_code = 'MANAGER')
		  AND NOT EXISTS (SELECT 1 FROM public.nursery_users WHERE user_id = $1 AND status = 'ACTIVE')`, userID); err != nil {
		return err
	}
	// BUYER comes back unless another exclusive role (DRIVER) still holds.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO public.user_roles (user_id, role_id)
		SELECT $1, b.role_id FROM public.roles b
		WHERE b.role_code = 'BUYER'
		  AND NOT EXISTS (
			SELECT 1 FROM public.user_roles ur JOIN public.roles x ON x.role_id = ur.role_id
			WHERE ur.user_id = $1 AND x.role_code IN ('MANAGER', 'DRIVER'))
		ON CONFLICT DO NOTHING`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *PostgresRepository) InvalidateUserSessions(ctx context.Context, userID int64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE public.user_sessions
		 SET status = 'LOGGED_OUT', session_token = NULL, last_activity_at = CURRENT_TIMESTAMP
		 WHERE user_id = $1 AND status = 'ACTIVE'`,
		userID)
	return err
}

func (r *PostgresRepository) DisconnectDriver(ctx context.Context, nurseryID int64, driverUserID int64, actorID int64) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE public.nursery_drivers
		 SET status = 'DISCONNECTED', disconnected_at = CURRENT_TIMESTAMP, disconnected_by_user_id = $3
		 WHERE nursery_id = $1 AND driver_user_id = $2 AND status IN ('REQUESTED','CONNECTED','APPROVED')`,
		nurseryID, driverUserID, actorID)
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

func (r *PostgresRepository) FindActiveManagerNursery(ctx context.Context, userID int64) (int64, error) {
	var nurseryID int64
	err := r.db.QueryRowContext(ctx,
		`SELECT nursery_id FROM public.nursery_users
		 WHERE user_id = $1 AND COALESCE(is_active, true) = true
		 LIMIT 1`,
		userID).Scan(&nurseryID)
	if err == sql.ErrNoRows {
		return 0, ErrNotFound
	}
	return nurseryID, err
}

func (r *PostgresRepository) CancelPendingInvitesForUser(ctx context.Context, nurseryID int64, userID int64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE public.invites
		SET status = 'CANCELLED', updated_at = CURRENT_TIMESTAMP
		WHERE nursery_id = $1
		  AND status = 'PENDING'
		  AND target_mobile = (SELECT mobile FROM public.users WHERE user_id = $2 LIMIT 1)
	`, nurseryID, userID)
	return err
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
		SELECT DISTINCT n.nursery_id, n.nursery_code, n.nursery_name, n.mobile,
			n.email, n.website, n.description, COALESCE(n.status::text, ''), n.owner_user_id,
			n.created_at, n.updated_at, n.created_by_user_id, n.updated_by_user_id, n.rejection_reason, n.rejected_at,
			n.logo_url, n.brand_icon_key, n.brand_color,
			(SELECT NULLIF(TRIM(CONCAT_WS(' ', ou.first_name, ou.last_name)), '') FROM public.users ou WHERE ou.user_id = n.owner_user_id)
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

func (r *PostgresRepository) WorkspaceUserIDs(ctx context.Context, nurseryID int64) ([]int64, error) {
	const query = `
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
			WHERE nursery_id = $1 AND status IN ('APPROVED', 'CONNECTED', 'ACTIVE')
		) users
		WHERE user_id IS NOT NULL
	`
	rows, err := r.db.QueryContext(ctx, query, nurseryID)
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

// GetCustomers returns the nursery's connected customers, newest first: buyers
// who accepted a customer invite, or who dealt with the nursery themselves —
// accepted one of its quotations, placed an order, or received a delivery
// (which needs the code from their own app). A nursery typing a mobile number
// into an order does not connect that person. Deleted and suspended accounts
// are left out. Keep in step with quotations.IsNurseryCustomer.
func (r *PostgresRepository) GetCustomers(ctx context.Context, nurseryID int64) ([]Customer, error) {
	const query = `
		WITH links AS (
			SELECT i.accepted_by_user_id AS user_id, i.accepted_at AS since
			FROM public.invites i
			WHERE i.nursery_id = $1
			  AND i.invite_type = 'CUSTOMER_INVITE'
			  AND i.status = 'ACCEPTED'
			UNION ALL
			SELECT o.buyer_user_id, o.created_at
			FROM public.orders o
			WHERE o.seller_nursery_id = $1
			  AND o.deleted_at IS NULL
			  AND (o.status = 'COMPLETED' OR o.created_by_user_id = o.buyer_user_id)
			UNION ALL
			SELECT u.user_id, q.customer_responded_at
			FROM public.quotations q
			JOIN public.users u ON u.user_id = q.customer_user_id OR u.mobile = q.recipient_mobile
			WHERE q.nursery_id = $1
			  AND q.deleted_at IS NULL
			  AND q.status IN ('CUSTOMER_ACCEPTED', 'CONVERTED')
			  AND q.customer_responded_at IS NOT NULL
		)
		SELECT u.user_id,
		       u.first_name,
		       u.last_name,
		       NULLIF(TRIM(CONCAT_WS(' ', u.first_name, u.last_name)), ''),
		       u.mobile, u.email, MIN(l.since)
		FROM links l
		JOIN public.users u ON u.user_id = l.user_id
		WHERE u.deleted_at IS NULL
		  AND COALESCE(u.status, 'ACTIVE') = 'ACTIVE'
		GROUP BY u.user_id, u.first_name, u.last_name, u.mobile, u.email
		ORDER BY MAX(l.since) DESC NULLS LAST
	`
	rows, err := r.db.QueryContext(ctx, query, nurseryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	customers := make([]Customer, 0)
	for rows.Next() {
		var c Customer
		var lastName, fullName, email sql.NullString
		var acceptedAt sql.NullTime
		if err := rows.Scan(&c.UserID, &c.FirstName, &lastName, &fullName, &c.Mobile, &email, &acceptedAt); err != nil {
			return nil, err
		}
		c.LastName = nullableString(lastName)
		if fullName.Valid && strings.TrimSpace(fullName.String) != "" {
			c.FullName = fullName.String
		} else {
			c.FullName = c.FirstName
		}
		c.Email = nullableString(email)
		if acceptedAt.Valid {
			c.AcceptedAt = &acceptedAt.Time
		}
		customers = append(customers, c)
	}
	return customers, rows.Err()
}
