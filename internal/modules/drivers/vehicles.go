package drivers

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/phone"
	"github.com/meherchaitanyabandaru/greenroot-api/internal/common/publiccode"
)

// A driver's lorries. The lorry and its owner are records of their own: the
// owner needs no GreenRoot account, many drivers may drive one lorry and one
// driver may drive lorries of different owners. GreenRoot approves each link
// (vehicle_drivers); public.driver_vehicle_eligibility says which links can
// take a trip now and why not.

var (
	ErrVehicleLinked     = errors.New("already linked to this lorry")
	ErrVehicleOnTrip     = errors.New("lorry is on an active trip")
	ErrLicenceExpired    = errors.New("licence expiry date is in the past")
	ErrInvalidOwnerPhone = errors.New("owner mobile is not a valid number")
)

// AddVehicleRequest links the driver to a lorry by its registration number.
// A lorry new to GreenRoot is created with its owner; for a lorry already
// registered the type and owner fields are ignored (only GreenRoot changes them).
type AddVehicleRequest struct {
	VehicleNumber string   `json:"vehicle_number"`
	VehicleType   string   `json:"vehicle_type"`
	CapacityKG    *float64 `json:"capacity_kg"`
	// IsOwner: the driver owns this lorry. Otherwise OwnerName is required and
	// OwnerMobile is optional (the owner does not need the app).
	IsOwner     bool    `json:"is_owner"`
	OwnerName   *string `json:"owner_name"`
	OwnerMobile *string `json:"owner_mobile"`
}

// MyVehicle is one of the driver's lorry links with whether it can take a trip.
type MyVehicle struct {
	LinkID                int64      `json:"link_id"`
	LinkStatus            string     `json:"link_status"`
	VehicleID             int64      `json:"vehicle_id"`
	VehicleNumber         string     `json:"vehicle_number"`
	VehicleType           *string    `json:"vehicle_type,omitempty"`
	CapacityKG            *float64   `json:"capacity_kg,omitempty"`
	VehicleStatus         string     `json:"vehicle_status"`
	VehicleApprovalStatus string     `json:"vehicle_approval_status"`
	OwnerName             *string    `json:"owner_name,omitempty"`
	IsOwner               bool       `json:"is_owner"`
	LicenseExpiryDate     *time.Time `json:"license_expiry_date,omitempty"`
	IsOnActiveTrip        bool       `json:"is_on_active_trip"`
	// BlockedReason is empty when the lorry can take a trip now: LINK_PENDING,
	// LINK_SUSPENDED, VEHICLE_NOT_APPROVED, VEHICLE_MAINTENANCE,
	// VEHICLE_INACTIVE, LICENCE_EXPIRED or VEHICLE_ON_TRIP.
	BlockedReason string `json:"blocked_reason,omitempty"`
	IsEligible    bool   `json:"is_eligible"`
}

type MyVehiclesResponse struct {
	Vehicles []MyVehicle `json:"vehicles"`
}

type MyVehicleResponse struct {
	Vehicle MyVehicle `json:"vehicle"`
}

// NormalizeVehicleNumber is how registration numbers are compared: "ts 09 ab-1234" = "TS09AB1234".
func NormalizeVehicleNumber(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(raw) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// vehicleNumberKey is NormalizeVehicleNumber in SQL, for rows stored before numbers were normalized.
const vehicleNumberKey = `regexp_replace(upper(%s), '[^A-Z0-9]', '', 'g')`

func validVehicleNumber(n string) bool { return len(n) >= 4 && len(n) <= 15 }

// MyVehicles lists the driver's open lorry links (pending, approved, suspended).
func (s *Service) MyVehicles(ctx context.Context, actor ActorContext) ([]MyVehicle, error) {
	if !actor.HasRole("DRIVER") {
		if _, err := s.repository.FindByUserID(ctx, actor.UserID); err != nil {
			return nil, ErrForbidden // an applicant without the role yet still has a profile
		}
	}
	return s.repository.ListMyVehicles(ctx, actor.UserID)
}

// AddVehicle asks GreenRoot to link the driver to a lorry.
func (s *Service) AddVehicle(ctx context.Context, actor ActorContext, req AddVehicleRequest) (MyVehicle, error) {
	if _, err := s.repository.FindByUserID(ctx, actor.UserID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return MyVehicle{}, ErrForbidden // register as a driver first
		}
		return MyVehicle{}, err
	}
	owner, err := normalizeAddVehicle(&req)
	if err != nil {
		return MyVehicle{}, err
	}
	if req.VehicleType == "" { // optional only in the older registration request
		return MyVehicle{}, ErrInvalidInput
	}
	v, err := s.repository.AddVehicleLink(ctx, actor.UserID, req, owner)
	if err != nil {
		return MyVehicle{}, err
	}
	s.audit(ctx, actor, v.LinkID, actionInsert, map[string]any{"vehicle_driver": "requested", "vehicle_id": v.VehicleID, "vehicle_number": v.VehicleNumber})
	return v, nil
}

// RemoveVehicle ends one of the driver's lorry links, never during a trip with it.
func (s *Service) RemoveVehicle(ctx context.Context, actor ActorContext, linkID int64) error {
	if err := s.repository.RemoveVehicleLink(ctx, actor.UserID, linkID); err != nil {
		return err
	}
	s.audit(ctx, actor, linkID, actionDelete, map[string]any{"vehicle_driver": "REMOVED", "self": true})
	return nil
}

// ownerInput is who owns a lorry the driver is adding for the first time.
type ownerInput struct {
	name, mobile string
	userID       int64 // the driver, when they own it
}

func normalizeAddVehicle(req *AddVehicleRequest) (ownerInput, error) {
	req.VehicleNumber = NormalizeVehicleNumber(req.VehicleNumber)
	req.VehicleType = strings.ToUpper(strings.TrimSpace(req.VehicleType))
	if !validVehicleNumber(req.VehicleNumber) {
		return ownerInput{}, ErrInvalidInput
	}
	if req.CapacityKG != nil && (*req.CapacityKG <= 0 || *req.CapacityKG > 100000) {
		return ownerInput{}, ErrInvalidInput
	}
	if req.IsOwner {
		return ownerInput{}, nil // filled from the driver's own profile
	}
	o := ownerInput{}
	if req.OwnerName != nil {
		o.name = strings.TrimSpace(*req.OwnerName)
	}
	if o.name == "" || len(o.name) > 255 {
		return ownerInput{}, ErrInvalidInput
	}
	if req.OwnerMobile != nil && strings.TrimSpace(*req.OwnerMobile) != "" {
		m, ok := phone.Normalize(*req.OwnerMobile)
		if !ok {
			return ownerInput{}, ErrInvalidOwnerPhone
		}
		o.mobile = m
	}
	return o, nil
}

func (r *PostgresRepository) ListMyVehicles(ctx context.Context, userID int64) ([]MyVehicle, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT e.vehicle_driver_id, e.link_status, e.vehicle_id, e.vehicle_number, e.vehicle_type, e.capacity_kg,
			COALESCE(e.vehicle_status, ''), e.vehicle_approval_status, o.name, COALESCE(o.user_id = e.driver_user_id, false),
			e.license_expiry_date, e.is_on_active_trip, COALESCE(e.blocked_reason, '')
		FROM public.driver_vehicle_eligibility e
		JOIN public.vehicles v ON v.vehicle_id = e.vehicle_id
		LEFT JOIN public.vehicle_owners o ON o.vehicle_owner_id = v.owner_id
		WHERE e.driver_user_id = $1
		ORDER BY e.blocked_reason IS NULL DESC, e.vehicle_driver_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]MyVehicle, 0)
	for rows.Next() {
		v, err := scanMyVehicle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func scanMyVehicle(row interface{ Scan(dest ...any) error }) (MyVehicle, error) {
	var v MyVehicle
	var vehicleType, ownerName sql.NullString
	var capacity sql.NullFloat64
	var expiry sql.NullTime
	if err := row.Scan(&v.LinkID, &v.LinkStatus, &v.VehicleID, &v.VehicleNumber, &vehicleType, &capacity,
		&v.VehicleStatus, &v.VehicleApprovalStatus, &ownerName, &v.IsOwner,
		&expiry, &v.IsOnActiveTrip, &v.BlockedReason); err != nil {
		return MyVehicle{}, err
	}
	v.VehicleType = nullableString(vehicleType)
	v.OwnerName = nullableString(ownerName)
	if capacity.Valid {
		v.CapacityKG = &capacity.Float64
	}
	if expiry.Valid {
		v.LicenseExpiryDate = &expiry.Time
	}
	v.IsEligible = v.BlockedReason == ""
	return v, nil
}

func (r *PostgresRepository) findMyVehicle(ctx context.Context, userID, linkID int64) (MyVehicle, error) {
	v, err := scanMyVehicle(r.db.QueryRowContext(ctx, `
		SELECT e.vehicle_driver_id, e.link_status, e.vehicle_id, e.vehicle_number, e.vehicle_type, e.capacity_kg,
			COALESCE(e.vehicle_status, ''), e.vehicle_approval_status, o.name, COALESCE(o.user_id = e.driver_user_id, false),
			e.license_expiry_date, e.is_on_active_trip, COALESCE(e.blocked_reason, '')
		FROM public.driver_vehicle_eligibility e
		JOIN public.vehicles v ON v.vehicle_id = e.vehicle_id
		LEFT JOIN public.vehicle_owners o ON o.vehicle_owner_id = v.owner_id
		WHERE e.driver_user_id = $1 AND e.vehicle_driver_id = $2`, userID, linkID))
	if errors.Is(err, sql.ErrNoRows) {
		return MyVehicle{}, ErrNotFound
	}
	return v, err
}

func (r *PostgresRepository) AddVehicleLink(ctx context.Context, userID int64, req AddVehicleRequest, owner ownerInput) (MyVehicle, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return MyVehicle{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if req.IsOwner {
		owner = ownerInput{userID: userID}
	}
	linkID, err := linkVehicleTx(ctx, tx, userID, req.VehicleNumber, req.VehicleType, req.CapacityKG, owner)
	if err != nil {
		return MyVehicle{}, err
	}
	if err := tx.Commit(); err != nil {
		return MyVehicle{}, err
	}
	return r.findMyVehicle(ctx, userID, linkID)
}

// linkVehicleTx finds the lorry by number (creating it with its owner when new)
// and opens a PENDING link for the driver. An open link already there is ErrVehicleLinked.
func linkVehicleTx(ctx context.Context, tx *sql.Tx, userID int64, number, vehicleType string, capacity *float64, owner ownerInput) (int64, error) {
	var vehicleID int64
	// #nosec G201 -- vehicleNumberKey is a fixed SQL fragment.
	err := tx.QueryRowContext(ctx, `SELECT vehicle_id FROM public.vehicles WHERE `+
		sqlf(vehicleNumberKey, "vehicle_number")+` = $1 ORDER BY vehicle_id LIMIT 1 FOR UPDATE`, number).Scan(&vehicleID)
	if errors.Is(err, sql.ErrNoRows) {
		ownerID, err := ensureOwnerTx(ctx, tx, owner)
		if err != nil {
			return 0, err
		}
		code, err := publiccode.Next(ctx, tx, publiccode.Vehicles, time.Now())
		if err != nil {
			return 0, err
		}
		var capArg any
		if capacity != nil {
			capArg = *capacity
		}
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO public.vehicles (vehicle_code, vehicle_number, vehicle_type, capacity_kg, owner_id, status, approval_status, created_at, updated_at)
			VALUES ($1, $2, NULLIF($3, ''), $4, $5, 'ACTIVE', 'PENDING', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
			RETURNING vehicle_id`, code, number, vehicleType, capArg, ownerID).Scan(&vehicleID); err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, err
	}
	var linkID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO public.vehicle_drivers (vehicle_id, driver_user_id, status)
		VALUES ($1, $2, 'PENDING')
		ON CONFLICT (vehicle_id, driver_user_id) WHERE status IN ('PENDING', 'APPROVED', 'SUSPENDED') DO NOTHING
		RETURNING vehicle_driver_id`, vehicleID, userID).Scan(&linkID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrVehicleLinked
	}
	return linkID, err
}

// ensureOwnerTx returns the owner record for o, reusing one with the same
// account or mobile. With userID set the owner is that user (name and mobile
// from their profile).
func ensureOwnerTx(ctx context.Context, tx *sql.Tx, o ownerInput) (int64, error) {
	if o.userID > 0 {
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(NULLIF(btrim(CONCAT_WS(' ', first_name, last_name)), ''), mobile), mobile
			FROM public.users WHERE user_id = $1`, o.userID).Scan(&o.name, &o.mobile); err != nil {
			return 0, err
		}
	}
	var id int64
	err := tx.QueryRowContext(ctx, `
		SELECT vehicle_owner_id FROM public.vehicle_owners
		WHERE ($1 > 0 AND user_id = $1) OR ($2 <> '' AND mobile = $2)
		ORDER BY (user_id = $1) DESC NULLS LAST LIMIT 1`, o.userID, o.mobile).Scan(&id)
	switch {
	case err == nil:
		if o.userID > 0 {
			// The owner record found by mobile is this user's: remember the account.
			_, err = tx.ExecContext(ctx, `UPDATE public.vehicle_owners SET user_id = $2, updated_at = CURRENT_TIMESTAMP
				WHERE vehicle_owner_id = $1 AND user_id IS NULL`, id, o.userID)
		}
		return id, err
	case !errors.Is(err, sql.ErrNoRows):
		return 0, err
	}
	var userArg any
	if o.userID > 0 {
		userArg = o.userID
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO public.vehicle_owners (name, mobile, user_id, created_by_user_id)
		VALUES ($1, NULLIF($2, ''), $3, $3) RETURNING vehicle_owner_id`, o.name, o.mobile, userArg).Scan(&id)
	return id, err
}

func (r *PostgresRepository) RemoveVehicleLink(ctx context.Context, userID, linkID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var vehicleID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT vehicle_id FROM public.vehicle_drivers
		WHERE vehicle_driver_id = $1 AND driver_user_id = $2 AND status IN ('PENDING', 'APPROVED', 'SUSPENDED')
		FOR UPDATE`, linkID, userID).Scan(&vehicleID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	var onTrip bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM public.dispatches
		WHERE driver_user_id = $1 AND vehicle_id = $2 AND status IN ('ACCEPTED', 'DISPATCHED', 'IN_TRANSIT'))`,
		userID, vehicleID).Scan(&onTrip); err != nil {
		return err
	}
	if onTrip {
		return ErrVehicleOnTrip
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE public.vehicle_drivers
		SET status = 'REMOVED', removed_by_user_id = $2, removed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE vehicle_driver_id = $1`, linkID, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// approvePendingLinksTx: approving a driver also approves the lorry links (and
// still-pending lorries) that came with the application GreenRoot reviewed.
func approvePendingLinksTx(ctx context.Context, tx *sql.Tx, userID, actorID int64) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE public.vehicles v
		SET approval_status = 'APPROVED', approved_by_user_id = $2, approved_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		FROM public.vehicle_drivers vd
		WHERE vd.vehicle_id = v.vehicle_id AND vd.driver_user_id = $1 AND vd.status = 'PENDING'
		  AND v.approval_status = 'PENDING'`, userID, actorID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE public.vehicle_drivers
		SET status = 'APPROVED', decided_by_user_id = $2, decided_at = CURRENT_TIMESTAMP,
		    decision_note = 'approved with the driver', updated_at = CURRENT_TIMESTAMP
		WHERE driver_user_id = $1 AND status = 'PENDING'`, userID, actorID)
	return err
}

func sqlf(format, column string) string { return strings.Replace(format, "%s", column, 1) }
